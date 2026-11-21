package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var hostRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

type agentCtxKey struct{}

// AgentIdentity comes ONLY from the verified credential, never from the request body.
type AgentIdentity struct {
	ID       string
	TenantID string
}

func agentFrom(r *http.Request) AgentIdentity {
	a, _ := r.Context().Value(agentCtxKey{}).(AgentIdentity)
	return a
}

func newSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Secrets are long random values, so a fast SHA-256 hash is appropriate (passwords would need bcrypt).
func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// parseAgentCredential reads "Bearer <agent-uuid>.<64-hex-secret>".
func parseAgentCredential(header string) (id, secret string, ok bool) {
	if !strings.HasPrefix(header, "Bearer ") {
		return "", "", false
	}
	tok := strings.TrimSpace(header[len("Bearer "):])
	id, secret, found := strings.Cut(tok, ".")
	if !found || !uuidRe.MatchString(id) || len(secret) != 64 {
		return "", "", false
	}
	return strings.ToLower(id), secret, true
}

// requireAgent: valid, ACTIVE agent credential or 401. Revoked agents are rejected here on every request.
func (s *Server) requireAgent(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deny := func() {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		}
		id, secret, ok := parseAgentCredential(r.Header.Get("Authorization"))
		if !ok {
			deny()
			return
		}
		var tenant, hash, status string
		err := s.pool.QueryRow(r.Context(),
			`SELECT tenant_id::text, credential_hash, status FROM agents WHERE id = $1::text::uuid`, id).
			Scan(&tenant, &hash, &status)
		if err != nil || subtle.ConstantTimeCompare([]byte(hash), []byte(hashSecret(secret))) != 1 {
			s.audit(r.Context(), "", "agent:"+id, "agent.auth", "", "failure")
			deny()
			return
		}
		if status != "active" {
			s.audit(r.Context(), tenant, "agent:"+id, "agent.rejected", "", "revoked")
			deny()
			return
		}
		ident := AgentIdentity{ID: id, TenantID: tenant}
		next(w, r.WithContext(context.WithValue(r.Context(), agentCtxKey{}, ident)))
	}
}

// Admin creates a one-time enrolment token (valid 1 hour) for THEIR tenant.
func (s *Server) handleCreateEnrolToken(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	tok, err := newSecret()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	_, err = s.pool.Exec(r.Context(),
		`INSERT INTO enrolment_tokens (tenant_id, token_hash, created_by, expires_at)
		 VALUES ($1::text::uuid, $2, $3::text::uuid, now() + interval '1 hour')`,
		sess.TenantID, hashSecret(tok), sess.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	s.audit(r.Context(), sess.TenantID, sess.Subject, "enrolment.token_created", "", "success")
	writeJSON(w, http.StatusCreated, map[string]string{"enrolment_token": tok, "expires_in": "1 hour"})
}

// Agent exchanges a one-time token for its own credential. The token is consumed atomically.
func (s *Server) handleEnrol(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var in struct {
		Token    string `json:"token"`
		Hostname string `json:"hostname"`
		OS       string `json:"os"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if !hostRe.MatchString(in.Hostname) || (in.OS != "windows" && in.OS != "linux") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid hostname or os"})
		return
	}

	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	defer tx.Rollback(ctx)

	var tenant string
	err = tx.QueryRow(ctx,
		`UPDATE enrolment_tokens SET used_at = now()
		  WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
		  RETURNING tenant_id::text`, hashSecret(in.Token)).Scan(&tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		s.audit(ctx, "", "enrol:"+in.Hostname, "agent.enrol", "", "failure")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or used token"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}

	secret, err := newSecret()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	var agentID string
	err = tx.QueryRow(ctx,
		`INSERT INTO agents (tenant_id, hostname, os, credential_hash)
		 VALUES ($1::text::uuid, $2, $3, $4) RETURNING id::text`,
		tenant, in.Hostname, in.OS, hashSecret(secret)).Scan(&agentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	s.audit(ctx, tenant, "agent:"+agentID, "agent.enrol", in.Hostname, "success")
	writeJSON(w, http.StatusCreated, map[string]string{"agent_id": agentID, "credential": agentID + "." + secret})
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	a := agentFrom(r)
	_, err := s.pool.Exec(r.Context(),
		`UPDATE agents SET last_seen_at = now() WHERE id = $1::text::uuid AND tenant_id = $2::text::uuid`, a.ID, a.TenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	rows, err := s.pool.Query(r.Context(),
		`SELECT id::text, hostname, os, last_seen_at, status
		   FROM agents WHERE tenant_id = $1::text::uuid ORDER BY hostname`, sess.TenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	defer rows.Close()
	type agent struct {
		ID         string     `json:"id"`
		Hostname   string     `json:"hostname"`
		OS         string     `json:"os"`
		LastSeenAt *time.Time `json:"last_seen_at"`
		Status     string     `json:"status"`
	}
	out := []agent{}
	for rows.Next() {
		var a agent
		if err := rows.Scan(&a.ID, &a.Hostname, &a.OS, &a.LastSeenAt, &a.Status); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
			return
		}
		out = append(out, a)
	}
	if rows.Err() != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

// Revoke is tenant-scoped: another tenant's agent looks exactly like a missing one (404).
func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	var host string
	err := s.pool.QueryRow(r.Context(),
		`UPDATE agents SET status = 'revoked'
		  WHERE id = $1::text::uuid AND tenant_id = $2::text::uuid
		  RETURNING hostname`, id, sess.TenantID).Scan(&host)
	if errors.Is(err, pgx.ErrNoRows) {
		s.audit(r.Context(), sess.TenantID, sess.Subject, "agent.revoke", id, "not_found")
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	s.audit(r.Context(), sess.TenantID, sess.Subject, "agent.revoke", id, "success")
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked", "hostname": host})
}
