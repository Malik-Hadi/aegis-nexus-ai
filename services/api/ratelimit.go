package main

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type requestWindow struct {
	start time.Time
	count int
}

// Separate login and ingestion budgets. Use the connection peer rather than
// trusting client-supplied forwarding headers. Bound memory even under churn.
func limitRequests(next http.Handler) http.Handler {
	var mu sync.Mutex
	windows := make(map[string]requestWindow)
	var lastSweep time.Time
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var limit int
		var interval time.Duration
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/login":
			limit, interval = 60, time.Minute
		case r.Method == http.MethodPost && r.URL.Path == "/v1/events":
			limit, interval = 250, time.Second
		default:
			next.ServeHTTP(w, r)
			return
		}
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			peer = "unknown"
		}
		key := r.URL.Path + "|" + peer
		now := time.Now()
		mu.Lock()
		if now.Sub(lastSweep) >= time.Minute {
			for k, entry := range windows {
				if now.Sub(entry.start) >= time.Minute {
					delete(windows, k)
				}
			}
			lastSweep = now
		}
		entry, exists := windows[key]
		if now.Sub(entry.start) >= interval {
			entry = requestWindow{start: now}
		}
		allowed := entry.count < limit && (exists || len(windows) < 8192)
		if allowed {
			entry.count++
			windows[key] = entry
		}
		mu.Unlock()
		if !allowed {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "request limit exceeded"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
