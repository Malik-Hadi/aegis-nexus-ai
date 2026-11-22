package main

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------- hub unit tests ----------

func TestHubDeliversOnlyToOwnTenant(t *testing.T) {
	h := newHub()
	a, _ := h.subscribe("A")
	b, _ := h.subscribe("B")
	h.publish("A", "event.new", map[string]any{"id": "1"})
	select {
	case msg := <-a.ch:
		if !strings.Contains(string(msg), `"type":"event.new"`) {
			t.Errorf("unexpected message %s", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("tenant A did not receive its message")
	}
	select {
	case msg := <-b.ch:
		t.Fatalf("tenant B must NOT receive tenant A messages, got %s", msg)
	default:
	}
}

func TestHubPublishNeverBlocks(t *testing.T) {
	h := newHub()
	slow, _ := h.subscribe("A") // nobody reads from it
	done := make(chan struct{})
	go func() {
		for i := 0; i < 5000; i++ {
			h.publish("A", "event.new", map[string]any{"i": i})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("publish blocked on a slow subscriber")
	}
	if !slow.lagged.Load() {
		t.Error("a subscriber that fell behind must be marked lagged so the browser can resync")
	}
}

func TestHubLimitsStreamsPerTenant(t *testing.T) {
	h := newHub()
	for i := 0; i < maxStreamsPerTenant; i++ {
		if _, ok := h.subscribe("A"); !ok {
			t.Fatalf("subscriber %d should be accepted", i)
		}
	}
	if _, ok := h.subscribe("A"); ok {
		t.Error("stream over the per-tenant limit must be refused")
	}
	if _, ok := h.subscribe("B"); !ok {
		t.Error("another tenant must not be affected by A's limit")
	}
}

func TestHubUnsubscribeStopsDelivery(t *testing.T) {
	h := newHub()
	a, _ := h.subscribe("A")
	h.unsubscribe("A", a)
	h.publish("A", "x", nil)
	select {
	case <-a.ch:
		t.Error("unsubscribed stream must not receive messages")
	default:
	}
}

func TestHubNilIsSafe(t *testing.T) {
	var h *hub
	h.publish("A", "x", nil) // must not panic: tests and tools build a Server without a hub
	if _, ok := h.subscribe("A"); ok {
		t.Error("nil hub cannot accept subscribers")
	}
}

func TestStreamRequiresLogin(t *testing.T) {
	s := testServer()
	s.hub = newHub()
	if got := doReq(s, "GET", "/v1/stream", "", "", "").Code; got != 401 {
		t.Errorf("anonymous stream: want 401, got %d", got)
	}
}

// ---------- real HTTP streaming tests (no database) ----------

type stream struct {
	lines  chan string
	cancel context.CancelFunc
}

func openStream(t *testing.T, base, cookie string) *stream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/v1/stream", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: cookie})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		cancel()
		t.Fatalf("stream: want 200, got %d", resp.StatusCode)
	}
	st := &stream{lines: make(chan string, 512), cancel: cancel}
	connected := make(chan struct{})
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		seen := false
		for sc.Scan() {
			line := sc.Text()
			if line == ": connected" && !seen {
				seen = true
				close(connected)
			}
			select {
			case st.lines <- line:
			default:
			}
		}
	}()
	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("stream did not connect in 2 seconds")
	}
	t.Cleanup(cancel)
	return st
}

func (st *stream) waitFor(substr string, d time.Duration) bool {
	deadline := time.After(d)
	for {
		select {
		case l := <-st.lines:
			if strings.Contains(l, substr) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func TestStreamPushesLiveAndIsolatesTenants(t *testing.T) {
	s := testServer()
	s.hub = newHub()
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close) // registered first, so it runs after the streams are cancelled
	a := openStream(t, srv.URL, sessionCookie(t, s, testTenantA, "analyst"))
	b := openStream(t, srv.URL, sessionCookie(t, s, testTenantB, "analyst"))

	start := time.Now()
	s.hub.publish(testTenantA, "incident.created", map[string]any{"id": "inc-1", "rule_id": "DET-001"})
	if !a.waitFor(`"type":"incident.created"`, 2*time.Second) {
		t.Fatal("tenant A did not receive the live message")
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("live delivery took %v, want under 500ms", d)
	}
	if b.waitFor("incident.created", 400*time.Millisecond) {
		t.Error("tenant B must NOT receive tenant A's live message")
	}
}

func TestStreamEndsWhenLoginExpires(t *testing.T) {
	old := streamPing
	streamPing = 100 * time.Millisecond
	defer func() { streamPing = old }()

	s := testServer()
	s.hub = newHub()
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	tok, err := signSession(s.secret, Session{UserID: "u1", TenantID: testTenantA, Role: "analyst", Subject: "x", Expires: time.Now().Add(2 * time.Second).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	st := openStream(t, srv.URL, tok)
	if !st.waitFor("event: expired", 6*time.Second) {
		t.Error("stream must end with an 'expired' event after the session expires")
	}
}

func TestStreamKeepsAliveWithPings(t *testing.T) {
	old := streamPing
	streamPing = 80 * time.Millisecond
	defer func() { streamPing = old }()
	s := testServer()
	s.hub = newHub()
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	st := openStream(t, srv.URL, sessionCookie(t, s, testTenantA, "analyst"))
	if !st.waitFor(": ping", 2*time.Second) {
		t.Error("expected keep-alive pings")
	}
}

// ---------- live pipeline with the database: ingest -> rule -> stream ----------

func TestLivePipelineFromAgentToStream(t *testing.T) {
	s := integServer(t)
	s.hub = newHub()
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)

	ag, _ := enrolAgent(t, s, testTenantA, "test-host-live")
	cleanupDetections(t, s, ag.AgentID)
	a := openStream(t, srv.URL, sessionCookie(t, s, testTenantA, "analyst"))
	b := openStream(t, srv.URL, sessionCookie(t, s, testTenantB, "analyst"))

	var evts int32
	_ = evts
	run := uniq()
	start := time.Now()
	runFixture(t, s, ag.Credential, "det001_positive.json", run, func(p map[string]any) { p["user"] = "u-" + run })
	if !a.waitFor(`"type":"event.new"`, 2*time.Second) {
		t.Error("tenant A analyst should see event.new live")
	}
	if !a.waitFor(`"type":"incident.created"`, 2*time.Second) {
		t.Error("tenant A analyst should see incident.created live")
	}
	atomic.AddInt32(&evts, 1)
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("fixture to live incident took %v, expected a few seconds at most", d)
	}
	if b.waitFor(`"type":"incident.created"`, 500*time.Millisecond) {
		t.Error("tenant B must NOT see tenant A's incident live")
	}
}
