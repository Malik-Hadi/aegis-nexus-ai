package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- unit tests (no database) ----------

func TestDET001Threshold(t *testing.T) {
	if det001Hit(5) || !det001Hit(6) || !det001Hit(7) {
		t.Error("DET-001 must fire at 6 or more, not at 5")
	}
}

func TestPrivilegedGroups(t *testing.T) {
	for _, g := range []string{"Administrators", " domain admins ", "ROOT", "sudo", "wheel"} {
		if !isPrivilegedGroup(g) {
			t.Errorf("%q should be privileged", g)
		}
	}
	for _, g := range []string{"Printer Users", "", "users", "administrators-lab-readonly"} {
		if isPrivilegedGroup(g) {
			t.Errorf("%q should NOT be privileged", g)
		}
	}
}

func TestDET002Decision(t *testing.T) {
	if checkDET002("e1", map[string]any{"user": "u", "group": "Administrators"}) == nil {
		t.Error("privileged group must alert")
	}
	if checkDET002("e1", map[string]any{"user": "u", "group": "Administrators", "approved": true}) != nil {
		t.Error("approved change must not alert")
	}
	if checkDET002("e1", map[string]any{"user": "u", "group": "Printer Users"}) != nil {
		t.Error("normal group must not alert")
	}
}

func TestDET003Decision(t *testing.T) {
	same := make([]string, 10)
	for i := range same {
		same[i] = "443"
	}
	many := []string{"22", "80", "443", "445", "3389", "8080", "5900", "21", "23", "25"}
	if det003Hit(same) {
		t.Error("10 failures to ONE port is not a scan")
	}
	if det003Hit(many[:9]) {
		t.Error("9 failures is below the threshold")
	}
	if !det003Hit(many) {
		t.Error("10 failures to many ports must alert")
	}
}

func TestDET004Decision(t *testing.T) {
	s := &Server{ioc: iocSet{"0000000000000000000000000000000000000000000000000000000000000001": true}}
	if s.checkDET004("e1", map[string]any{"sha256": "0000000000000000000000000000000000000000000000000000000000000001"}) == nil {
		t.Error("known-bad hash must alert")
	}
	if s.checkDET004("e1", map[string]any{"sha256": "0000000000000000000000000000000000000000000000000000000000000001"[:10]}) != nil {
		t.Error("malformed hash must not alert")
	}
	if s.checkDET004("e1", map[string]any{"sha256": strings.Repeat("a", 64)}) != nil {
		t.Error("unknown hash must not alert")
	}
}

func TestLoadIOC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ioc.txt")
	good := strings.Repeat("A", 64)
	_ = os.WriteFile(path, []byte("# comment\n\n"+good+"\nnot-a-hash\n"), 0o600)
	set, err := loadIOC(path)
	if err != nil || len(set) != 1 || !set[strings.ToLower(good)] {
		t.Fatalf("want exactly 1 lower-cased hash, got %v err=%v", set, err)
	}
}

func TestProjectIOCListLoads(t *testing.T) {
	set, err := loadIOC(filepath.Join("..", "..", "simulations", "ioc_hashes.txt"))
	if err != nil || len(set) < 2 {
		t.Fatalf("project IOC list should hold at least 2 hashes, got %d err=%v", len(set), err)
	}
}

func TestIncidentRoutesNeedLogin(t *testing.T) {
	s := testServer()
	for _, p := range []string{"/v1/incidents", "/v1/incidents/" + testTenantA} {
		if got := doReq(s, "GET", p, "", "", "").Code; got != 401 {
			t.Errorf("%s: want 401, got %d", p, got)
		}
	}
}

// ---------- integration helpers ----------

type fxEvent struct {
	Kind           string         `json:"kind"`
	OccurredAt     string         `json:"occurred_at"`
	Severity       string         `json:"severity"`
	Payload        map[string]any `json:"payload"`
	IdempotencyKey string         `json:"idempotency_key"`
}

func uniq() string {
	k, _ := newSecret()
	return k[:8]
}

