package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ---------- unit tests ----------

func TestTransitionTable(t *testing.T) {
	ok := [][2]string{{"open", "investigating"}, {"open", "resolved"}, {"investigating", "resolved"}, {"investigating", "open"}, {"resolved", "closed"}, {"resolved", "investigating"}}
	for _, c := range ok {
		if !canTransition(c[0], c[1]) {
			t.Errorf("%s -> %s should be allowed", c[0], c[1])
		}
	}
	bad := [][2]string{{"open", "closed"}, {"investigating", "closed"}, {"closed", "open"}, {"closed", "investigating"}, {"open", "open"}}
	for _, c := range bad {
		if canTransition(c[0], c[1]) {
			t.Errorf("%s -> %s must be refused", c[0], c[1])
		}
	}
}

func TestOnlyAdminCanClose(t *testing.T) {
	if canActorTransition("analyst", "closed") || !canActorTransition("admin", "closed") || !canActorTransition("analyst", "resolved") {
		t.Error("closing requires admin; other transitions are open to analysts")
	}
}

func TestLifecycleRoutesNeedLogin(t *testing.T) {
	s := testServer()
	id := "00000000-0000-0000-0000-000000000001"
	if got := doReq(s, "PATCH", "/v1/incidents/"+id, `{"status":"investigating"}`, "", "").Code; got != 401 {
		t.Errorf("anonymous PATCH: want 401, got %d", got)
	}
	if got := doReq(s, "POST", "/v1/incidents/"+id+"/approvals", `{"action":"isolate_host"}`, "", "").Code; got != 401 {
		t.Errorf("anonymous approval request: want 401, got %d", got)
	}
	if got := doReq(s, "POST", "/v1/approvals/"+id+"/decision", `{"decision":"approved"}`, "", "").Code; got != 401 {
		t.Errorf("anonymous decision: want 401, got %d", got)
	}
	analyst := sessionCookie(t, s, testTenantA, "analyst")
	if got := doReq(s, "POST", "/v1/approvals/"+id+"/decision", `{"decision":"approved"}`, "", analyst).Code; got != 403 {
		t.Errorf("analyst deciding an approval: want 403, got %d", got)
	}
}

// ---------- integration: AT-08 (lifecycle audited) and AT-10 (containment needs approval) ----------

