package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type incidentOut struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Severity   string    `json:"severity"`
	Status     string    `json:"status"`
	RuleID     string    `json:"rule_id"`
	OwnerID    *string   `json:"owner_id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	EventCount int       `json:"event_count"`
}

var validStatus = map[string]bool{"open": true, "investigating": true, "resolved": true, "closed": true}

func (s *Server) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	limit, offset := 50, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be 1 to 200"})
			return
		}
		limit = n
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 100000 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid offset"})
			return
		}
		offset = n
	}
	status := r.URL.Query().Get("status")
	if status != "" && !validStatus[status] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	rows, err := s.pool.Query(r.Context(),
		`SELECT i.id::text, i.title, i.severity, i.status, COALESCE(i.rule_id,''), i.owner_id::text,
		        i.created_at, i.updated_at,
		        (SELECT count(*) FROM incident_events ie WHERE ie.incident_id = i.id)
		   FROM incidents i
		  WHERE i.tenant_id = $1::text::uuid AND ($2::text = '' OR i.status = $2::text)
		  ORDER BY i.created_at DESC
		  LIMIT $3 OFFSET $4`, sess.TenantID, status, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	defer rows.Close()
	out := []incidentOut{}
	for rows.Next() {
		var i incidentOut
		if err := rows.Scan(&i.ID, &i.Title, &i.Severity, &i.Status, &i.RuleID, &i.OwnerID, &i.CreatedAt, &i.UpdatedAt, &i.EventCount); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
			return
		}
		out = append(out, i)
	}
	if rows.Err() != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidents": out, "limit": limit, "offset": offset})
}

// Another tenant's incident looks exactly like a missing one (404).
func (s *Server) handleGetIncident(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	var inc incidentOut
	err := s.pool.QueryRow(r.Context(),
		`SELECT i.id::text, i.title, i.severity, i.status, COALESCE(i.rule_id,''), i.owner_id::text,
		        i.created_at, i.updated_at,
		        (SELECT count(*) FROM incident_events ie WHERE ie.incident_id = i.id)
		   FROM incidents i WHERE i.id = $1::text::uuid AND i.tenant_id = $2::text::uuid`, id, sess.TenantID).
		Scan(&inc.ID, &inc.Title, &inc.Severity, &inc.Status, &inc.RuleID, &inc.OwnerID, &inc.CreatedAt, &inc.UpdatedAt, &inc.EventCount)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}

	type ev struct {
		ID         string          `json:"id"`
		AgentID    string          `json:"agent_id"`
		OccurredAt time.Time       `json:"occurred_at"`
		Kind       string          `json:"kind"`
		Severity   string          `json:"severity"`
		Payload    json.RawMessage `json:"payload"`
	}
	events := []ev{}
	erows, err := s.pool.Query(r.Context(),
		`SELECT e.id::text, e.agent_id::text, e.occurred_at, e.kind, e.severity, e.payload
		   FROM incident_events ie JOIN events e ON e.id = ie.event_id
		  WHERE ie.incident_id = $1::text::uuid AND e.tenant_id = $2::text::uuid
		  ORDER BY e.occurred_at`, id, sess.TenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	defer erows.Close()
	for erows.Next() {
		var e ev
		var p []byte
		if err := erows.Scan(&e.ID, &e.AgentID, &e.OccurredAt, &e.Kind, &e.Severity, &p); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
			return
		}
		e.Payload = json.RawMessage(p)
		events = append(events, e)
	}
	if erows.Err() != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}

	type alert struct {
		ID        string    `json:"id"`
		RuleID    string    `json:"rule_id"`
		Severity  string    `json:"severity"`
		Status    string    `json:"status"`
		EventID   string    `json:"event_id"`
		AgentID   string    `json:"agent_id"`
		CreatedAt time.Time `json:"created_at"`
	}
	alerts := []alert{}
	arows, err := s.pool.Query(r.Context(),
		`SELECT id::text, rule_id, COALESCE(severity,''), status, COALESCE(event_id::text,''), COALESCE(agent_id::text,''), created_at
		   FROM alerts WHERE incident_id = $1::text::uuid AND tenant_id = $2::text::uuid
		  ORDER BY created_at`, id, sess.TenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	defer arows.Close()
	for arows.Next() {
		var a alert
		if err := arows.Scan(&a.ID, &a.RuleID, &a.Severity, &a.Status, &a.EventID, &a.AgentID, &a.CreatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
			return
		}
		alerts = append(alerts, a)
	}
	if arows.Err() != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	approvals, trail, err := s.loadApprovalsAndAudit(r.Context(), sess.TenantID, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incident": inc, "events": events, "alerts": alerts, "approvals": approvals, "audit_trail": trail})
}