// runFixture posts a fixture file through the real API: shifted to "now", unique keys, optional payload change.
func runFixture(t *testing.T, s *Server, cred, file, run string, mutate func(map[string]any)) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "simulations", file))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Events []fxEvent `json:"events"`
	}
	if err := json.Unmarshal(b, &f); err != nil || len(f.Events) == 0 {
		t.Fatalf("bad fixture %s: %v", file, err)
	}
	var latest time.Time
	for _, e := range f.Events {
		tt, err := time.Parse(time.RFC3339, e.OccurredAt)
		if err != nil {
			t.Fatal(err)
		}
		if tt.After(latest) {
			latest = tt
		}
	}
	delta := time.Now().UTC().Truncate(time.Second).Sub(latest)
	for i, e := range f.Events {
		tt, _ := time.Parse(time.RFC3339, e.OccurredAt)
		e.OccurredAt = tt.Add(delta).UTC().Format(time.RFC3339)
		e.IdempotencyKey = fmt.Sprintf("%s-%s-%d", e.IdempotencyKey, run, i)
		if mutate != nil {
			mutate(e.Payload)
		}
		body, _ := json.Marshal(e)
		if rr := doReq(s, "POST", "/v1/events", string(body), cred, ""); rr.Code != 202 {
			t.Fatalf("%s event %d: want 202, got %d %s", file, i, rr.Code, rr.Body.String())
		}
	}
}

func countQ(t *testing.T, s *Server, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	return n
}

func alertsFor(t *testing.T, s *Server, agentID, rule string) int {
	return countQ(t, s, `SELECT count(*) FROM alerts a JOIN events e ON a.event_id = e.id
		WHERE e.agent_id = $1::text::uuid AND a.rule_id = $2`, agentID, rule)
}

func incidentsFor(t *testing.T, s *Server, agentID, rule string) int {
	return countQ(t, s, `SELECT count(DISTINCT ie.incident_id) FROM incident_events ie
		JOIN events e ON e.id = ie.event_id JOIN incidents i ON i.id = ie.incident_id
		WHERE e.agent_id = $1::text::uuid AND i.rule_id = $2`, agentID, rule)
}

// Registered AFTER enrolAgent, so it runs BEFORE enrolAgent's own cleanup (t.Cleanup is last-in first-out).
func cleanupDetections(t *testing.T, s *Server, agentID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		var ids []string
		rows, err := s.pool.Query(ctx,
			`SELECT id::text FROM incidents WHERE id IN (
			   SELECT ie.incident_id FROM incident_events ie JOIN events e ON e.id = ie.event_id WHERE e.agent_id = $1::text::uuid)
			 OR dedup_key = 'DET-005:' || $1::text`, agentID)
		if err == nil {
			ids, _ = collectIDs(rows)
		}
		_, _ = s.pool.Exec(ctx, `DELETE FROM incident_events WHERE incident_id = ANY($1::text[]::uuid[])`, ids)
		_, _ = s.pool.Exec(ctx, `DELETE FROM alerts WHERE incident_id = ANY($1::text[]::uuid[]) OR agent_id = $2::text::uuid`, ids, agentID)
		_, _ = s.pool.Exec(ctx, `DELETE FROM incidents WHERE id = ANY($1::text[]::uuid[])`, ids)
	})
}

func postEvent(t *testing.T, s *Server, cred, kind, key string, payload map[string]any) {
	t.Helper()
	body, _ := json.Marshal(fxEvent{Kind: kind, OccurredAt: time.Now().UTC().Format(time.RFC3339), Severity: "low", Payload: payload, IdempotencyKey: key})
	if rr := doReq(s, "POST", "/v1/events", string(body), cred, ""); rr.Code != 202 {
		t.Fatalf("post %s: want 202, got %d %s", kind, rr.Code, rr.Body.String())
	}
}

// ---------- integration tests: one positive and one benign fixture per rule (AT-07) ----------

