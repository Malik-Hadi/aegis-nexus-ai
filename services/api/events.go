package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

var kindRe = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
var keyRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
var validSeverity = map[string]bool{"info": true, "low": true, "medium": true, "high": true, "critical": true}

const maxClockSkew = 5 * time.Minute
const maxEventAge = 30 * 24 * time.Hour
const maxPayloadBytes = 16 * 1024

type eventIn struct {
	Kind           string          `json:"kind"`
	OccurredAt     string          `json:"occurred_at"`
	Severity       string          `json:"severity"`
	Payload        json.RawMessage `json:"payload"`
	SourceRef      string          `json:"source_ref"`
	IdempotencyKey string          `json:"idempotency_key"`
}

type cleanEvent struct {
	kind, severity, sourceRef, key string
	occurred                       time.Time
	payload                        []byte
}

// validateEvent checks every field. Anything not on the allow-list is rejected.
func validateEvent(in eventIn, now time.Time) (cleanEvent, error) {
	var c cleanEvent
	if len(in.Kind) > 64 || !kindRe.MatchString(in.Kind) {
		return c, errors.New("invalid kind")
	}
	c.kind = in.Kind
	c.severity = in.Severity
	if c.severity == "" {
		c.severity = "info"
	}
	if !validSeverity[c.severity] {
		return c, errors.New("invalid severity")
	}
	t, err := time.Parse(time.RFC3339, in.OccurredAt)
	if err != nil {
		return c, errors.New("occurred_at must be RFC 3339 time")
	}
	t = t.UTC()
	if t.After(now.Add(maxClockSkew)) {
		return c, errors.New("occurred_at is in the future")
	}
	if t.Before(now.Add(-maxEventAge)) {
		return c, errors.New("occurred_at is too old")
	}
	c.occurred = t
	if !keyRe.MatchString(in.IdempotencyKey) {
		return c, errors.New("invalid idempotency_key")
	}
	c.key = in.IdempotencyKey
	if len(in.SourceRef) > 128 {
		return c, errors.New("source_ref too long")
	}
	c.sourceRef = in.SourceRef
	p := bytes.TrimSpace(in.Payload)
	if len(p) == 0 || string(p) == "null" {
		p = []byte("{}")
	}
	if len(p) > maxPayloadBytes || p[0] != '{' || !json.Valid(p) {
		return c, errors.New("payload must be a JSON object under 16 KB")
	}
	c.payload = p
	return c, nil
}

// handleIngest: tenant and agent come from the credential, NOT from the body.
func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	a := agentFrom(r)
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var in eventIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "body too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	c, err := validateEvent(in, time.Now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var id string
	err = s.pool.QueryRow(r.Context(),
		`INSERT INTO events (tenant_id, agent_id, occurred_at, kind, severity, payload, source_ref, idempotency_key)
		 VALUES ($1::text::uuid, $2::text::uuid, $3, $4, $5, $6::text::jsonb, NULLIF($7::text,''), $8)
		 ON CONFLICT (tenant_id, agent_id, idempotency_key) DO NOTHING
		 RETURNING id::text`,
		a.TenantID, a.ID, c.occurred, c.kind, c.severity, string(c.payload), c.sourceRef, c.key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "duplicate"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	_, _ = s.pool.Exec(r.Context(),
		`UPDATE agents SET last_seen_at = now() WHERE id = $1::text::uuid AND tenant_id = $2::text::uuid`, a.ID, a.TenantID)
	ectx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.evaluate(ectx, a.TenantID, a.ID, id, c)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "event_id": id})
}

// handleListEvents: the tenant comes ONLY from the session cookie.
func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
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
	kind := r.URL.Query().Get("kind")
	if len(kind) > 64 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid kind"})
		return
	}

	rows, err := s.pool.Query(r.Context(),
		`SELECT id::text, agent_id::text, occurred_at, received_at, kind, severity, payload
		   FROM events
		  WHERE tenant_id = $1::text::uuid AND ($2::text = '' OR kind = $2::text)
		  ORDER BY occurred_at DESC, id
		  LIMIT $3 OFFSET $4`, sess.TenantID, kind, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	defer rows.Close()
	type ev struct {
		ID         string          `json:"id"`
		AgentID    string          `json:"agent_id"`
		OccurredAt time.Time       `json:"occurred_at"`
		ReceivedAt time.Time       `json:"received_at"`
		Kind       string          `json:"kind"`
		Severity   string          `json:"severity"`
		Payload    json.RawMessage `json:"payload"`
	}
	out := []ev{}
	for rows.Next() {
		var e ev
		var p []byte
		if err := rows.Scan(&e.ID, &e.AgentID, &e.OccurredAt, &e.ReceivedAt, &e.Kind, &e.Severity, &p); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
			return
		}
		e.Payload = json.RawMessage(p)
		out = append(out, e)
	}
	if rows.Err() != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out, "limit": limit, "offset": offset})
}
