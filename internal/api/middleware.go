package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"
)

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeyAuth
)

// authInfo is the authenticated user/session pair stored in the request context.
type authInfo struct {
	User    storeUser
	Session sessionInfo
}

type storeUser struct {
	ID       int64
	Username string
	Role     string
}

// isAdmin reports whether the authenticated account may change settings.
func (u storeUser) isAdmin() bool { return u.Role == "admin" }

type sessionInfo struct {
	ID        string
	ExpiresAt time.Time
}

// withRecovery converts panics into 500 responses and logs them.
func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic serving request", "panic", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal_error", "Internal server error.", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withRequestID assigns a request id and echoes it in the response.
func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 64 {
			buf := make([]byte, 8)
			if _, err := rand.Read(buf); err == nil {
				id = hex.EncodeToString(buf)
			} else {
				id = "unknown"
			}
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Flush implements http.Flusher for SSE.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap supports http.ResponseController.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (s *Server) withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", clientIP(r),
		}
		if id, ok := r.Context().Value(ctxKeyRequestID).(string); ok {
			attrs = append(attrs, "request_id", id)
		}
		if a, ok := r.Context().Value(ctxKeyAuth).(authInfo); ok {
			attrs = append(attrs, "user", a.User.Username)
		}
		if rec.status >= 500 {
			s.log.Error("http request", attrs...)
		} else {
			s.log.Info("http request", attrs...)
		}
	})
}

const csp = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"connect-src 'self'; font-src 'self' data:; script-src 'self'; object-src 'none'; " +
	"base-uri 'self'; frame-ancestors 'none'; form-action 'self'"

func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

const (
	sessionCookieName = "aiptimize_session"
	csrfCookieName    = "aiptimize_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

func (s *Server) secureCookie(r *http.Request) bool {
	switch s.cfg.CookieSecure {
	case configCookieSecureAlways:
		return true
	case configCookieSecureNever:
		return false
	default:
		if r.TLS != nil {
			return true
		}
		if s.cfg.TrustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			return true
		}
		return false
	}
}

// withCSRF issues the double-submit CSRF cookie and enforces it on
// state-changing API requests.
func (s *Server) withCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(csrfCookieName)
		token := ""
		if err == nil {
			token = cookie.Value
		}
		if token == "" {
			newToken, genErr := newCSRFToken()
			if genErr != nil {
				writeError(w, http.StatusInternalServerError, "internal_error", "Could not initialise request token.", nil)
				return
			}
			token = newToken
			http.SetCookie(w, &http.Cookie{
				Name:     csrfCookieName,
				Value:    token,
				Path:     "/",
				MaxAge:   30 * 24 * 3600,
				HttpOnly: false, // double-submit cookie must be readable by the SPA
				Secure:   s.secureCookie(r),
				SameSite: http.SameSiteLaxMode,
			})
		}

		if isStateChanging(r.Method) && strings.HasPrefix(r.URL.Path, "/api/") {
			if origin := r.Header.Get("Origin"); origin != "" {
				if !sameOrigin(r, origin, s.cfg.TrustProxy) {
					writeError(w, http.StatusForbidden, "origin_mismatch", "Request origin does not match this server.", nil)
					return
				}
			}
			header := r.Header.Get(csrfHeaderName)
			if header == "" || token == "" || !constantTimeEqual(header, token) {
				writeError(w, http.StatusForbidden, "csrf_failed", "Missing or invalid CSRF token.", nil)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// withAuth resolves the session cookie into a request context value. It never
// rejects requests; protected handlers use requireAuth.
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		hash := hashToken(cookie.Value)
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		ses, err := s.st.SessionByID(ctx, hash)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		now := s.now().UTC()
		if now.After(ses.ExpiresAt) {
			_ = s.st.DeleteSession(ctx, hash)
			next.ServeHTTP(w, r)
			return
		}
		user, err := s.st.UserByID(ctx, ses.UserID)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		// Slide the idle expiry at most once every five minutes.
		if now.Sub(ses.LastSeen) > 5*time.Minute {
			_ = s.st.TouchSession(ctx, hash, now, now.Add(sessionTTL))
		}
		info := authInfo{
			User:    storeUser{ID: user.ID, Username: user.Username, Role: user.Role},
			Session: sessionInfo{ID: hash, ExpiresAt: ses.ExpiresAt},
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyAuth, info)))
	})
}

// requireAuth wraps a handler that requires a valid session.
func (s *Server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authFrom(r.Context()); !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
			return
		}
		h(w, r)
	}
}

// requireAdmin wraps a handler that only administrators may reach.
func (s *Server) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		a, ok := authFrom(r.Context())
		if !ok || !a.User.isAdmin() {
			writeError(w, http.StatusForbidden, "forbidden", "Administrator access is required.", nil)
			return
		}
		h(w, r)
	})
}

func authFrom(ctx context.Context) (authInfo, bool) {
	a, ok := ctx.Value(ctxKeyAuth).(authInfo)
	return a, ok
}

func isStateChanging(method string) bool {
	return method == http.MethodPost || method == http.MethodPut ||
		method == http.MethodPatch || method == http.MethodDelete
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
