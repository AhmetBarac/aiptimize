// Package api implements the HTTP API, authentication middleware and the
// embedded single-page application.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/config"
	"github.com/AhmetBarac/aiptimize/internal/prices"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

// Deps are the server dependencies.
type Deps struct {
	Config   *config.Config
	Store    *store.Store
	Settings *settings.Service
	Prices   *prices.Service
	Logger   *slog.Logger
	WebFS    fs.FS
}

// Server serves the API and the SPA.
type Server struct {
	cfg      *config.Config
	st       *store.Store
	settings *settings.Service
	prices   *prices.Service
	log      *slog.Logger
	web      fs.FS
	started  time.Time

	mux          *http.ServeMux
	broker       *Broker
	loginLimiter *rateLimiter
	setupMu      sync.Mutex
	now          func() time.Time
}

// New builds a server and registers all routes.
func New(d Deps) *Server {
	s := &Server{
		cfg:          d.Config,
		st:           d.Store,
		settings:     d.Settings,
		prices:       d.Prices,
		log:          d.Logger,
		web:          d.WebFS,
		started:      time.Now(),
		mux:          http.NewServeMux(),
		broker:       NewBroker(),
		loginLimiter: newRateLimiter(5, time.Minute),
		now:          time.Now,
	}
	s.routes()
	return s
}

// Broker exposes the SSE broker for other components (pollers, planners).
func (s *Server) Broker() *Broker { return s.broker }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)

	s.mux.HandleFunc("GET /api/v1/healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /api/v1/readyz", s.handleReadyz)
	s.mux.HandleFunc("GET /api/v1/meta", s.handleMeta)
	s.mux.HandleFunc("POST /api/v1/setup", s.handleSetup)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.requireAuth(s.handleLogout))
	s.mux.HandleFunc("GET /api/v1/auth/me", s.handleMe)

	s.mux.HandleFunc("GET /api/v1/status", s.requireAuth(s.handleStatus))
	s.mux.HandleFunc("GET /api/v1/prices", s.requireAuth(s.handlePrices))
	s.mux.HandleFunc("POST /api/v1/prices/refresh", s.requireAuth(s.handleRefreshPrices))
	s.mux.HandleFunc("GET /api/v1/settings", s.requireAuth(s.handleGetSettings))
	s.mux.HandleFunc("PATCH /api/v1/settings", s.requireAuth(s.handlePatchSettings))
	s.mux.HandleFunc("GET /api/v1/events", s.requireAuth(s.handleEvents))
	s.mux.HandleFunc("GET /api/v1/stream", s.requireAuth(s.handleStream))

	s.mux.HandleFunc("/", s.handleFallback)
}

// Handler returns the fully wrapped HTTP handler.
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = s.withAuth(h)
	h = s.withCSRF(h)
	h = s.withSecurityHeaders(h)
	h = s.withRequestLog(h)
	h = s.withRequestID(h)
	h = s.withRecovery(h)
	return h
}

// RunBackground starts periodic maintenance until ctx is cancelled.
func (s *Server) RunBackground(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := s.st.Prune(ctx, now); err != nil {
				s.log.Warn("store prune failed", "err", err)
			}
			s.loginLimiter.cleanup(now)
		}
	}
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "No such API endpoint.", nil)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": s.cfg.Version,
	})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	checks := map[string]string{"database": "ok"}
	status := http.StatusOK
	if err := s.st.Ping(ctx); err != nil {
		checks["database"] = "error: " + err.Error()
		status = http.StatusServiceUnavailable
	}
	state := "not_ready"
	if status == http.StatusOK {
		state = "ready"
	}
	writeJSON(w, status, map[string]any{"status": state, "checks": checks})
}

// featureSet describes which product phases are implemented.
func featureSet() map[string]any {
	return map[string]any{
		"phase":        1,
		"prices":       true,
		"telemetry":    false,
		"forecast":     false,
		"battery_plan": false,
		"actuation":    false,
		"plan_editing": false,
		"ev_charging":  false,
		"history":      false,
	}
}

func (s *Server) gates() map[string]any {
	v := s.settings.Get()
	return map[string]any{
		"writes_env":       s.cfg.EnableWrites,
		"writes_ui":        v.Safety.WritesArmedUI,
		"writes_armed":     s.cfg.EnableWrites && v.Safety.WritesArmedUI,
		"ev_control_env":   s.cfg.EnableEVControl,
		"ev_control_ui":    v.Safety.EVControlArmedUI,
		"ev_control_armed": s.cfg.EnableEVControl && v.Safety.EVControlArmedUI,
		"simulator":        s.cfg.Simulator,
	}
}

func (s *Server) setupRequired(ctx context.Context) (bool, error) {
	n, err := s.st.UserCount(ctx)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	required, err := s.setupRequired(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not read application state.", nil)
		return
	}
	v := s.settings.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"version":        s.cfg.Version,
		"commit":         s.cfg.Commit,
		"build_date":     s.cfg.BuildDate,
		"now":            s.now().UTC(),
		"timezone":       v.Site.Timezone,
		"setup_required": required,
		"gates":          s.gates(),
		"features":       featureSet(),
		"site": map[string]any{
			"name":     v.Site.Name,
			"currency": v.Price.Currency,
			"area":     v.Price.Area,
		},
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.statusSnapshot(r.Context()))
}

func (s *Server) statusSnapshot(ctx context.Context) map[string]any {
	v := s.settings.Get()
	return map[string]any{
		"version":  s.cfg.Version,
		"now":      s.now().UTC(),
		"uptime_s": int(s.now().Sub(s.started).Seconds()),
		"site": map[string]any{
			"name":     v.Site.Name,
			"timezone": v.Site.Timezone,
			"currency": v.Price.Currency,
			"area":     v.Price.Area,
		},
		"gates": s.gates(),
		"telemetry": map[string]any{
			"available": false,
			"stale":     true,
			"sample":    nil,
		},
		"prices":   s.priceStatus(ctx),
		"plan":     nil,
		"ev":       map[string]any{"available": false},
		"schedule": map[string]any{"available": false},
		"checks":   map[string]string{"database": "ok"},
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	sections, err := s.settings.JSON()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not read settings.", nil)
		return
	}
	out := make(map[string]json.RawMessage, len(sections))
	for k, v := range sections {
		out[k] = v
	}
	writeJSON(w, http.StatusOK, out)
}

type patchSettingsRequest struct {
	Section string          `json:"section"`
	Values  json.RawMessage `json:"values"`
}

func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	var req patchSettingsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if req.Section == "" || len(req.Values) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "section and values are required", nil)
		return
	}
	raw, err := s.settings.Patch(r.Context(), req.Section, req.Values)
	if err != nil {
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
	s.broker.Publish("settings", map[string]any{"section": req.Section, "values": raw})
	writeJSON(w, http.StatusOK, map[string]any{"section": req.Section, "values": raw})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	var since time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "since must be RFC3339", nil)
			return
		}
		since = t
	}
	events, err := s.st.ListEvents(r.Context(), since, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not read events.", nil)
		return
	}
	if events == nil {
		events = []store.Event{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}
