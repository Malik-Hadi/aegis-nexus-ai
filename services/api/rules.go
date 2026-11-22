package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	det001Threshold = 6
	det001Window    = 5 * time.Minute
	det003Threshold = 10
	det003MinPorts  = 3
	det003Window    = 5 * time.Minute
	det005Stale     = 3 * time.Minute
)

var privilegedGroups = map[string]bool{
	"administrators": true, "domain admins": true, "enterprise admins": true,
	"schema admins": true, "root": true, "sudo": true, "wheel": true,
}

var hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

type iocSet map[string]bool

// loadIOC reads one SHA-256 hash per line (# starts a comment). Invalid lines are ignored.
func loadIOC(path string) (iocSet, error) {
	f, err := os.Open(path)
	if err != nil {
		return iocSet{}, err
	}
	defer f.Close()
	out := iocSet{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.ToLower(strings.TrimSpace(sc.Text()))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if hashRe.MatchString(line) {
			out[line] = true
		}
	}
	return out, sc.Err()
}

func isPrivilegedGroup(g string) bool { return privilegedGroups[strings.ToLower(strings.TrimSpace(g))] }
func det001Hit(n int) bool           { return n >= det001Threshold }

func distinctCount(vals []string) int {
	seen := map[string]bool{}
	for _, v := range vals {
		seen[v] = true
	}
	return len(seen)
}

func det003Hit(ports []string) bool {
	return len(ports) >= det003Threshold && distinctCount(ports) >= det003MinPorts
}

func str(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return strings.TrimSpace(v)
}

type detection struct {
	ruleID, title, severity, dedupKey string
	evidence                          []string // event IDs that justify the alert
}

// evaluate runs after a NEW event is stored. Errors are logged; they never fail ingestion.
func (s *Server) evaluate(ctx context.Context, tenantID, agentID, eventID string, c cleanEvent) {
	var p map[string]any
	if err := json.Unmarshal(c.payload, &p); err != nil {
		return
	}
	var d *detection
	var err error
	switch c.kind {
	case "auth.failed_login":
		d, err = s.checkDET001(ctx, tenantID, str(p, "user"), c.occurred)
	case "privilege.group_added":
		d = checkDET002(eventID, p)
	case "net.connection_failed":
		d, err = s.checkDET003(ctx, tenantID, str(p, "source_ip"), c.occurred)
	case "process.executable_seen":
		d = s.checkDET004(eventID, p)
	}
	if err != nil {
		log.Printf("rule evaluation error (%s): %v", c.kind, err)
		return
	}
	if d == nil {
		return
	}
	if err := s.raise(ctx, tenantID, agentID, eventID, *d); err != nil {
		log.Printf("could not raise %s: %v", d.ruleID, err)
	}
}

