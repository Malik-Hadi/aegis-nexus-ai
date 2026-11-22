package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func reqWith(s *Server, method, path, body string, headers map[string]string, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: cookie})
	}
	rr := httptest.NewRecorder()
	s.routes().ServeHTTP(rr, req)
	return rr
}

func TestDashboardFilesAreServed(t *testing.T) {
	useFallback(t)
	s := testServer()
	cases := []struct{ path, ctype, contains string }{
		{"/", "text/html", "AEGIS"},
		{"/ui/app.js", "text/javascript", "EventSource"},
		{"/ui/style.css", "text/css", "--accent"},
	}
	for _, c := range cases {
		rr := doReq(s, "GET", c.path, "", "", "")
		if rr.Code != 200 || !strings.HasPrefix(rr.Header().Get("Content-Type"), c.ctype) || !strings.Contains(rr.Body.String(), c.contains) {
			t.Errorf("%s: want 200 %s containing %q, got %d %s", c.path, c.ctype, c.contains, rr.Code, rr.Header().Get("Content-Type"))
		}
	}
}

func TestOnlyDashboardFilesAreServed(t *testing.T) {
	useFallback(t)
	s := testServer()
	for _, p := range []string{"/ui/", "/ui/nope.js", "/ui/../go.mod", "/ui/..%2fgo.mod", "/go.mod", "/web/app.js", "/.env"} {
		rr := doReq(s, "GET", p, "", "", "")
		if rr.Code == 200 {
			t.Errorf("%s must not be served (got 200)", p)
		}
		if strings.Contains(rr.Body.String(), "module github.com") {
			t.Errorf("%s leaked go.mod", p)
		}
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	useFallback(t)
	s := testServer()
	for _, p := range []string{"/", "/ui/app.js", "/healthz", "/v1/me"} {
		h := doReq(s, "GET", p, "", "", "").Header()
		if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("X-Frame-Options") != "DENY" || h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: missing basic security headers", p)
		}
		c := h.Get("Content-Security-Policy")
		if !strings.Contains(c, "default-src 'none'") || !strings.Contains(c, "script-src 'self'") || strings.Contains(c, "unsafe-inline") || strings.Contains(c, "unsafe-eval") {
			t.Errorf("%s: weak or missing CSP: %q", p, c)
		}
	}
	if got := doReq(s, "GET", "/v1/me", "", "", "").Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("API responses must not be cached, got %q", got)
	}
}

// The page must work under the strict CSP and must never turn untrusted event text into HTML.
func TestDashboardCodeHasNoUnsafePatterns(t *testing.T) {
	js, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function", "setAttribute(\"style\"", ".style.cssText"} {
		if strings.Contains(string(js), bad) {
			t.Errorf("app.js must not use %s", bad)
		}
	}
	html, _ := os.ReadFile("web/index.html")
	h := string(html)
	if regexp.MustCompile(`<script(\s[^>]*)?>`).FindAllString(h, -1) == nil {
		t.Error("index.html should load app.js with a script tag")
	}
	for _, m := range regexp.MustCompile(`<script[^>]*>`).FindAllString(h, -1) {
		if !strings.Contains(m, "src=") {
			t.Errorf("inline script not allowed: %s", m)
		}
	}
	for _, bad := range []string{" style=", " onclick=", " onload=", " onerror=", "javascript:"} {
		if strings.Contains(h, bad) {
			t.Errorf("index.html must not contain %q", bad)
		}
	}
}

func TestCrossOriginWritesAreRefused(t *testing.T) {
	s := testServer()
	evil := map[string]string{"Origin": "http://evil.example"}
	same := map[string]string{"Origin": "http://example.com"} // httptest requests use Host example.com
	admin := sessionCookie(t, s, testTenantA, "admin")

	for _, c := range []struct{ method, path, body, cookie string }{
		{"POST", "/v1/auth/login", `{}`, ""},
		{"POST", "/v1/auth/logout", "", ""},
		{"POST", "/v1/admin/enrolment-tokens", "", admin},
		{"PATCH", "/v1/incidents/" + testTenantA, `{"status":"resolved"}`, admin},
	} {
		rr := reqWith(s, c.method, c.path, c.body, evil, c.cookie)
		if rr.Code != 403 || !strings.Contains(rr.Body.String(), "cross-origin") {
			t.Errorf("%s %s with a foreign Origin: want 403 cross-origin, got %d %s", c.method, c.path, rr.Code, rr.Body.String())
		}
	}
	if got := reqWith(s, "POST", "/v1/auth/logout", "", same, "").Code; got != 200 {
		t.Errorf("same-origin logout: want 200, got %d", got)
	}
	if got := reqWith(s, "POST", "/v1/auth/logout", "", nil, "").Code; got != 200 {
		t.Errorf("no Origin (scripts, agents): want 200, got %d", got)
	}
	if got := reqWith(s, "GET", "/healthz", "", evil, "").Code; got != 200 {
		t.Errorf("reads are not affected by Origin, got %d", got)
	}
	if got := reqWith(s, "POST", "/v1/auth/logout", "", map[string]string{"Origin": "null"}, "").Code; got != 403 {
		t.Errorf(`Origin "null" must be refused, got %d`, got)
	}
}

