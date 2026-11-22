package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var transitions = map[string][]string{
	"open":          {"investigating", "resolved"},
	"investigating": {"open", "resolved"},
	"resolved":      {"investigating", "closed"},
	"closed":        {},
}

func canTransition(from, to string) bool {
	for _, t := range transitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// Only an Admin may close an incident.
func canActorTransition(role, to string) bool { return to != "closed" || role == "admin" }

var allowedActions = map[string]bool{"isolate_host": true, "block_ip": true, "disable_account": true}

func isUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func (s *Server) auditMeta(ctx context.Context, tenantID, actor, action, target, result string, meta map[string]any) {
	if s.pool == nil {
		return
	}
	b, _ := json.Marshal(meta)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO audit_logs (tenant_id, actor, action, target, result, metadata)
		 VALUES (NULLIF($1::text,'')::uuid, $2, $3, $4, $5, $6::text::jsonb)`,
		tenantID, short(actor), action, short(target), result, string(b))
	if err != nil {
		log.Printf("audit write failed: %v", err)
	}
}

// PATCH /v1/incidents/{id}: status transitions and assignment, role-checked and audited.
func (s *Server) handlePatchIncident(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<14)
	var in struct {
		Status *string `json:"status"`
		Owner  *string `json:"owner"` // "me", "none", or a user id
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || (in.Status == nil && in.Owner == nil) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "send status and/or owner"})
		return
	}
	if in.Status != nil && !validStatus[*in.Status] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	ctx := r.Context()
	detached := context.WithoutCancel(ctx)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	defer tx.Rollback(ctx)

	var cur string
	var owner *string
	err = tx.QueryRow(ctx,
		`SELECT status, owner_id::text FROM incidents
		  WHERE id = $1::text::uuid AND tenant_id = $2::text::uuid FOR UPDATE`, id, sess.TenantID).Scan(&cur, &owner)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}

	var after []func()
	if in.Status != nil {
		from, to := cur, *in.Status
		if to == from {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "already in that status"})
			return
		}
		if !canTransition(from, to) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "transition not allowed", "from": from, "to": to})
			return
		}
		if !canActorTransition(sess.Role, to) {
			s.audit(detached, sess.TenantID, sess.Subject, "authz.denied", id, "denied")
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only an admin can close an incident"})
			return
		}
		if _, err := tx.Exec(ctx,
			`UPDATE incidents SET status = $3, updated_at = now() WHERE id = $1::text::uuid AND tenant_id = $2::text::uuid`,
			id, sess.TenantID, to); err != nil {
			if isUniqueViolation(err) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "another open incident with the same key exists"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
			return
		}
		after = append(after, func() {
			s.auditMeta(detached, sess.TenantID, sess.Subject, "incident.status", id, "success", map[string]any{"from": from, "to": to})
		})
		cur = to
	}

	if in.Owner != nil {
		v := *in.Owner
		var target *string
		switch {
		case v == "none":
			if sess.Role != "admin" && owner != nil && *owner != sess.UserID {
				s.audit(detached, sess.TenantID, sess.Subject, "authz.denied", id, "denied")
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "analysts can only release their own incidents"})
				return
			}
		case v == "me":
			me := sess.UserID
			target = &me
		case uuidRe.MatchString(v):
			if sess.Role != "admin" && v != sess.UserID {
				s.audit(detached, sess.TenantID, sess.Subject, "authz.denied", id, "denied")
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "only an admin can assign to someone else"})
				return
			}
			target = &v
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "owner must be me, none or a user id"})
			return
		}
		if target != nil {
			var n int
			if err := tx.QueryRow(ctx,
				`SELECT count(*) FROM users WHERE id = $1::text::uuid AND tenant_id = $2::text::uuid AND status = 'active'`,
				*target, sess.TenantID).Scan(&n); err != nil || n == 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown or inactive user"})
				return
			}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE incidents SET owner_id = $3::text::uuid, updated_at = now() WHERE id = $1::text::uuid AND tenant_id = $2::text::uuid`,
			id, sess.TenantID, target); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
			return
		}
		owner = target
		who := "none"
		if target != nil {
			who = *target
		}
		after = append(after, func() {
			s.auditMeta(detached, sess.TenantID, sess.Subject, "incident.assign", id, "success", map[string]any{"owner": who})
		})
	}

	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	for _, f := range after {
		f()
	}
	s.hub.publish(sess.TenantID, "incident.updated", map[string]any{"id": id, "status": cur})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": cur, "owner_id": owner})
}