func collectIDs(rows pgx.Rows) ([]string, error) {
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DET-001: 6 failed logins for one identity within 5 minutes.
func (s *Server) checkDET001(ctx context.Context, tenant, user string, at time.Time) (*detection, error) {
	if user == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id::text FROM events
		  WHERE tenant_id = $1::text::uuid AND kind = 'auth.failed_login' AND payload->>'user' = $2
		    AND occurred_at BETWEEN $3 AND $4
		  ORDER BY occurred_at`, tenant, user, at.Add(-det001Window), at)
	if err != nil {
		return nil, err
	}
	ids, err := collectIDs(rows)
	if err != nil || !det001Hit(len(ids)) {
		return nil, err
	}
	return &detection{"DET-001", "Repeated failed logins for " + user, "high", "DET-001:" + short(user), ids}, nil
}

// DET-002: privileged group membership added (unless the event says approved=true).
func checkDET002(eventID string, p map[string]any) *detection {
	group := str(p, "group")
	if !isPrivilegedGroup(group) {
		return nil
	}
	if approved, _ := p["approved"].(bool); approved {
		return nil
	}
	user := str(p, "user")
	return &detection{"DET-002", fmt.Sprintf("Unexpected privileged group membership: %s added to %s", user, group),
		"high", "DET-002:" + short(user) + ":" + strings.ToLower(group), []string{eventID}}
}

// DET-003: 10 failed connections from one source to at least 3 different ports within 5 minutes.
func (s *Server) checkDET003(ctx context.Context, tenant, src string, at time.Time) (*detection, error) {
	if src == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, COALESCE(payload->>'dest_port','') FROM events
		  WHERE tenant_id = $1::text::uuid AND kind = 'net.connection_failed' AND payload->>'source_ip' = $2
		    AND occurred_at BETWEEN $3 AND $4
		  ORDER BY occurred_at`, tenant, src, at.Add(-det003Window), at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids, ports []string
	for rows.Next() {
		var id, port string
		if err := rows.Scan(&id, &port); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		ports = append(ports, port)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !det003Hit(ports) {
		return nil, nil
	}
	return &detection{"DET-003", "Port-scan pattern from " + src, "medium", "DET-003:" + short(src), ids}, nil
}

// DET-004: executable hash is in the local lab IOC list.
func (s *Server) checkDET004(eventID string, p map[string]any) *detection {
	h := strings.ToLower(str(p, "sha256"))
	if !hashRe.MatchString(h) || !s.ioc[h] {
		return nil
	}
	return &detection{"DET-004", "Known-bad executable hash observed (lab IOC) " + h[:12], "high", "DET-004:" + h, []string{eventID}}
}

func upsertIncident(ctx context.Context, tx pgx.Tx, tenant string, d detection) (string, bool, error) {
	var id string
	err := tx.QueryRow(ctx,
		`INSERT INTO incidents (tenant_id, title, severity, rule_id, dedup_key)
		 VALUES ($1::text::uuid, $2, $3, $4, $5)
		 ON CONFLICT (tenant_id, dedup_key) WHERE dedup_key IS NOT NULL AND status IN ('open','investigating')
		 DO NOTHING RETURNING id::text`,
		tenant, d.title, d.severity, d.ruleID, d.dedupKey).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	err = tx.QueryRow(ctx,
		`SELECT id::text FROM incidents
		  WHERE tenant_id = $1::text::uuid AND dedup_key = $2 AND status IN ('open','investigating')
		  ORDER BY created_at DESC LIMIT 1`, tenant, d.dedupKey).Scan(&id)
	return id, false, err
}

// raise creates (or joins) ONE incident per dedup key, records the alert and links the evidence events.
func (s *Server) raise(ctx context.Context, tenantID, agentID, eventID string, d detection) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	incID, created, err := upsertIncident(ctx, tx, tenantID, d)
	if err != nil {
		return err
	}
	if eventID == "" && !created {
		return tx.Commit(ctx) // heartbeat rule: do not repeat the alert every 30 seconds
	}
	if _, err = tx.Exec(ctx,
		`INSERT INTO alerts (tenant_id, event_id, rule_id, agent_id, incident_id, severity)
		 VALUES ($1::text::uuid, NULLIF($2::text,'')::uuid, $3, $4::text::uuid, $5::text::uuid, $6)
		 ON CONFLICT DO NOTHING`,
		tenantID, eventID, d.ruleID, agentID, incID, d.severity); err != nil {
		return err
	}
	if len(d.evidence) > 0 {
		if _, err = tx.Exec(ctx,
			`INSERT INTO incident_events (incident_id, event_id)
			 SELECT $1::text::uuid, x::uuid FROM unnest($2::text[]) AS x
			 ON CONFLICT DO NOTHING`, incID, d.evidence); err != nil {
			return err
		}
	}
	if !created {
		if _, err = tx.Exec(ctx, `UPDATE incidents SET updated_at = now() WHERE id = $1::text::uuid`, incID); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if created {
		s.audit(ctx, tenantID, "system:"+d.ruleID, "incident.created", incID, "success")
		s.hub.publish(tenantID, "incident.created", map[string]any{"id": incID, "rule_id": d.ruleID, "severity": d.severity, "title": d.title})
	} else {
		s.hub.publish(tenantID, "incident.updated", map[string]any{"id": incID, "rule_id": d.ruleID})
	}
	return nil
}

// DET-005: an active agent whose last heartbeat is older than 3 minutes.
func (s *Server) checkHeartbeats(ctx context.Context) error {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, tenant_id::text, hostname FROM agents
		  WHERE status = 'active' AND last_seen_at IS NOT NULL
		    AND last_seen_at < now() - $1::text::interval`, fmt.Sprintf("%d seconds", int(det005Stale.Seconds())))
	if err != nil {
		return err
	}
	type stale struct{ id, tenant, host string }
	var list []stale
	for rows.Next() {
		var x stale
		if err := rows.Scan(&x.id, &x.tenant, &x.host); err != nil {
			rows.Close()
			return err
		}
		list = append(list, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, x := range list {
		d := detection{"DET-005", "Agent heartbeat missing: " + x.host, "medium", "DET-005:" + x.id, nil}
		if err := s.raise(ctx, x.tenant, x.id, "", d); err != nil {
			log.Printf("DET-005 for agent %s: %v", x.id, err)
		}
	}
	return nil
}

func (s *Server) monitorHeartbeats(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c, cancel := context.WithTimeout(ctx, 20*time.Second)
			if err := s.checkHeartbeats(c); err != nil {
				log.Printf("heartbeat monitor: %v", err)
			}
			cancel()
		}
	}
}
