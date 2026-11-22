package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Real-time push: an in-memory hub keeps one list of live browser connections PER TENANT.
// A message is only ever delivered to the connections of the tenant it was published for,
// so one tenant can never receive another tenant's notifications.

const (
	maxStreamsPerTenant = 50
	subBuffer           = 256
)

var streamPing = 15 * time.Second

type streamMsg struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

type subscriber struct {
	ch     chan []byte
	lagged atomic.Bool // set when a message had to be dropped because the browser was too slow
}

type hub struct {
	mu   sync.RWMutex
	subs map[string]map[*subscriber]struct{}
}

func newHub() *hub { return &hub{subs: map[string]map[*subscriber]struct{}{}} }

func (h *hub) subscribe(tenant string) (*subscriber, bool) {
	if h == nil {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.subs[tenant]
	if m == nil {
		m = map[*subscriber]struct{}{}
		h.subs[tenant] = m
	}
	if len(m) >= maxStreamsPerTenant {
		return nil, false
	}
	s := &subscriber{ch: make(chan []byte, subBuffer)}
	m[s] = struct{}{}
	return s, true
}

func (h *hub) unsubscribe(tenant string, s *subscriber) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs[tenant], s)
	if len(h.subs[tenant]) == 0 {
		delete(h.subs, tenant)
	}
}

// publish NEVER blocks: ingestion speed must not depend on a slow browser. Safe on a nil hub.
func (h *hub) publish(tenant, typ string, data map[string]any) {
	if h == nil {
		return
	}
	b, err := json.Marshal(streamMsg{Type: typ, Data: data})
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs[tenant] {
		select {
		case s.ch <- b:
		default:
			s.lagged.Store(true)
		}
	}
}

// GET /v1/stream (Server-Sent Events). Tenant comes ONLY from the session cookie.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	fl, ok := w.(http.Flusher)
	if s.hub == nil || !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "streaming unavailable"})
		return
	}
	sub, ok := s.hub.subscribe(sess.TenantID)
	if !ok {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many open streams"})
		return
	}
	defer s.hub.unsubscribe(sess.TenantID, sub)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n: connected\n\n")
	fl.Flush()

	ping := time.NewTicker(streamPing)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-sub.ch:
			if sub.lagged.Swap(false) {
				fmt.Fprint(w, "event: resync\ndata: {}\n\n") // tell the browser to reload its lists
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			fl.Flush()
		case <-ping.C:
			if time.Now().Unix() >= sess.Expires { // the login expired: end the stream, the browser must sign in again
				fmt.Fprint(w, "event: expired\ndata: {}\n\n")
				fl.Flush()
				return
			}
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}