// POST /v1/incidents/{id}/approvals: ask for a (simulated) response action. Nothing runs until an admin approves.
func (s *Server) handleRequestApproval(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<14)
	var in struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !allowedActions[in.Action] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be isolate_host, block_ip or disable_account"})
		return
	}
	ctx := r.Context()
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT status FROM incidents WHERE id = $1::text::uuid AND tenant_id = $2::text::uuid`, id, sess.TenantID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	if status != "open" && status != "investigating" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "incident is not active"})
		return
	}
	var aid string
	err = s.pool.QueryRow(ctx,
		`INSERT INTO approvals (tenant_id, incident_id, action, requester)
		 VALUES ($1::text::uuid, $2::text::uuid, $3, $4::text::uuid) RETURNING id::text`,
		sess.TenantID, id, in.Action, sess.UserID).Scan(&aid)
	if err != nil {
		if isUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "an approval is already pending for this incident"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	s.auditMeta(context.WithoutCancel(ctx), sess.TenantID, sess.Subject, "approval.requested", id, "success", map[string]any{"approval": aid, "action": in.Action})
	s.hub.publish(sess.TenantID, "approval.requested", map[string]any{"id": aid, "incident_id": id, "action": in.Action})
	writeJSON(w, http.StatusCreated, map[string]string{"approval_id": aid, "decision": "pending"})
}

// POST /v1/approvals/{id}/decision (Admin only). The requester can never decide their own request.
func (s *Server) handleDecideApproval(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<14)
	var in struct {
		Decision string `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || (in.Decision != "approved" && in.Decision != "denied") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "decision must be approved or denied"})
		return
	}
	ctx := r.Context()
	detached := context.WithoutCancel(ctx)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	defer tx.Rollback(ctx)

	var incID, action, requester, current string
	err = tx.QueryRow(ctx,
		`SELECT incident_id::text, action, requester::text, decision FROM approvals
		  WHERE id = $1::text::uuid AND tenant_id = $2::text::uuid FOR UPDATE`, id, sess.TenantID).
		Scan(&incID, &action, &requester, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	if current != "pending" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already decided"})
		return
	}
	if requester == sess.UserID {
		s.auditMeta(detached, sess.TenantID, sess.Subject, "approval.self_decision", incID, "denied", map[string]any{"approval": id})
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "the requester cannot decide their own request"})
		return
	}
	note := ""
	if in.Decision == "approved" {
		note = fmt.Sprintf("SIMULATED %s for incident %s. No real system was changed.", action, incID)
		_, err = tx.Exec(ctx,
			`UPDATE approvals SET decision = 'approved', approver = $2::text::uuid, decided_at = now(), executed_at = now(), simulation_note = $3
			  WHERE id = $1::text::uuid`, id, sess.UserID, note)
	} else {
		_, err = tx.Exec(ctx,
			`UPDATE approvals SET decision = 'denied', approver = $2::text::uuid, decided_at = now() WHERE id = $1::text::uuid`, id, sess.UserID)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	s.auditMeta(detached, sess.TenantID, sess.Subject, "approval."+in.Decision, incID, "success", map[string]any{"approval": id, "action": action})
	if in.Decision == "approved" {
		s.auditMeta(detached, sess.TenantID, "system:simulation", "containment.simulated", incID, "success", map[string]any{"approval": id, "action": action, "note": note})
	}
	s.hub.publish(sess.TenantID, "approval.decided", map[string]any{"id": id, "incident_id": incID, "decision": in.Decision})
	writeJSON(w, http.StatusOK, map[string]string{"approval_id": id, "decision": in.Decision, "simulation": note})
}

type approvalOut struct {
	ID             string     `json:"id"`
	Action         string     `json:"action"`
	Decision       string     `json:"decision"`
	Requester      string     `json:"requester"`
	Approver       string     `json:"approver"`
	CreatedAt      time.Time  `json:"created_at"`
	DecidedAt      *time.Time `json:"decided_at"`
	ExecutedAt     *time.Time `json:"executed_at"`
	SimulationNote string     `json:"simulation_note"`
}

type auditOut struct {
	At       time.Time       `json:"at"`
	Actor    string          `json:"actor"`
	Action   string          `json:"action"`
	Result   string          `json:"result"`
	Metadata json.RawMessage `json:"metadata"`
}

func (s *Server) loadApprovalsAndAudit(ctx context.Context, tenantID, incID string) ([]approvalOut, []auditOut, error) {
	approvals := []approvalOut{}
	rows, err := s.pool.Query(ctx,
		`SELECT a.id::text, a.action, a.decision, COALESCE(ru.subject,''), COALESCE(au.subject,''),
		        a.created_at, a.decided_at, a.executed_at, COALESCE(a.simulation_note,'')
		   FROM approvals a
		   LEFT JOIN users ru ON ru.id = a.requester
		   LEFT JOIN users au ON au.id = a.approver
		  WHERE a.incident_id = $1::text::uuid AND a.tenant_id = $2::text::uuid
		  ORDER BY a.created_at`, incID, tenantID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var a approvalOut
		if err := rows.Scan(&a.ID, &a.Action, &a.Decision, &a.Requester, &a.Approver, &a.CreatedAt, &a.DecidedAt, &a.ExecutedAt, &a.SimulationNote); err != nil {
			rows.Close()
			return nil, nil, err
		}
		approvals = append(approvals, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	trail := []auditOut{}
	arows, err := s.pool.Query(ctx,
		`SELECT timestamp, actor, action, result, metadata FROM audit_logs
		  WHERE tenant_id = $1::text::uuid AND target = $2 ORDER BY id LIMIT 200`, tenantID, incID)
	if err != nil {
		return nil, nil, err
	}
	defer arows.Close()
	for arows.Next() {
		var x auditOut
		var m []byte
		if err := arows.Scan(&x.At, &x.Actor, &x.Action, &x.Result, &m); err != nil {
			return nil, nil, err
		}
		x.Metadata = json.RawMessage(m)
		trail = append(trail, x)
	}
	return approvals, trail, arows.Err()
}
