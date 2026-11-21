package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fastClient(url string) *client {
	c := newClient(url, "cred")
	c.baseBackoff = time.Millisecond
	c.maxBackoff = 5 * time.Millisecond
	return c
}

func TestRetriesServerErrorsThenSucceeds(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) < 3 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(202)
	}))
	defer srv.Close()
	c := fastClient(srv.URL)
	status, _, err := c.postN(context.Background(), "/v1/events", map[string]string{"a": "b"}, c.maxAttempts)
	if err != nil || status != 202 || atomic.LoadInt32(&hits) != 3 {
		t.Fatalf("want 202 after 3 hits, got status=%d hits=%d err=%v", status, hits, err)
	}
}

func TestDoesNotRetryBadRequest(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(400)
	}))
	defer srv.Close()
	c := fastClient(srv.URL)
	status, _, err := c.postN(context.Background(), "/v1/events", map[string]string{}, c.maxAttempts)
	if err != nil || status != 400 || atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("want a single 400, got status=%d hits=%d err=%v", status, hits, err)
	}
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(500)
	}))
	defer srv.Close()
	c := fastClient(srv.URL)
	c.maxAttempts = 3
	_, _, err := c.postN(context.Background(), "/v1/events", map[string]string{}, c.maxAttempts)
	if err == nil || atomic.LoadInt32(&hits) != 3 {
		t.Fatalf("want error after exactly 3 hits, got hits=%d err=%v", hits, err)
	}
}

func TestSendsBearerCredential(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(202)
	}))
	defer srv.Close()
	c := fastClient(srv.URL)
	_, _, _ = c.postN(context.Background(), "/v1/events", map[string]string{}, 1)
	if got != "Bearer cred" {
		t.Errorf("want Bearer cred, got %q", got)
	}
}

func TestSubmitQueuesWhenServerIsDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // now nothing is listening
	a := &agent{c: fastClient(url), queuePath: filepath.Join(t.TempDir(), "q.jsonl")}
	a.c.maxAttempts = 2
	res, err := a.submit(context.Background(), outEvent{Kind: "auth.failed_login", OccurredAt: "2026-11-21T10:00:00Z", IdempotencyKey: "k1"})
	if err != nil || res != "queued" {
		t.Fatalf("want queued, got %q err=%v", res, err)
	}
	q, _ := readQueue(a.queuePath)
	if len(q) != 1 || q[0].IdempotencyKey != "k1" {
		t.Fatalf("event not in queue: %+v", q)
	}
}

func TestFlushSendsQueueAndRemovesFile(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(202)
	}))
	defer srv.Close()
	a := &agent{c: fastClient(srv.URL), queuePath: filepath.Join(t.TempDir(), "q.jsonl")}
	evs := []outEvent{{Kind: "a.b", OccurredAt: "2026-11-21T10:00:00Z", IdempotencyKey: "k1"}, {Kind: "a.b", OccurredAt: "2026-11-21T10:00:01Z", IdempotencyKey: "k2"}, {Kind: "a.b", OccurredAt: "2026-11-21T10:00:02Z", IdempotencyKey: "k3"}}
	if err := writeQueue(a.queuePath, evs); err != nil {
		t.Fatal(err)
	}
	n, err := a.flush(context.Background())
	if err != nil || n != 3 || atomic.LoadInt32(&hits) != 3 {
		t.Fatalf("want 3 sent, got n=%d hits=%d err=%v", n, hits, err)
	}
	if _, err := os.Stat(a.queuePath); !os.IsNotExist(err) {
		t.Error("queue file should be removed after a full flush")
	}
}

func TestFlushKeepsEventsWhenServerDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	a := &agent{c: fastClient(url), queuePath: filepath.Join(t.TempDir(), "q.jsonl")}
	_ = writeQueue(a.queuePath, []outEvent{{Kind: "a.b", OccurredAt: "2026-11-21T10:00:00Z", IdempotencyKey: "k1"}})
	n, err := a.flush(context.Background())
	if n != 0 || err != nil {
		t.Fatalf("want 0 sent and no error, got n=%d err=%v", n, err)
	}
	q, _ := readQueue(a.queuePath)
	if len(q) != 1 {
		t.Errorf("event must stay queued, got %d", len(q))
	}
}

func TestFlushStopsOnRevokedCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer srv.Close()
	a := &agent{c: fastClient(srv.URL), queuePath: filepath.Join(t.TempDir(), "q.jsonl")}
	_ = writeQueue(a.queuePath, []outEvent{{Kind: "a.b", OccurredAt: "2026-11-21T10:00:00Z", IdempotencyKey: "k1"}})
	_, err := a.flush(context.Background())
	if err != errUnauthorized {
		t.Fatalf("want errUnauthorized, got %v", err)
	}
	if q, _ := readQueue(a.queuePath); len(q) != 1 {
		t.Error("events must be kept when the credential is rejected")
	}
}

func TestPrepareRebasesAndKeepsSpacing(t *testing.T) {
	in := []outEvent{
		{Kind: "a.b", OccurredAt: "2026-11-20T10:00:00Z", IdempotencyKey: "k1"},
		{Kind: "a.b", OccurredAt: "2026-11-20T10:00:50Z", IdempotencyKey: "k2"},
	}
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	out, err := prepare(in, now, true, true, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	t0, _ := time.Parse(time.RFC3339, out[0].OccurredAt)
	t1, _ := time.Parse(time.RFC3339, out[1].OccurredAt)
	if t1.Sub(t0) != 50*time.Second {
		t.Errorf("spacing changed: %v", t1.Sub(t0))
	}
	if !t1.Equal(now) {
		t.Errorf("latest event should be now, got %v", t1)
	}
	if out[0].IdempotencyKey != "k1-abc123" {
		t.Errorf("unique key wrong: %s", out[0].IdempotencyKey)
	}
	if in[0].IdempotencyKey != "k1" {
		t.Error("input must not be modified")
	}
	same, _ := prepare(in, now, false, false, "x")
	if same[0].OccurredAt != in[0].OccurredAt || same[0].IdempotencyKey != "k1" {
		t.Error("with rebase and unique off, nothing should change")
	}
}

func TestPrepareRejectsBadTime(t *testing.T) {
	if _, err := prepare([]outEvent{{OccurredAt: "nope"}}, time.Now(), true, false, "x"); err == nil {
		t.Error("expected an error for a bad timestamp")
	}
}

func TestSanitizeHostname(t *testing.T) {
	if got := sanitizeHostname("My PC!"); got != "My-PC-" {
		t.Errorf("got %q", got)
	}
	if got := sanitizeHostname(""); got != "lab-agent" {
		t.Errorf("got %q", got)
	}
	if got := sanitizeHostname(strings.Repeat("a", 200)); len(got) != 100 {
		t.Errorf("len %d", len(got))
	}
}

func TestProjectFixturesLoad(t *testing.T) {
	want := map[string]int{"det001_positive.json": 6, "det001_benign_five.json": 5, "det001_benign_spread.json": 6, "det002_positive.json": 1, "det003_positive.json": 10}
	for name, n := range want {
		evs, err := loadFixture(filepath.Join("..", "..", "simulations", name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(evs) != n {
			t.Errorf("%s: want %d events, got %d", name, n, len(evs))
		}
	}
}
