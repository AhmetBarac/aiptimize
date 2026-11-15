package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/auth"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,64}$`)

type setupRequest struct {
	Username string          `json:"username"`
	Password string          `json:"password"`
	Site     json.RawMessage `json:"site,omitempty"`
}

type userResponse struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if !usernamePattern.MatchString(req.Username) {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed",
			"Username must be 3-64 characters of letters, digits, dot, dash or underscore.", nil)
		return
	}
	if len(req.Password) < auth.MinPasswordLength {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed",
			fmt.Sprintf("Password must be at least %d characters.", auth.MinPasswordLength), nil)
		return
	}
	if len(req.Site) > 0 {
		if _, err := s.settings.Patch(r.Context(), settings.SectionSite, req.Site); err != nil {
			var verr *settings.ValidationError
			if errors.As(err, &verr) {
				writeError(w, http.StatusUnprocessableEntity, "validation_failed", verr.Message, map[string]any{
					"section": verr.Section,
					"field":   verr.Field,
				})
				return
			}
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
	}

	s.setupMu.Lock()
	defer s.setupMu.Unlock()

	required, err := s.setupRequired(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not read application state.", nil)
		return
	}
	if !required {
		writeError(w, http.StatusConflict, "already_initialized", "Setup has already been completed.", nil)
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not create the user.", nil)
		return
	}
	user, err := s.st.CreateUser(r.Context(), req.Username, hash, store.RoleAdmin)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(w, http.StatusConflict, "already_initialized", "Setup has already been completed.", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not create the user.", nil)
		return
	}
	if err := s.startSession(w, r, user); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not create a session.", nil)
		return
	}
	_ = s.st.InsertEvent(r.Context(), store.Event{
		Severity: "info", Code: "setup_completed", Message: "Initial administrator account created.",
	})
	writeJSON(w, http.StatusCreated, map[string]any{"user": userResponse{ID: user.ID, Username: user.Username}})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	required, err := s.setupRequired(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not read application state.", nil)
		return
	}
	if required {
		writeError(w, http.StatusForbidden, "setup_required", "Complete first-run setup first.", nil)
		return
	}
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	ip := clientIP(r)
	if !s.loginLimiter.allow(ip, s.now()) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many login attempts. Try again in a minute.", nil)
		return
	}
	user, err := s.st.UserByUsername(r.Context(), strings.TrimSpace(req.Username))
	if err != nil {
		s.log.Warn("login failed", "ip", ip, "reason", "unknown_user")
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid username or password.", nil)
		return
	}
	ok, err := auth.VerifyPassword(user.PasswordHash, req.Password)
	if err != nil || !ok {
		s.log.Warn("login failed", "ip", ip, "reason", "bad_password")
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid username or password.", nil)
		return
	}
	if err := s.startSession(w, r, user); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not create a session.", nil)
		return
	}
	s.loginLimiter.reset(ip)
	writeJSON(w, http.StatusOK, map[string]any{"user": userResponse{ID: user.ID, Username: user.Username}})
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user store.User) error {
	raw, hash, err := auth.NewToken()
	if err != nil {
		return err
	}
	now := s.now().UTC()
	ses := store.Session{
		ID:        hash,
		UserID:    user.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(sessionTTL),
		LastSeen:  now,
		IP:        clientIP(r),
	}
	if err := s.st.CreateSession(r.Context(), ses); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    raw,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	a, ok := authFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": userResponse{ID: a.User.ID, Username: a.User.Username, Role: a.User.Role},
		"session": map[string]any{
			"expires_at": a.Session.ExpiresAt,
		},
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if a, ok := authFrom(r.Context()); ok {
		if err := s.st.DeleteSession(r.Context(), a.Session.ID); err != nil {
			s.log.Warn("delete session failed", "err", err)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// rateLimiter is a per-key sliding-window limiter.
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, hits: make(map[string][]time.Time)}
}

func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	times := l.hits[key]
	kept := make([]time.Time, 0, len(times)+1)
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

func (l *rateLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

func (l *rateLimiter) cleanup(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	for key, times := range l.hits {
		kept := times[:0]
		for _, t := range times {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(l.hits, key)
		} else {
			l.hits[key] = kept
		}
	}
}
