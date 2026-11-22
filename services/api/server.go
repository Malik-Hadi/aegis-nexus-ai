package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Server struct {
	pool         *pgxpool.Pool
	secret       []byte
	secureCookie bool
	dummyHash    []byte
	ioc          iocSet
	hub          *hub
}

type ctxKey struct{}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func short(s string) string {
	if len(s) > 100 {
		return s[:100]
	}
	return s
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.HandleFunc("/v1/auth/login", s.handleLogin)
	mux.HandleFunc("/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("/v1/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("/v1/admin/ping", s.requireRole(s.handleAdminPing, "admin"))

	// Block 2: agents and events
	mux.HandleFunc("POST /v1/admin/enrolment-tokens", s.requireRole(s.handleCreateEnrolToken, "admin"))
	mux.HandleFunc("POST /v1/agents/enrol", s.handleEnrol)
	mux.HandleFunc("POST /v1/agents/heartbeat", s.requireAgent(s.handleHeartbeat))
	mux.HandleFunc("GET /v1/agents", s.requireAuth(s.handleListAgents))
	mux.HandleFunc("POST /v1/agents/{id}/revoke", s.requireRole(s.handleRevoke, "admin"))
	mux.HandleFunc("POST /v1/events", s.requireAgent(s.handleIngest))
	mux.HandleFunc("GET /v1/events", s.requireAuth(s.handleListEvents))

	// Block 4: incidents (read-only for now)
	mux.HandleFunc("GET /v1/incidents", s.requireAuth(s.handleListIncidents))
	mux.HandleFunc("GET /v1/incidents/{id}", s.requireAuth(s.handleGetIncident))

	// Block 5: lifecycle, approvals, real-time stream
	mux.HandleFunc("PATCH /v1/incidents/{id}", s.requireAuth(s.handlePatchIncident))
	mux.HandleFunc("POST /v1/incidents/{id}/approvals", s.requireAuth(s.handleRequestApproval))
	mux.HandleFunc("POST /v1/approvals/{id}/decision", s.requireRole(s.handleDecideApproval, "admin"))
	mux.HandleFunc("GET /v1/stream", s.requireAuth(s.handleStream))

	mux.HandleFunc("POST /v1/incidents/{id}/analyse", s.requireRole(s.handleAnalyse, "admin", "analyst"))
	mux.HandleFunc("GET /v1/incidents/{id}/analysis/{job}", s.requireRole(s.handleAnalysisResult, "admin", "analyst"))

	// Block 6: dashboard
	s.routeWeb(mux)
	return secureHeaders(mux)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.pool.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready", "database": "down"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "database": "up"})
}

// audit writes one row to audit_logs. tenantID may be "" when unknown.
func (s *Server) audit(ctx context.Context, tenantID, actor, action, target, result string) {
	if s.pool == nil {
		return
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO audit_logs (tenant_id, actor, action, target, result)
		 VALUES (NULLIF($1::text,'')::uuid, $2, $3, $4, $5)`,
		tenantID, short(actor), action, short(target), result)
	if err != nil {
		log.Printf("audit write failed: %v", err)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !originOK(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request refused"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use POST"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var in struct {
		TenantID string `json:"tenant_id"`
		Subject  string `json:"subject"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}

	var id, tenant, hash, role, status string
	err := s.pool.QueryRow(r.Context(),
		`SELECT id::text, tenant_id::text, COALESCE(password_hash,''), role, status
		   FROM users WHERE tenant_id = $1::text::uuid AND subject = $2`,
		in.TenantID, in.Subject).Scan(&id, &tenant, &hash, &role, &status)
	if err != nil {
		hash = string(s.dummyHash) // unknown user: still do the slow compare
	}
	passOK := bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) == nil
	if err != nil || !passOK || status != "active" {
		s.audit(r.Context(), "", in.Subject, "auth.login", "", "failure")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	exp := time.Now().Add(sessionTTL)
	tok, err := signSession(s.secret, Session{UserID: id, TenantID: tenant, Role: role, Subject: in.Subject, Expires: exp.Unix()})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server error"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: tok, Path: "/", Expires: exp,
		MaxAge: int(sessionTTL.Seconds()), HttpOnly: true,
		Secure: s.secureCookie, SameSite: http.SameSiteLaxMode,
	})
	s.audit(r.Context(), tenant, in.Subject, "auth.login", "", "success")
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged in", "role": role})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !originOK(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request refused"})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

func sessionFrom(r *http.Request) Session {
	sess, _ := r.Context().Value(ctxKey{}).(Session)
	return sess
}

// requireAuth: no valid signed, unexpired cookie = 401. Tenant and role come ONLY from the cookie.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !originOK(r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request refused"})
			return
		}
		c, err := r.Cookie(cookieName)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
			return
		}
		sess, err := verifySession(s.secret, c.Value, time.Now())
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
	}
}

// requireRole: logged in but wrong role = 403 (and it is audited).
func (s *Server) requireRole(next http.HandlerFunc, roles ...string) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		sess := sessionFrom(r)
		for _, role := range roles {
			if sess.Role == role {
				next(w, r)
				return
			}
		}
		s.audit(r.Context(), sess.TenantID, sess.Subject, "authz.denied", r.URL.Path, "denied")
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	writeJSON(w, http.StatusOK, map[string]string{"subject": sess.Subject, "role": sess.Role, "tenant_id": sess.TenantID})
}

func (s *Server) handleAdminPing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"message": "admin access granted"})
}
