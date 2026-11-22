package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestAnalysisAdapterHTTPS(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-adapter-token" {
			t.Error("adapter request is missing its expected method or credential")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
			!reflect.DeepEqual(body, map[string]any{"rule_id": "DET-001", "event_ids": []any{"e1"}}) {
			t.Error("adapter received fields outside the minimal evidence envelope")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/valid":
			_, _ = w.Write([]byte(`{"summary":"Review e1","confidence":"low","referenced_event_ids":["e1"],"proposed_next_steps":[]}`))
		case "/invalid":
			_, _ = w.Write([]byte(`{"summary":"Review unknown","confidence":"low","referenced_event_ids":["foreign"],"proposed_next_steps":[]}`))
		case "/redirect":
			http.Redirect(w, r, "/valid", http.StatusTemporaryRedirect)
		default:
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	defer server.Close()
	for _, tt := range []struct {
		path string
		ok   bool
	}{
		{"/valid", true}, {"/invalid", false}, {"/redirect", false}, {"/limited", false},
	} {
		result, err := requestModelAnalysis(context.Background(), server.URL+tt.path,
			"test-adapter-token", "DET-001", []string{"e1"}, server.Client().Transport)
		if tt.ok && (err != nil || result.Mode != "adapter" || !reflect.DeepEqual(result.ReferencedEventIDs, []string{"e1"})) {
			t.Fatalf("%s did not return grounded analysis: %v", tt.path, err)
		}
		if !tt.ok && err == nil {
			t.Fatalf("%s should return an error so the worker uses offline analysis", tt.path)
		}
	}
	if requests.Load() != 4 {
		t.Fatalf("adapter issued %d requests, want exactly 4 without following redirects", requests.Load())
	}
}