func TestDET001Fixtures(t *testing.T) {
	s := integServer(t)
	ag, _ := enrolAgent(t, s, testTenantA, "test-host-det001")
	cleanupDetections(t, s, ag.AgentID)
	run := uniq()
	as := func(suffix string) func(map[string]any) {
		return func(p map[string]any) { p["user"] = "u-" + run + suffix }
	}

	runFixture(t, s, ag.Credential, "det001_benign_five.json", run+"a", as("a"))
	runFixture(t, s, ag.Credential, "det001_benign_spread.json", run+"b", as("b"))
	if n := alertsFor(t, s, ag.AgentID, "DET-001"); n != 0 {
		t.Fatalf("benign fixtures must not alert, got %d alerts", n)
	}
	if n := incidentsFor(t, s, ag.AgentID, "DET-001"); n != 0 {
		t.Fatalf("benign fixtures must not create incidents, got %d", n)
	}

	runFixture(t, s, ag.Credential, "det001_positive.json", run+"p", as("p"))
	if n := alertsFor(t, s, ag.AgentID, "DET-001"); n != 1 {
		t.Fatalf("positive fixture: want 1 alert, got %d", n)
	}
	if n := incidentsFor(t, s, ag.AgentID, "DET-001"); n != 1 {
		t.Fatalf("positive fixture: want 1 incident, got %d", n)
	}
	ev := countQ(t, s, `SELECT count(*) FROM incident_events ie JOIN events e ON e.id = ie.event_id
		WHERE e.agent_id = $1::text::uuid AND e.payload->>'user' = $2`, ag.AgentID, "u-"+run+"p")
	if ev != 6 {
		t.Errorf("incident must link all 6 evidence events, got %d", ev)
	}
	sev := countQ(t, s, `SELECT count(*) FROM incidents i WHERE i.rule_id = 'DET-001' AND i.severity = 'high'
		AND i.dedup_key = $1`, "DET-001:u-"+run+"p")
	if sev != 1 {
		t.Error("DET-001 incident must have severity high")
	}

	// Deduplication: more failures for the same user join the SAME incident.
	postEvent(t, s, ag.Credential, "auth.failed_login", "extra-"+run+"-1", map[string]any{"user": "u-" + run + "p", "source_ip": "192.0.2.10"})
	postEvent(t, s, ag.Credential, "auth.failed_login", "extra-"+run+"-2", map[string]any{"user": "u-" + run + "p", "source_ip": "192.0.2.10"})
	if n := incidentsFor(t, s, ag.AgentID, "DET-001"); n != 1 {
		t.Errorf("dedup: want still 1 incident, got %d", n)
	}
}

func TestDET002Fixtures(t *testing.T) {
	s := integServer(t)
	ag, _ := enrolAgent(t, s, testTenantA, "test-host-det002")
	cleanupDetections(t, s, ag.AgentID)
	run := uniq()
	as := func(p map[string]any) { p["user"] = "u-" + run }

	runFixture(t, s, ag.Credential, "det002_benign.json", run+"n", as)
	if n := alertsFor(t, s, ag.AgentID, "DET-002"); n != 0 {
		t.Fatalf("benign: want 0 alerts, got %d", n)
	}
	runFixture(t, s, ag.Credential, "det002_positive.json", run+"p", as)
	if n := alertsFor(t, s, ag.AgentID, "DET-002"); n != 1 {
		t.Errorf("positive: want 1 alert, got %d", n)
	}
	if n := incidentsFor(t, s, ag.AgentID, "DET-002"); n != 1 {
		t.Errorf("positive: want 1 incident, got %d", n)
	}
}

func TestDET003Fixtures(t *testing.T) {
	s := integServer(t)
	ag, _ := enrolAgent(t, s, testTenantA, "test-host-det003")
	cleanupDetections(t, s, ag.AgentID)
	run := uniq()

	runFixture(t, s, ag.Credential, "det003_benign.json", run+"n", func(p map[string]any) { p["source_ip"] = "sim-" + run + "n" })
	if n := alertsFor(t, s, ag.AgentID, "DET-003"); n != 0 {
		t.Fatalf("benign: want 0 alerts, got %d", n)
	}
	runFixture(t, s, ag.Credential, "det003_positive.json", run+"p", func(p map[string]any) { p["source_ip"] = "sim-" + run + "p" })
	if n := alertsFor(t, s, ag.AgentID, "DET-003"); n != 1 {
		t.Errorf("positive: want 1 alert, got %d", n)
	}
	if n := incidentsFor(t, s, ag.AgentID, "DET-003"); n != 1 {
		t.Errorf("positive: want 1 incident, got %d", n)
	}
}

