package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Inbound request inspection: looks at the URL path, query string and User-Agent of every
// request BEFORE it reaches authentication or any handler. Request bodies are deliberately
// not inspected (agent telemetry legitimately contains arbitrary text).
//
// INSPECT_MODE: "block" (default) rejects matches with 403; "monitor" only records them;
// "off" disables inspection.
const (
	maxInspectURL  = 2048
	banAfter       = 10               // flagged requests from one non-loopback IP ...
	banWindow      = 5 * time.Minute  // ... within this window ...
	banFor         = 15 * time.Minute // ... earns a temporary ban
	auditPerMinute = 30               // cap on audit rows written by inspection per minute
)

var scannerAgents = []string{
	"sqlmap", "nikto", "nmap", "masscan", "dirbuster", "gobuster", "wfuzz",
	"acunetix", "nessus", "openvas", "zgrab", "havij", "w3af",
}

type inspectRule struct {
	name string
	re   *regexp.Regexp
}

var inspectRules = []inspectRule{
	{"path_traversal", regexp.MustCompile(`(?i)(\.\./|\.\.\\|/etc/passwd|/etc/shadow|boot\.ini|win\.ini)`)},
	{"sql_injection", regexp.MustCompile(`(?i)(\bunion\b[\s/*+]+(all[\s/*+]+)?select\b|\bor\b\s+['"]?1['"]?\s*=\s*['"]?1|'\s*or\s*'|"\s*or\s*"|;\s*drop\s+table|\bsleep\s*\(\s*\d|\bbenchmark\s*\(|information_schema|xp_cmdshell|load_file\s*\()`)},
	{"xss", regexp.MustCompile(`(?i)(<\s*script|<\s*iframe|<\s*svg[^>]*on\w+\s*=|javascript\s*:|\bon(error|load|click|mouseover|focus)\s*=)`)},
	{"command_injection", regexp.MustCompile(`(?i)(\$\(|\$\{jndi:|;\s*(cat|ls|wget|curl|bash|sh|nc|powershell)\b|\|\s*(bash|sh)\b|/bin/(ba)?sh|cmd\.exe)`)},
}

// decodeForInspection percent-decodes up to three times so double-encoded payloads are caught.
func decodeForInspection(s string) string {
	for i := 0; i < 3; i++ {
		d, err := url.QueryUnescape(s)
		if err != nil || d == s {
			break
		}
		s = d
	}
	return s
}

// inspectRequest returns the name of the first rule the request trips, or "" if it is clean.
func inspectRequest(r *http.Request) (rule, detail string) {
	if len(r.URL.RequestURI()) > maxInspectURL {
		return "oversized_url", ""
	}
	ua := strings.ToLower(r.UserAgent())
	for _, sig := range scannerAgents {
		if strings.Contains(ua, sig) {
			return "scanner_agent", sig
		}
	}
	target := decodeForInspection(r.URL.RawQuery) + "\n" + decodeForInspection(r.URL.EscapedPath())
	if strings.ContainsRune(target, 0) {
		return "null_byte", ""
	}
	if strings.Contains(target, "`") {
		return "command_injection", "backtick"
	}
	for _, ir := range inspectRules {
		if m := ir.re.FindString(target); m != "" {
			return ir.name, m
		}
	}
	return "", ""
}

// inspectRequests wraps next. It never reads the request body.
func (s *Server) inspectRequests(mode string, next http.Handler) http.Handler {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "off" {
		return next
	}
	if mode != "monitor" {
		mode = "block"
	}
	var mu sync.Mutex
	hits := map[string][]time.Time{}
	banned := map[string]time.Time{}
	var auditWindow time.Time
	auditCount := 0

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			peer = r.RemoteAddr
		}
		// A loopback peer is usually the dashboard gateway or a local proxy that fronts many
		// real users, so it is never banned (individual bad requests are still blocked).
		ip := net.ParseIP(peer)
		canBan := mode == "block" && !(ip != nil && ip.IsLoopback())
		now := time.Now()

		if canBan {
			mu.Lock()
			until, isBanned := banned[peer]
			if isBanned && now.After(until) {
				delete(banned, peer)
				isBanned = false
			}
			mu.Unlock()
			if isBanned {
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("Retry-After", "900")
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "temporarily blocked"})
				return
			}
		}

		rule, detail := inspectRequest(r)
		if rule == "" {
			next.ServeHTTP(w, r)
			return
		}

		mu.Lock()
		doAudit := false
		if now.Sub(auditWindow) >= time.Minute {
			auditWindow, auditCount = now, 0
		}
		if auditCount < auditPerMinute {
			auditCount++
			doAudit = true
		}
		newlyBanned := false
		if canBan {
			cutoff := now.Add(-banWindow)
			kept := make([]time.Time, 0, banAfter)
			for _, t := range hits[peer] {
				if t.After(cutoff) {
					kept = append(kept, t)
				}
			}
			kept = append(kept, now)
			hits[peer] = kept
			if len(kept) >= banAfter {
				banned[peer] = now.Add(banFor)
				delete(hits, peer)
				newlyBanned = true
			}
			if len(hits) > 4096 {
				hits = map[string][]time.Time{}
			}
			if len(banned) > 4096 {
				banned = map[string]time.Time{}
			}
		}
		mu.Unlock()

		result := "blocked"
		if mode == "monitor" {
			result = "flagged"
		}
		log.Printf("inspect: %s %s from %s rule=%s (%s)", r.Method, r.URL.Path, peer, rule, result)
		if doAudit {
			actor, target := "ip:"+peer, r.Method+" "+r.URL.Path
			meta := map[string]any{"rule": rule, "match": short(detail), "mode": mode, "user_agent": short(r.UserAgent()), "banned": newlyBanned}
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				s.auditMeta(ctx, "", actor, "security.inspect", target, result, meta)
			}()
		}
		if mode == "monitor" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "request blocked"})
	})
}
