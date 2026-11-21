package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"
)

var errUnauthorized = errors.New("credential rejected (agent revoked or invalid)")

type state struct {
	AgentID    string `json:"agent_id"`
	Credential string `json:"credential"`
	API        string `json:"api"`
}

func loadState(path string) (state, error) {
	var s state
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	if s.API == "" || s.Credential == "" {
		return s, errors.New("state file is incomplete; enrol again")
	}
	return s, nil
}

// The credential is a secret: the file is created with owner-only permissions.
func saveState(path string, s state) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

type outEvent struct {
	Kind           string          `json:"kind"`
	OccurredAt     string          `json:"occurred_at"`
	Severity       string          `json:"severity"`
	Payload        json.RawMessage `json:"payload"`
	IdempotencyKey string          `json:"idempotency_key"`
}

type client struct {
	api         string
	cred        string
	http        *http.Client
	maxAttempts int
	baseBackoff time.Duration
	maxBackoff  time.Duration
}

func newClient(api, cred string) *client {
	return &client{
		api: api, cred: cred,
		http:        &http.Client{Timeout: 10 * time.Second},
		maxAttempts: 5,
		baseBackoff: 500 * time.Millisecond,
		maxBackoff:  30 * time.Second,
	}
}

func (c *client) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.api+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cred != "" {
		req.Header.Set("Authorization", "Bearer "+c.cred)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b, nil
}

// postN retries network errors, 5xx and 429 with exponential backoff plus jitter.
// 2xx and other 4xx are returned immediately (retrying a bad request never helps).
func (c *client) postN(ctx context.Context, path string, body any, attempts int) (int, []byte, error) {
	delay := c.baseBackoff
	var status int
	var respBody []byte
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		var err error
		status, respBody, err = c.do(ctx, http.MethodPost, path, body)
		lastErr = err
		if err == nil && status != http.StatusTooManyRequests && status < 500 {
			return status, respBody, nil
		}
		if attempt == attempts {
			break
		}
		wait := delay + time.Duration(rand.Int63n(int64(delay)/2+1))
		select {
		case <-ctx.Done():
			return status, respBody, ctx.Err()
		case <-time.After(wait):
		}
		delay *= 2
		if delay > c.maxBackoff {
			delay = c.maxBackoff
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("server returned %d", status)
	}
	return status, respBody, lastErr
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

// ---------- offline queue (JSON lines) ----------

func readQueue(path string) ([]outEvent, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []outEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev outEvent
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		out = append(out, ev)
	}
	return out, sc.Err()
}

func writeQueue(path string, evs []outEvent) error {
	if len(evs) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	var buf bytes.Buffer
	for _, ev := range evs {
		b, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

const maxQueue = 10000

type agent struct {
	c         *client
	queuePath string
	mu        sync.Mutex
}

func (a *agent) enqueue(ev outEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	q, _ := readQueue(a.queuePath)
	if len(q) >= maxQueue {
		log.Printf("queue full (%d events), dropping %s", len(q), ev.IdempotencyKey)
		return
	}
	q = append(q, ev)
	if err := writeQueue(a.queuePath, q); err != nil {
		log.Printf("could not write queue: %v", err)
	}
}

// submit returns "sent", "queued" or "dropped". Only a 401 is returned as an error.
func (a *agent) submit(ctx context.Context, ev outEvent) (string, error) {
	status, body, err := a.c.postN(ctx, "/v1/events", ev, a.c.maxAttempts)
	switch {
	case err != nil:
		a.enqueue(ev)
		return "queued", nil
	case status == http.StatusAccepted:
		return "sent", nil
	case status == http.StatusUnauthorized:
		return "", errUnauthorized
	default:
		log.Printf("server rejected event %s (%d): %s", ev.IdempotencyKey, status, truncate(string(body)))
		return "dropped", nil
	}
}

// flush tries to send queued events. It stops at the first network failure and keeps the rest.
func (a *agent) flush(ctx context.Context) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	q, err := readQueue(a.queuePath)
	if err != nil || len(q) == 0 {
		return 0, err
	}
	sent := 0
	var remaining []outEvent
	var authErr error
	for i, ev := range q {
		status, body, perr := a.c.postN(ctx, "/v1/events", ev, 1)
		if perr != nil {
			remaining = append(remaining, q[i:]...)
			break
		}
		if status == http.StatusAccepted {
			sent++
			continue
		}
		if status == http.StatusUnauthorized {
			remaining = append(remaining, q[i:]...)
			authErr = errUnauthorized
			break
		}
		log.Printf("server rejected queued event %s (%d): %s", ev.IdempotencyKey, status, truncate(string(body)))
	}
	if werr := writeQueue(a.queuePath, remaining); werr != nil {
		return sent, werr
	}
	return sent, authErr
}