func TestDET004Fixtures(t *testing.T) {
	s := integServer(t)
	ioc, err := loadIOC(filepath.Join("..", "..", "simulations", "ioc_hashes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	s.ioc = ioc
	ag, _ := enrolAgent(t, s, testTenantA, "test-host-det004")
	cleanupDetections(t, s, ag.AgentID)
	run := uniq()

	runFixture(t, s, ag.Credential, "det004_benign.json", run+"n", nil)
	if n := alertsFor(t, s, ag.AgentID, "DET-004"); n != 0 {
		t.Fatalf("benign: want 0 alerts, got %d", n)
	}
	runFixture(t, s, ag.Credential, "det004_positive.json", run+"p", nil)
	if n := alertsFor(t, s, ag.AgentID, "DET-004"); n != 1 {
		t.Errorf("positive: want 1 alert, got %d", n)
	}
	if n := incidentsFor(t, s, ag.AgentID, "DET-004"); n != 1 {
		t.Errorf("positive: want 1 incident, got %d", n)
	}
}

func TestDET005Fixtures(t *testing.T) {
	s := integServer(t)
	ag, _ := enrolAgent(t, s, testTenantA, "test-host-det005")
	cleanupDetections(t, s, ag.AgentID)
	ctx := context.Background()
	key := "DET-005:" + ag.AgentID

	// Benign: the agent just sent a heartbeat.
	if got := doReq(s, "POST", "/v1/agents/heartbeat", "", ag.Credential, "").Code; got != 200 {
		t.Fatalf("heartbeat: want 200, got %d", got)
	}
	if err := s.checkHeartbeats(ctx); err != nil {
		t.Fatal(err)
	}
	if n := countQ(t, s, `SELECT count(*) FROM incidents WHERE dedup_key = $1`, key); n != 0 {
		t.Fatalf("fresh heartbeat must not alert, got %d incidents", n)
	}

	// Positive: last heartbeat 10 minutes ago. Running the check twice must still give ONE incident and ONE alert.
	if _, err := s.pool.Exec(ctx, `UPDATE agents SET last_seen_at = now() - interval '10 minutes' WHERE id = $1::text::uuid`, ag.AgentID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.checkHeartbeats(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if n := countQ(t, s, `SELECT count(*) FROM incidents WHERE dedup_key = $1 AND severity = 'medium'`, key); n != 1 {
		t.Errorf("want 1 medium incident, got %d", n)
	}
	if n := countQ(t, s, `SELECT count(*) FROM alerts WHERE agent_id = $1::text::uuid AND rule_id = 'DET-005'`, ag.AgentID); n != 1 {
		t.Errorf("want exactly 1 alert (no repeats), got %d", n)
	}
}

func TestIncidentAPIAndTenantIsolation(t *testing.T) {
	s := integServer(t)
	ag, _ := enrolAgent(t, s, testTenantA, "test-host-incapi")
	cleanupDetections(t, s, ag.AgentID)
	run := uniq()
	runFixture(t, s, ag.Credential, "det001_positive.json", run, func(p map[string]any) { p["user"] = "u-" + run })

	var incID string
	if err := s.pool.QueryRow(context.Background(),
		`SELECT ie.incident_id::text FROM incident_events ie JOIN events e ON e.id = ie.event_id
		  WHERE e.agent_id = $1::text::uuid LIMIT 1`, ag.AgentID).Scan(&incID); err != nil {
		t.Fatalf("no incident was created: %v", err)
	}
	cookieA := sessionCookie(t, s, testTenantA, "analyst")
	cookieB := sessionCookie(t, s, testTenantB, "admin")

	list := doReq(s, "GET", "/v1/incidents?limit=200", "", "", cookieA)
	if list.Code != 200 || !strings.Contains(list.Body.String(), incID) {
		t.Errorf("tenant A analyst must see the incident (status %d)", list.Code)
	}
	detail := doReq(s, "GET", "/v1/incidents/"+incID, "", "", cookieA)
	if detail.Code != 200 || !strings.Contains(detail.Body.String(), "DET-001") {
		t.Fatalf("detail: want 200 with rule id, got %d", detail.Code)
	}
	var d struct {
		Events []json.RawMessage `json:"events"`
		Alerts []json.RawMessage `json:"alerts"`
	}
	_ = json.Unmarshal(detail.Body.Bytes(), &d)
	if len(d.Events) != 6 || len(d.Alerts) != 1 {
		t.Errorf("want 6 evidence events and 1 alert, got %d and %d", len(d.Events), len(d.Alerts))
	}

	// AT-03 for incidents: tenant B can neither list nor read it.
	listB := doReq(s, "GET", "/v1/incidents?limit=200", "", "", cookieB)
	if listB.Code != 200 || strings.Contains(listB.Body.String(), incID) {
		t.Errorf("tenant B must NOT see tenant A incidents (status %d)", listB.Code)
	}
	if got := doReq(s, "GET", "/v1/incidents/"+incID, "", "", cookieB).Code; got != 404 {
		t.Errorf("tenant B detail: want 404, got %d", got)
	}
	if got := doReq(s, "GET", "/v1/incidents/not-a-uuid", "", "", cookieA).Code; got != 404 {
		t.Errorf("malformed id: want 404, got %d", got)
	}
	if got := doReq(s, "GET", "/v1/incidents?status=bogus", "", "", cookieA).Code; got != 400 {
		t.Errorf("bad status filter: want 400, got %d", got)
	}
}
