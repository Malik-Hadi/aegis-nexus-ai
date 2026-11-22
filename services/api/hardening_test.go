package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestRateLimitIsolatesSourceAndRoute(t *testing.T) {
	handler := limitRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := func(peer, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = peer
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	for i := 0; i < 60; i++ {
		if got := request("127.0.0.1:1000", "/v1/auth/login").Code; got != http.StatusNoContent {
			t.Fatalf("attempt %d: status %d", i, got)
		}
	}
	rejected := request("127.0.0.1:2000", "/v1/auth/login")
	if rejected.Code != http.StatusTooManyRequests || rejected.Header().Get("Retry-After") == "" {
		t.Fatalf("expected retryable 429, got %d", rejected.Code)
	}
	for _, allowed := range []*httptest.ResponseRecorder{
		request("127.0.0.2:1000", "/v1/auth/login"),
		request("127.0.0.1:1000", "/v1/events"),
	} {
		if allowed.Code != http.StatusNoContent {
			t.Fatalf("separate budget was throttled: %d", allowed.Code)
		}
	}
}

func TestOfflineAnalysisReferencesOnlySuppliedEvidence(t *testing.T) {
	expected := []string{"event-1", "event-2"}
	result := offlineAnalysis("DET-001", expected)
	if !reflect.DeepEqual(result.ReferencedEventIDs, expected) || result.Mode != "offline" {
		t.Fatalf("unexpected result: %+v", result)
	}
	empty := offlineAnalysis("DET-005", nil)
	if empty.ReferencedEventIDs == nil || len(empty.ReferencedEventIDs) != 0 || empty.Confidence != "low" {
		t.Fatalf("unexpected heartbeat result: %+v", empty)
	}
}

func TestAnalyseRequiresSession(t *testing.T) {
	router := (&Server{}).routes()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/incidents/123/analyse", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestModelAnalysisRejectsEvidenceOutsideIncident(t *testing.T) {
	valid := []byte(`{"summary":"Review the recorded event","confidence":"low","referenced_event_ids":["known"],"proposed_next_steps":[]}`)
	result, err := validateAnalysis(valid, []string{"known"})
	if err != nil || result.Mode != "adapter" {
		t.Fatalf("valid model result: %+v, %v", result, err)
	}
	if _, err := validateAnalysis(valid, []string{"another"}); err == nil {
		t.Fatal("accepted an evidence ID outside the incident")
	}
	t.Setenv("SOC_ANALYST_URL", "")
	if _, err := modelAnalysis(context.Background(), "DET-001", nil); err == nil {
		t.Fatal("expected fallback when adapter is missing")
	}
}
