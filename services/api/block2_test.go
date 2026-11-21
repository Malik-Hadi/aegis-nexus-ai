package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const testTenantA = "11111111-1111-1111-1111-111111111111"
const testTenantB = "22222222-2222-2222-2222-222222222222"

// ---------- helpers ----------

func doReq(s *Server, method, path, body, bearer, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: cookie})
	}
	rr := httptest.NewRecorder()
	s.routes().ServeHTTP(rr, req)
	return rr
}

func sessionCookie(t *testing.T, s *Server, tenant, role string) string {
	t.Helper()
	tok, err := signSession(s.secret, Session{UserID: "u1", TenantID: tenant, Role: role, Subject: "tester-" + role, Expires: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func eventBody(key string) string {
	return fmt.Sprintf(`{"kind":"auth.failed_login","occurred_at":%q,"severity":"low","payload":{"user":"lab-user-01","source_ip":"192.0.2.10"},"idempotency_key":%q}`,
		time.Now().UTC().Format(time.RFC3339), key)
}

// ---------- unit tests (no database needed) ----------

func TestValidateEvent(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	ok := eventIn{Kind: "auth.failed_login", OccurredAt: "2026-11-21T11:59:00Z", Severity: "low", Payload: json.RawMessage(`{"user":"x"}`), IdempotencyKey: "fx-1"}
	if _, err := validateEvent(ok, now); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	cases := map[string]func(e eventIn) eventIn{
		"missing kind":       func(e eventIn) eventIn { e.Kind = ""; return e },
		"bad kind":           func(e eventIn) eventIn { e.Kind = "BAD KIND;DROP"; return e },
		"bad severity":       func(e eventIn) eventIn { e.Severity = "urgent"; return e },
		"bad time":           func(e eventIn) eventIn { e.OccurredAt = "yesterday"; return e },
		"future time":        func(e eventIn) eventIn { e.OccurredAt = "2026-11-21T13:00:00Z"; return e },
		"too old":            func(e eventIn) eventIn { e.OccurredAt = "2026-01-01T00:00:00Z"; return e },
		"missing key":        func(e eventIn) eventIn { e.IdempotencyKey = ""; return e },
		"key with spaces":    func(e eventIn) eventIn { e.IdempotencyKey = "a b"; return e },
		"payload is array":   func(e eventIn) eventIn { e.Payload = json.RawMessage(`[1,2]`); return e },
		"payload not json":   func(e eventIn) eventIn { e.Payload = json.RawMessage(`{bad`); return e },
		"payload too large":  func(e eventIn) eventIn { e.Payload = json.RawMessage(`{"a":"` + strings.Repeat("x", 20000) + `"}`); return e },
		"source_ref too big": func(e eventIn) eventIn { e.SourceRef = strings.Repeat("x", 200); return e },
	}
	for name, mutate := range cases {
		if _, err := validateEvent(mutate(ok), now); err == nil {
			t.Errorf("%s: expected rejection, got none", name)
		}
	}
}

func TestParseAgentCredential(t *testing.T) {
	good := "11111111-1111-1111-1111-111111111111." + strings.Repeat("a", 64)
	if _, _, ok := parseAgentCredential("Bearer " + good); !ok {
		t.Error("good credential rejected")
	}
	for _, bad := range []string{"", "Bearer", "Bearer x", "Basic " + good, "Bearer 1111." + strings.Repeat("a", 64), "Bearer 11111111-1111-1111-1111-111111111111.short"} {
		if _, _, ok := parseAgentCredential(bad); ok {
			t.Errorf("bad credential accepted: %q", bad)
		}
	}
}

func TestIngestWithoutValidCredentialGets401(t *testing.T) {
	s := testServer()
	if got := doReq(s, "POST", "/v1/events", eventBody("k1"), "", "").Code; got != 401 {
		t.Errorf("no credential: want 401, got %d", got)
	}
	if got := doReq(s, "POST", "/v1/events", eventBody("k1"), "garbage", "").Code; got != 401 {
		t.Errorf("garbage credential: want 401, got %d", got)
	}
	// A browser session cookie must NOT work as an agent credential.
	cookie := sessionCookie(t, s, testTenantA, "admin")
	if got := doReq(s, "POST", "/v1/events", eventBody("k1"), "", cookie).Code; got != 401 {
		t.Errorf("cookie on agent route: want 401, got %d", got)
	}
}

func TestAnonymousCannotReadEventsOrAgents(t *testing.T) {
	s := testServer()
	for _, p := range []string{"/v1/events", "/v1/agents"} {
		if got := doReq(s, "GET", p, "", "", "").Code; got != 401 {
			t.Errorf("%s: want 401, got %d", p, got)
		}
	}
}

func TestAnalystCannotRevokeOrCreateEnrolToken(t *testing.T) {
	s := testServer()
	cookie := sessionCookie(t, s, testTenantA, "analyst")
	if got := doReq(s, "POST", "/v1/agents/"+testTenantB+"/revoke", "", "", cookie).Code; got != 403 {
		t.Errorf("analyst revoke: want 403, got %d", got)
	}
	if got := doReq(s, "POST", "/v1/admin/enrolment-tokens", "", "", cookie).Code; got != 403 {
		t.Errorf("analyst create token: want 403, got %d", got)
	}
}

// ---------- integration tests (need the database from docker compose) ----------

func integServer(t *testing.T) *Server {
	t.Helper()
	loadEnv("../../.env")
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Skip("cannot create pool: " + err.Error())
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skip("database not reachable: " + err.Error())
	}
	t.Cleanup(pool.Close)
	return &Server{pool: pool, secret: []byte("test-secret-test-secret-test-secret-1234"), dummyHash: []byte("x")}
}

type enrolResp struct {
	AgentID    string `json:"agent_id"`
	Credential string `json:"credential"`
}

// enrolAgent creates a one-time token directly in the DB, then enrols through the real endpoint.
func enrolAgent(t *testing.T, s *Server, tenant, host string) (enrolResp, string) {
	t.Helper()
	raw, err := newSecret()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = s.pool.Exec(ctx, `INSERT INTO enrolment_tokens (tenant_id, token_hash, expires_at)
		VALUES ($1::text::uuid, $2, now() + interval '1 hour')`, tenant, hashSecret(raw))
	if err != nil {
		t.Fatalf("insert token (did you apply migration 003?): %v", err)
	}
	body := fmt.Sprintf(`{"token":%q,"hostname":%q,"os":"linux"}`, raw, host)
	rr := doReq(s, "POST", "/v1/agents/enrol", body, "", "")
	if rr.Code != 201 {
		t.Fatalf("enrol: want 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var er enrolResp
	if err := json.Unmarshal(rr.Body.Bytes(), &er); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM events WHERE agent_id = $1::text::uuid`, er.AgentID)
		_, _ = s.pool.Exec(ctx, `DELETE FROM agents WHERE id = $1::text::uuid`, er.AgentID)
	})
	return er, raw
}

func TestEnrolIngestAndDuplicate(t *testing.T) {
	s := integServer(t)
	ag, rawToken := enrolAgent(t, s, testTenantA, "test-host-enrol")

	// The one-time token cannot be used twice.
	reuse := fmt.Sprintf(`{"token":%q,"hostname":"test-host-again","os":"linux"}`, rawToken)
	if got := doReq(s, "POST", "/v1/agents/enrol", reuse, "", "").Code; got != 401 {
		t.Errorf("token reuse: want 401, got %d", got)
	}

	key, _ := newSecret()
	key = key[:16]
	if got := doReq(s, "POST", "/v1/events", eventBody(key), ag.Credential, "").Code; got != 202 {
		t.Fatalf("ingest: want 202, got %d", got)
	}
	rr := doReq(s, "POST", "/v1/events", eventBody(key), ag.Credential, "")
	if rr.Code != 202 || !strings.Contains(rr.Body.String(), "duplicate") {
		t.Errorf("repeat: want 202 duplicate, got %d %s", rr.Code, rr.Body.String())
	}
	var n int
	if err := s.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM events WHERE agent_id = $1::text::uuid AND idempotency_key = $2`, ag.AgentID, key).Scan(&n); err != nil || n != 1 {
		t.Errorf("want exactly 1 stored event, got %d (err %v)", n, err)
	}
	// Bad data is rejected with 400 and nothing is stored.
	if got := doReq(s, "POST", "/v1/events", `{"kind":"NOPE"}`, ag.Credential, "").Code; got != 400 {
		t.Errorf("invalid event: want 400, got %d", got)
	}
	// Analyst of the same tenant can read it.
	cookie := sessionCookie(t, s, testTenantA, "analyst")
	list := doReq(s, "GET", "/v1/events?limit=200", "", "", cookie)
	if list.Code != 200 || !strings.Contains(list.Body.String(), ag.AgentID) {
		t.Errorf("tenant A should see its event, got %d", list.Code)
	}
}

// AT-03: Tenant B can never list, read, or change Tenant A's data.
func TestTenantIsolation(t *testing.T) {
	s := integServer(t)
	ag, _ := enrolAgent(t, s, testTenantA, "test-host-tenant-a")
	key, _ := newSecret()
	if got := doReq(s, "POST", "/v1/events", eventBody(key[:16]), ag.Credential, "").Code; got != 202 {
		t.Fatalf("ingest: want 202, got %d", got)
	}

	cookieB := sessionCookie(t, s, testTenantB, "admin")
	evB := doReq(s, "GET", "/v1/events?limit=200", "", "", cookieB)
	if evB.Code != 200 || strings.Contains(evB.Body.String(), ag.AgentID) {
		t.Errorf("tenant B must NOT see tenant A events (status %d)", evB.Code)
	}
	agB := doReq(s, "GET", "/v1/agents", "", "", cookieB)
	if agB.Code != 200 || strings.Contains(agB.Body.String(), ag.AgentID) {
		t.Errorf("tenant B must NOT see tenant A agents (status %d)", agB.Code)
	}
	// Tenant B admin tries to revoke tenant A's agent: looks like "not found" and changes nothing.
	if got := doReq(s, "POST", "/v1/agents/"+ag.AgentID+"/revoke", "", "", cookieB).Code; got != 404 {
		t.Errorf("cross-tenant revoke: want 404, got %d", got)
	}
	var st string
	_ = s.pool.QueryRow(context.Background(), `SELECT status FROM agents WHERE id = $1::text::uuid`, ag.AgentID).Scan(&st)
	if st != "active" {
		t.Errorf("tenant A agent status changed to %q by tenant B", st)
	}
	// Tenant A still sees its own agent.
	cookieA := sessionCookie(t, s, testTenantA, "analyst")
	agA := doReq(s, "GET", "/v1/agents", "", "", cookieA)
	if !strings.Contains(agA.Body.String(), ag.AgentID) {
		t.Errorf("tenant A should see its own agent")
	}
}

// AT-06: a revoked agent is denied on the next ingestion.
func TestRevokedAgentIsRejected(t *testing.T) {
	s := integServer(t)
	ag, _ := enrolAgent(t, s, testTenantA, "test-host-revoke")
	k1, _ := newSecret()
	k2, _ := newSecret()
	if got := doReq(s, "POST", "/v1/events", eventBody(k1[:16]), ag.Credential, "").Code; got != 202 {
		t.Fatalf("before revoke: want 202, got %d", got)
	}
	analyst := sessionCookie(t, s, testTenantA, "analyst")
	if got := doReq(s, "POST", "/v1/agents/"+ag.AgentID+"/revoke", "", "", analyst).Code; got != 403 {
		t.Errorf("analyst revoke: want 403, got %d", got)
	}
	admin := sessionCookie(t, s, testTenantA, "admin")
	if got := doReq(s, "POST", "/v1/agents/"+ag.AgentID+"/revoke", "", "", admin).Code; got != 200 {
		t.Fatalf("admin revoke: want 200, got %d", got)
	}
	if got := doReq(s, "POST", "/v1/events", eventBody(k2[:16]), ag.Credential, "").Code; got != 401 {
		t.Errorf("after revoke: want 401, got %d", got)
	}
	if got := doReq(s, "POST", "/v1/agents/heartbeat", "", ag.Credential, "").Code; got != 401 {
		t.Errorf("heartbeat after revoke: want 401, got %d", got)
	}
}
