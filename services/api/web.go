package main

import (
	"embed"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Fallback console (plain HTML), used only while the Next.js dashboard has not been built yet.
//
//go:embed web/index.html web/app.js web/style.css
var webFS embed.FS

// Strict policy: only our own files may run or load. No inline scripts or styles, no other origins.
const csp = "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", csp)
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// dashboardDir is where `npm run build` (static export) puts the Next.js dashboard.
func dashboardDir() string {
	if d := os.Getenv("DASHBOARD_DIR"); d != "" {
		return d
	}
	return filepath.Join("apps", "dashboard", "out")
}

func nextBuilt() bool {
	st, err := os.Stat(filepath.Join(dashboardDir(), "index.html"))
	return err == nil && !st.IsDir()
}

// dashFS serves files from the Next.js build and NEVER lists directories.
// The directory is looked up on every request, so building the dashboard needs no API restart.
type dashFS struct{}

func (dashFS) Open(name string) (http.File, error) {
	f, err := http.Dir(dashboardDir()).Open(name)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if st.IsDir() {
		f.Close()
		return nil, os.ErrNotExist
	}
	return f, nil
}

// Fixed content types: Windows can map .js to text/plain, which would break the page under nosniff.
func contentTypeFor(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json", ".map":
		return "application/json"
	case ".html":
		return "text/html; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".png":
		return "image/png"
	case ".woff2":
		return "font/woff2"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	return ""
}

func (s *Server) routeWeb(mux *http.ServeMux) {
	embedded := func(file, ctype string) http.HandlerFunc {
		b, _ := webFS.ReadFile("web/" + file)
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ctype)
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(b)
		}
	}
	fallbackIndex := embedded("index.html", "text/html; charset=utf-8")
	fallbackJS := embedded("app.js", "text/javascript; charset=utf-8")
	fallbackCSS := embedded("style.css", "text/css; charset=utf-8")
	files := http.FileServer(dashFS{})

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if !nextBuilt() {
			fallbackIndex(w, r)
			return
		}
		b, err := os.ReadFile(filepath.Join(dashboardDir(), "index.html"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(b)
	})

	// Next.js assets (hashed file names, safe to cache for a long time).
	static := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only clean paths under /_next/ (plus the favicon). "..", encoded slashes and directory paths are refused,
		// so a crafted URL can never reach other files in the build folder.
		p := r.URL.Path
		if path.Clean(p) != p || strings.Contains(p, "..") || !(strings.HasPrefix(p, "/_next/") || p == "/favicon.ico") {
			http.NotFound(w, r)
			return
		}
		if ct := contentTypeFor(r.URL.Path); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		if strings.HasPrefix(r.URL.Path, "/_next/static/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
	mux.Handle("GET /_next/", static)
	mux.Handle("GET /favicon.ico", static)

	// Fallback console files exist only while there is no Next.js build.
	only := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if nextBuilt() {
				http.NotFound(w, r)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /ui/app.js", only(fallbackJS))
	mux.HandleFunc("GET /ui/style.css", only(fallbackCSS))
}

func originAllowed(o string) bool {
	for _, a := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if a = strings.TrimSpace(a); a != "" && a == o {
			return true
		}
	}
	return false
}

// originOK is a CSRF defence on top of SameSite cookies: a browser always sends Origin on cross-site
// writes, so a state-changing request whose Origin is not this server is refused. Agents and scripts send no Origin.
// ALLOWED_ORIGINS (exact match, comma separated) is only for the dev server, for example http://localhost:3000.
func originOK(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if u, err := url.Parse(o); err == nil && u.Scheme == scheme && u.Host == r.Host && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" {
		return true
	}
	return originAllowed(o)
}