// ---------- Next.js build serving ----------

// useFallback makes the tests independent of whether you have built the dashboard.
func useFallback(t *testing.T) {
	t.Helper()
	t.Setenv("DASHBOARD_DIR", t.TempDir())
}

func writeNextBuild(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	check := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	check(os.MkdirAll(filepath.Join(dir, "_next", "static", "chunks"), 0o755))
	check(os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<!DOCTYPE html><html><head><title>AEGIS Next</title></head><body><script src="/_next/static/chunks/main.js" defer></script></body></html>`), 0o644))
	check(os.WriteFile(filepath.Join(dir, "_next", "static", "chunks", "main.js"), []byte("console.log('ok')"), 0o644))
	check(os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("top secret"), 0o644))
	return dir
}

func TestNextBuildIsServedSafely(t *testing.T) {
	t.Setenv("DASHBOARD_DIR", writeNextBuild(t))
	s := testServer()
	page := doReq(s, "GET", "/", "", "", "")
	if page.Code != 200 || !strings.Contains(page.Body.String(), "AEGIS Next") || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("index: want the Next.js page, got %d %s", page.Code, page.Header().Get("Content-Type"))
	}
	if c := page.Header().Get("Content-Security-Policy"); !strings.Contains(c, "script-src 'self'") || strings.Contains(c, "unsafe") {
		t.Errorf("weak CSP on the Next.js page: %q", c)
	}
	js := doReq(s, "GET", "/_next/static/chunks/main.js", "", "", "")
	if js.Code != 200 || !strings.HasPrefix(js.Header().Get("Content-Type"), "text/javascript") || !strings.Contains(js.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset: want 200 javascript immutable, got %d %s %s", js.Code, js.Header().Get("Content-Type"), js.Header().Get("Cache-Control"))
	}
	for _, p := range []string{"/_next/", "/_next/static/", "/_next/static/chunks/", "/secret.txt", "/index.html", "/_next/../secret.txt", "/_next/static/..%2f..%2fsecret.txt", "/ui/app.js", "/ui/style.css"} {
		rr := doReq(s, "GET", p, "", "", "")
		if rr.Code == 200 || strings.Contains(rr.Body.String(), "top secret") {
			t.Errorf("%s must not be served (got %d)", p, rr.Code)
		}
	}
}

func TestFallbackConsoleWhenNoNextBuild(t *testing.T) {
	useFallback(t)
	s := testServer()
	page := doReq(s, "GET", "/", "", "", "")
	if page.Code != 200 || !strings.Contains(page.Body.String(), "/ui/app.js") {
		t.Errorf("without a build the fallback console must be served, got %d", page.Code)
	}
	if got := doReq(s, "GET", "/_next/static/x.js", "", "", "").Code; got != 404 {
		t.Errorf("no build: /_next/ must be 404, got %d", got)
	}
}

func TestAllowedOriginsAreExact(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:3000, http://dev.example")
	s := testServer()
	post := func(origin string) int {
		return reqWith(s, "POST", "/v1/auth/logout", "", map[string]string{"Origin": origin}, "").Code
	}
	if post("http://localhost:3000") != 200 || post("http://dev.example") != 200 {
		t.Error("listed origins must be accepted")
	}
	for _, o := range []string{"http://evil.example", "http://localhost:3000.evil.example", "http://localhost:30001", "https://localhost:3000"} {
		if got := post(o); got != 403 {
			t.Errorf("origin %q must be refused, got %d", o, got)
		}
	}
}