func insertUser(t *testing.T, s *Server, tenant, subject, role string) string {
	t.Helper()
	var id string
	err := s.pool.QueryRow(context.Background(),
		`INSERT INTO users (tenant_id, subject, role) VALUES ($1::text::uuid, $2, $3) RETURNING id::text`, tenant, subject, role).Scan(&id)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func userCookie(t *testing.T, s *Server, tenant, userID, role, subject string) string {
	t.Helper()
	tok, err := signSession(s.secret, Session{UserID: userID, TenantID: tenant, Role: role, Subject: subject, Expires: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func cleanupApprovals(t *testing.T, s *Server, agentID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		sub := `(SELECT ie.incident_id FROM incident_events ie JOIN events e ON e.id = ie.event_id WHERE e.agent_id = $1::text::uuid)`
		_, _ = s.pool.Exec(ctx, `DELETE FROM approvals WHERE incident_id IN `+sub, agentID)
		_, _ = s.pool.Exec(ctx, `UPDATE incidents SET owner_id = NULL WHERE id IN `+sub, agentID)
	})
}

func jsonField(t *testing.T, body, field string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("not json: %s", body)
	}
	v, _ := m[field].(string)
	return v
}

func TestIncidentLifecycleAndApprovalFlow(t *testing.T) {
	s := integServer(t)
	s.hub = newHub()
	ag, _ := enrolAgent(t, s, testTenantA, "test-host-life")
	cleanupDetections(t, s, ag.AgentID)
	run := uniq()
	analystID := insertUser(t, s, testTenantA, "t-an-"+run, "analyst")
	admin1ID := insertUser(t, s, testTenantA, "t-ad1-"+run, "admin")
	admin2ID := insertUser(t, s, testTenantA, "t-ad2-"+run, "admin")
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1::text[]::uuid[])`, []string{analystID, admin1ID, admin2ID})
	})
	cleanupApprovals(t, s, ag.AgentID) // registered last, so it runs first

	runFixture(t, s, ag.Credential, "det001_positive.json", run, func(p map[string]any) { p["user"] = "u-" + run })
	var incID string
	if err := s.pool.QueryRow(context.Background(),
		`SELECT ie.incident_id::text FROM incident_events ie JOIN events e ON e.id = ie.event_id WHERE e.agent_id = $1::text::uuid LIMIT 1`,
		ag.AgentID).Scan(&incID); err != nil {
		t.Fatalf("no incident: %v", err)
	}
	an := userCookie(t, s, testTenantA, analystID, "analyst", "t-an-"+run)
	ad1 := userCookie(t, s, testTenantA, admin1ID, "admin", "t-ad1-"+run)
	ad2 := userCookie(t, s, testTenantA, admin2ID, "admin", "t-ad2-"+run)
	other := sessionCookie(t, s, testTenantB, "admin")
	patch := func(cookie, body string) int { return doReq(s, "PATCH", "/v1/incidents/"+incID, body, "", cookie).Code }
	status := func() string {
		var st string
		_ = s.pool.QueryRow(context.Background(), `SELECT status FROM incidents WHERE id = $1::text::uuid`, incID).Scan(&st)
		return st
	}

	// --- lifecycle (AT-08) ---
	if got := patch(other, `{"status":"investigating"}`); got != 404 {
		t.Errorf("another tenant's PATCH: want 404, got %d", got)
	}
	if got := patch(an, `{"status":"nonsense"}`); got != 400 {
		t.Errorf("invalid status: want 400, got %d", got)
	}
	if got := patch(an, `{"status":"investigating"}`); got != 200 {
		t.Fatalf("open -> investigating: want 200, got %d", got)
	}
	if got := patch(an, `{"status":"investigating"}`); got != 409 {
		t.Errorf("same status: want 409, got %d", got)
	}
	if got := patch(an, `{"status":"closed"}`); got != 409 {
		t.Errorf("investigating -> closed: want 409, got %d", got)
	}
	if got := patch(an, `{"owner":"me"}`); got != 200 {
		t.Errorf("analyst assigns self: want 200, got %d", got)
	}
	if got := patch(an, fmt.Sprintf(`{"owner":%q}`, admin1ID)); got != 403 {
		t.Errorf("analyst assigns someone else: want 403, got %d", got)
	}
	if got := patch(ad1, fmt.Sprintf(`{"owner":%q}`, analystID)); got != 200 {
		t.Errorf("admin assigns analyst: want 200, got %d", got)
	}
	if got := patch(ad1, `{"owner":"00000000-0000-0000-0000-0000000000aa"}`); got != 400 {
		t.Errorf("assign to unknown user: want 400, got %d", got)
	}

	// --- approvals (AT-10): denied by default, requester cannot self-approve, denial changes nothing ---
	if got := doReq(s, "POST", "/v1/incidents/"+incID+"/approvals", `{"action":"rm_rf"}`, "", an).Code; got != 400 {
		t.Errorf("unknown action: want 400, got %d", got)
	}
	if got := doReq(s, "POST", "/v1/incidents/"+incID+"/approvals", `{"action":"isolate_host"}`, "", other).Code; got != 404 {
		t.Errorf("other tenant request: want 404, got %d", got)
	}
	rq := doReq(s, "POST", "/v1/incidents/"+incID+"/approvals", `{"action":"isolate_host"}`, "", an)
	if rq.Code != 201 {
		t.Fatalf("analyst requests isolation: want 201, got %d %s", rq.Code, rq.Body.String())
	}
	appr1 := jsonField(t, rq.Body.String(), "approval_id")
	executed := func(id string) int {
		return countQ(t, s, `SELECT count(*) FROM approvals WHERE id = $1::text::uuid AND executed_at IS NOT NULL`, id)
	}
	simulations := func() int {
		return countQ(t, s, `SELECT count(*) FROM audit_logs WHERE target = $1 AND action = 'containment.simulated'`, incID)
	}
	if executed(appr1) != 0 || simulations() != 0 {
		t.Fatal("a pending request must not execute anything")
	}
	if got := doReq(s, "POST", "/v1/incidents/"+incID+"/approvals", `{"action":"block_ip"}`, "", an).Code; got != 409 {
		t.Errorf("second pending request: want 409, got %d", got)
	}
	if got := doReq(s, "POST", "/v1/approvals/"+appr1+"/decision", `{"decision":"approved"}`, "", an).Code; got != 403 {
		t.Errorf("analyst approving: want 403, got %d", got)
	}
	if got := doReq(s, "POST", "/v1/approvals/"+appr1+"/decision", `{"decision":"approved"}`, "", other).Code; got != 404 {
		t.Errorf("other tenant approving: want 404, got %d", got)
	}
	before := status()
	if got := doReq(s, "POST", "/v1/approvals/"+appr1+"/decision", `{"decision":"denied"}`, "", ad1).Code; got != 200 {
		t.Fatalf("admin denies: want 200, got %d", got)
	}
	if executed(appr1) != 0 || simulations() != 0 || status() != before {
		t.Error("a DENIED request must leave everything unchanged")
	}
	if got := doReq(s, "POST", "/v1/approvals/"+appr1+"/decision", `{"decision":"approved"}`, "", ad2).Code; got != 409 {
		t.Errorf("deciding twice: want 409, got %d", got)
	}

	rq2 := doReq(s, "POST", "/v1/incidents/"+incID+"/approvals", `{"action":"isolate_host"}`, "", ad1)
	if rq2.Code != 201 {
		t.Fatalf("admin1 requests: want 201, got %d", rq2.Code)
	}
	appr2 := jsonField(t, rq2.Body.String(), "approval_id")
	if got := doReq(s, "POST", "/v1/approvals/"+appr2+"/decision", `{"decision":"approved"}`, "", ad1).Code; got != 403 {
		t.Errorf("requester approving own request: want 403, got %d", got)
	}
	if executed(appr2) != 0 {
		t.Fatal("self-approval must not execute")
	}
	ap := doReq(s, "POST", "/v1/approvals/"+appr2+"/decision", `{"decision":"approved"}`, "", ad2)
	if ap.Code != 200 || !strings.Contains(ap.Body.String(), "SIMULATED") {
		t.Fatalf("second admin approves: want 200 with simulation note, got %d %s", ap.Code, ap.Body.String())
	}
	if executed(appr2) != 1 || simulations() != 1 {
		t.Error("an approved request must record exactly one simulated execution")
	}

	// --- detail shows approvals and audit trail ---
	d := doReq(s, "GET", "/v1/incidents/"+incID, "", "", an)
	var detail struct {
		Approvals []json.RawMessage `json:"approvals"`
		Audit     []json.RawMessage `json:"audit_trail"`
	}
	_ = json.Unmarshal(d.Body.Bytes(), &detail)
	if d.Code != 200 || len(detail.Approvals) != 2 || len(detail.Audit) < 6 || !strings.Contains(d.Body.String(), "containment.simulated") {
		t.Errorf("detail must list 2 approvals and the audit trail, got %d approvals, %d audit rows", len(detail.Approvals), len(detail.Audit))
	}
	if got := doReq(s, "GET", "/v1/incidents/"+incID, "", "", other).Code; got != 404 {
		t.Errorf("other tenant detail: want 404, got %d", got)
	}

	// --- closing needs an admin; closed incidents are final ---
	if got := patch(an, `{"status":"resolved"}`); got != 200 {
		t.Fatalf("investigating -> resolved: want 200, got %d", got)
	}
	if got := patch(an, `{"status":"closed"}`); got != 403 {
		t.Errorf("analyst closing: want 403, got %d", got)
	}
	if got := patch(ad2, `{"status":"closed"}`); got != 200 {
		t.Errorf("admin closing: want 200, got %d", got)
	}
	if got := patch(ad2, `{"status":"investigating"}`); got != 409 {
		t.Errorf("reopening a closed incident: want 409, got %d", got)
	}
	if got := doReq(s, "POST", "/v1/incidents/"+incID+"/approvals", `{"action":"isolate_host"}`, "", ad1).Code; got != 409 {
		t.Errorf("approval on a closed incident: want 409, got %d", got)
	}

	// --- every status change is recorded with who and when (AT-08): investigating, resolved, closed ---
	n := countQ(t, s, `SELECT count(*) FROM audit_logs WHERE target = $1 AND action = 'incident.status' AND result = 'success' AND actor <> ''`, incID)
	if n != 3 {
		t.Errorf("want 3 audited status changes, got %d", n)
	}
	if countQ(t, s, `SELECT count(*) FROM audit_logs WHERE target = $1 AND action IN ('approval.self_decision','authz.denied')`, incID) < 2 {
		t.Error("denied attempts must be audited")
	}
}
