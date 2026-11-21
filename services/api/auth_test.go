package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testServer() *Server {
	return &Server{secret: []byte("test-secret-test-secret-test-secret-1234")}
}

func tokenFor(t *testing.T, secret []byte, role string, exp time.Time) string {
	t.Helper()
	tok, err := signSession(secret, Session{UserID: "u1", TenantID: "t1", Role: role, Subject: "tester", Expires: exp.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func status(s *Server, path, token string) int {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	}
	rr := httptest.NewRecorder()
	s.routes().ServeHTTP(rr, req)
	return rr.Code
}

func TestAnonymousGets401(t *testing.T) {
	s := testServer()
	for _, p := range []string{"/v1/me", "/v1/admin/ping"} {
		if got := status(s, p, ""); got != 401 {
			t.Errorf("%s: want 401, got %d", p, got)
		}
	}
}

func TestAnalystOnAdminRouteGets403(t *testing.T) {
	s := testServer()
	tok := tokenFor(t, s.secret, "analyst", time.Now().Add(time.Hour))
	if got := status(s, "/v1/admin/ping", tok); got != 403 {
		t.Errorf("want 403, got %d", got)
	}
}

func TestAnalystCanUseNormalRoute(t *testing.T) {
	s := testServer()
	tok := tokenFor(t, s.secret, "analyst", time.Now().Add(time.Hour))
	if got := status(s, "/v1/me", tok); got != 200 {
		t.Errorf("want 200, got %d", got)
	}
}

func TestAdminGetsAdminRoute(t *testing.T) {
	s := testServer()
	tok := tokenFor(t, s.secret, "admin", time.Now().Add(time.Hour))
	if got := status(s, "/v1/admin/ping", tok); got != 200 {
		t.Errorf("want 200, got %d", got)
	}
}

func TestExpiredCookieGets401(t *testing.T) {
	s := testServer()
	tok := tokenFor(t, s.secret, "admin", time.Now().Add(-time.Minute))
	if got := status(s, "/v1/me", tok); got != 401 {
		t.Errorf("want 401, got %d", got)
	}
}

func TestWrongSecretGets401(t *testing.T) {
	s := testServer()
	tok := tokenFor(t, []byte("some-other-secret-some-other-secret-99"), "admin", time.Now().Add(time.Hour))
	if got := status(s, "/v1/me", tok); got != 401 {
		t.Errorf("want 401, got %d", got)
	}
}

// An analyst tries to upgrade themselves by pasting the admin payload onto their own signature.
func TestForgedRoleGets401(t *testing.T) {
	s := testServer()
	exp := time.Now().Add(time.Hour)
	admin := strings.Split(tokenFor(t, s.secret, "admin", exp), ".")
	analyst := strings.Split(tokenFor(t, s.secret, "analyst", exp), ".")
	forged := admin[0] + "." + analyst[1]
	if got := status(s, "/v1/admin/ping", forged); got != 401 {
		t.Errorf("want 401, got %d", got)
	}
}
