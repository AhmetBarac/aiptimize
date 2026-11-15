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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/config"
	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/inverter"
	"github.com/AhmetBarac/aiptimize/internal/prices"
	"github.com/AhmetBarac/aiptimize/internal/service"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
	"github.com/AhmetBarac/aiptimize/internal/update"
)

// Deps are the server dependencies.
type Deps struct {
	Config    *config.Config
	Store     *store.Store
	Settings  *settings.Service
	Prices    *prices.Service
	Telemetry *service.TelemetryPoller
	Forecast  *service.ForecastService
	Solar     *service.SolarService
	Planner   *service.PlannerService
	EV        *service.EVService
	History   *service.HistoryService
	Broker    *Broker
	Logger    *slog.Logger
	WebFS     fs.FS
	// Updates checks the container registry for a newer build.
	Updates *update.Checker
	// Instance identifies this installation to the companion app.
	Instance *service.Instance
	// RequestUpdate asks the host supervisor to pull and restart. It is nil when
	// the deployment cannot update itself.
	RequestUpdate func() error
	// RequestRestart asks the process supervisor to restart after the response
	// has been sent. It is nil in tests and standalone API embeddings.
	RequestRestart func()
}

// Server serves the API and the SPA.
type Server struct {
	cfg       *config.Config
	st        *store.Store
	settings  *settings.Service
	prices    *prices.Service
	telemetry *service.TelemetryPoller
	forecast  *service.ForecastService
	solar     *service.SolarService
	planner   *service.PlannerService
	ev        *service.EVService
	history   *service.HistoryService
	log       *slog.Logger
	web       fs.FS
	started   time.Time

	mux            *http.ServeMux
	broker         *Broker
	loginLimiter   *rateLimiter
	setupMu        sync.Mutex
	now            func() time.Time
	requestRestart func()
	updates        *update.Checker
	requestUpdate  func() error
	instance       *service.Instance
}

// New builds a server and registers all routes.
func New(d Deps) *Server {
	broker := d.Broker
	if broker == nil {
		broker = NewBroker()
	}
	s := &Server{
		cfg:            d.Config,
		st:             d.Store,
		settings:       d.Settings,
		prices:         d.Prices,
		telemetry:      d.Telemetry,
		forecast:       d.Forecast,
		solar:          d.Solar,
		planner:        d.Planner,
		ev:             d.EV,
		history:        d.History,
		log:            d.Logger,
		web:            d.WebFS,
		started:        time.Now(),
		mux:            http.NewServeMux(),
		broker:         broker,
		loginLimiter:   newRateLimiter(5, time.Minute),
		now:            time.Now,
		requestRestart: d.RequestRestart,
		updates:        d.Updates,
		requestUpdate:  d.RequestUpdate,
		instance:       d.Instance,
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
	s.mux.HandleFunc("GET /api/v1/instance", s.handleInstance)
	s.mux.HandleFunc("POST /api/v1/setup", s.handleSetup)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.requireAuth(s.handleLogout))
	s.mux.HandleFunc("GET /api/v1/auth/me", s.handleMe)
	s.mux.HandleFunc("POST /api/v1/auth/password", s.requireAuth(s.handleChangePassword))

	// Account management is administrator-only: the two roles are admin and user.
	s.mux.HandleFunc("GET /api/v1/users", s.requireAdmin(s.handleListUsers))
	s.mux.HandleFunc("POST /api/v1/users", s.requireAdmin(s.handleCreateUser))
	s.mux.HandleFunc("DELETE /api/v1/users/{id}", s.requireAdmin(s.handleDeleteUser))
	s.mux.HandleFunc("POST /api/v1/users/{id}/password", s.requireAdmin(s.handleResetPassword))

	s.mux.HandleFunc("GET /api/v1/status", s.requireAuth(s.handleStatus))
	s.mux.HandleFunc("GET /api/v1/prices", s.requireAuth(s.handlePrices))
	s.mux.HandleFunc("POST /api/v1/prices/refresh", s.requireAuth(s.handleRefreshPrices))
	s.mux.HandleFunc("GET /api/v1/forecast", s.requireAuth(s.handleForecast))
	s.mux.HandleFunc("POST /api/v1/forecast/refresh", s.requireAuth(s.handleRefreshForecast))
	s.mux.HandleFunc("GET /api/v1/solar", s.requireAuth(s.handleSolar))
	s.mux.HandleFunc("GET /api/v1/plan", s.requireAuth(s.handlePlanView))
	s.mux.HandleFunc("POST /api/v1/plan/recompute", s.requireAuth(s.handleRecomputePlan))
	s.mux.HandleFunc("POST /api/v1/plan/drafts", s.requireAuth(s.handleSaveDraft))
	s.mux.HandleFunc("POST /api/v1/plan/control", s.requireAuth(s.handlePlanControl))
	s.mux.HandleFunc("POST /api/v1/plan/drafts/{id}/preview", s.requireAuth(s.handlePreviewDraft))
	s.mux.HandleFunc("POST /api/v1/plan/drafts/{id}/apply", s.requireAuth(s.handleApplyDraft))
	s.mux.HandleFunc("DELETE /api/v1/plan/drafts/{id}", s.requireAuth(s.handleDiscardDraft))
	s.mux.HandleFunc("POST /api/v1/plan/auto/apply", s.requireAuth(s.handleApplyAuto))
	s.mux.HandleFunc("POST /api/v1/plan/download", s.requireAuth(s.handleDownloadPlan))
	s.mux.HandleFunc("GET /api/v1/plan/revisions", s.requireAuth(s.handlePlanRevisions))
	s.mux.HandleFunc("GET /api/v1/schedule", s.requireAuth(s.handleSchedule))
	s.mux.HandleFunc("POST /api/v1/schedule/clear", s.requireAuth(s.handleScheduleClear))
	s.mux.HandleFunc("POST /api/v1/schedule/takeover", s.requireAuth(s.handleScheduleTakeover))
	s.mux.HandleFunc("POST /api/v1/battery/mode", s.requireAuth(s.handleBatteryMode))
	s.mux.HandleFunc("POST /api/v1/battery/force", s.requireAuth(s.handleBatteryForce))
	s.mux.HandleFunc("GET /api/v1/chargers", s.requireAuth(s.handleChargers))
	s.mux.HandleFunc("GET /api/v1/ev/config", s.requireAuth(s.handleEVConfig))
	s.mux.HandleFunc("PUT /api/v1/ev/config", s.requireAuth(s.handleUpdateEVConfig))
	s.mux.HandleFunc("POST /api/v1/system/restart", s.requireAdmin(s.handleRestart))
	s.mux.HandleFunc("GET /api/v1/system/update", s.requireAuth(s.handleUpdateCheck))
	s.mux.HandleFunc("POST /api/v1/system/update/apply", s.requireAdmin(s.handleUpdateApply))
	s.mux.HandleFunc("GET /api/v1/chargers/{id}", s.requireAuth(s.handleCharger))
	s.mux.HandleFunc("GET /api/v1/chargers/{id}/status", s.requireAuth(s.handleChargerStatus))
	s.mux.HandleFunc("PATCH /api/v1/chargers/{id}/config", s.requireAuth(s.handleChargerConfig))
	s.mux.HandleFunc("POST /api/v1/chargers/{id}/mode", s.requireAuth(s.handleChargerMode))
	s.mux.HandleFunc("POST /api/v1/chargers/{id}/current", s.requireAuth(s.handleChargerCurrent))
	s.mux.HandleFunc("POST /api/v1/chargers/{id}/pause", s.requireAuth(s.handleChargerPause))
	s.mux.HandleFunc("POST /api/v1/chargers/{id}/resume", s.requireAuth(s.handleChargerResume))
	s.mux.HandleFunc("GET /api/v1/chargers/{id}/plan", s.requireAuth(s.handleChargerPlan))
	s.mux.HandleFunc("POST /api/v1/chargers/{id}/plan/static", s.requireAuth(s.handleStaticPlan))
	s.mux.HandleFunc("DELETE /api/v1/chargers/{id}/plan/static", s.requireAuth(s.handleDeleteStaticPlan))
	s.mux.HandleFunc("POST /api/v1/chargers/{id}/plan/static/activate", s.requireAuth(s.handleActivateStaticPlan))
	s.mux.HandleFunc("PUT /api/v1/chargers/{id}/plans/repeating", s.requireAuth(s.handleRepeatingPlans))
	s.mux.HandleFunc("POST /api/v1/chargers/{id}/plan/preview", s.requireAuth(s.handlePlanPreview))
	s.mux.HandleFunc("GET /api/v1/chargers/{id}/session", s.requireAuth(s.handleEVSession))
	s.mux.HandleFunc("GET /api/v1/history", s.requireAuth(s.handleHistory))
	s.mux.HandleFunc("GET /api/v1/metrics", s.requireAuth(s.handleMetrics))
	s.mux.HandleFunc("GET /api/v1/settings/export", s.requireAdmin(s.handleExportSettings))
	s.mux.HandleFunc("POST /api/v1/settings/import", s.requireAdmin(s.handleImportSettings))
	s.mux.HandleFunc("POST /api/v1/settings/test/inverter", s.requireAdmin(s.handleTestInverter))
	s.mux.HandleFunc("GET /api/v1/settings", s.requireAuth(s.handleGetSettings))
	s.mux.HandleFunc("PATCH /api/v1/settings", s.requireAdmin(s.handlePatchSettings))
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
		"phase":        7,
		"prices":       true,
		"telemetry":    true,
		"forecast":     true,
		"battery_plan": true,
		"actuation":    true,
		"plan_editing": true,
		"ev_charging":  true,
		"history":      true,
	}
}

func (s *Server) gates() map[string]any {
	v := s.settings.Get()
	evEnv := s.evControlEnv()
	return map[string]any{
		"writes_env":       s.writesEnv(),
		"writes_mode":      string(s.cfg.WriteMode.Effective()),
		"writes_ui":        v.Safety.WritesArmedUI,
		"writes_armed":     s.writesEnv() && v.Safety.WritesArmedUI,
		"ev_control_env":   evEnv,
		"ev_control_mode":  string(s.cfg.EVControlMode.Effective()),
		"ev_control_ui":    v.Safety.EVControlArmedUI,
		"ev_control_armed": evEnv && v.Safety.EVControlArmedUI,
		"simulator":        s.cfg.Simulator,
	}
}

// writesEnv resolves the environment half of the inverter write gate. In auto
// mode it follows the inverter link, so a configured and reachable inverter
// needs no environment flag; the UI arming switch still has the final say.
func (s *Server) writesEnv() bool {
	healthy := s.telemetry != nil && s.telemetry.Healthy()
	return s.cfg.WritesEnv(healthy)
}

// evControlEnv resolves the environment half of the EV gate. In the default
// auto mode it follows the charger link instead of an environment variable.
func (s *Server) evControlEnv() bool {
	return s.cfg.EVControlMode.Resolve(s.ev != nil && s.ev.Healthy())
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
		"instance_id":    s.instanceID(),
		"gates":          s.gates(),
		"features":       featureSet(),
		"ev": map[string]any{
			"provider":         s.evConfigPayload()["provider"],
			"active_provider":  s.evConfigPayload()["active_provider"],
			"restart_required": s.evConfigPayload()["restart_required"],
			"healthy":          s.ev != nil && s.ev.Healthy(),
		},
		"site": map[string]any{
			"name":            v.Site.Name,
			"currency":        v.Price.Currency,
			"area":            v.Price.Area,
			"show_full_price": v.Site.ShowFullPrice,
		},
	})
}

// handleInstance is the public identity endpoint used by LAN discovery and the
// companion app to prove a URL is this server.
func (s *Server) handleInstance(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	required, err := s.setupRequired(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not read application state.", nil)
		return
	}
	v := s.settings.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":             s.instanceID(),
		"name":           v.Site.Name,
		"version":        s.cfg.Version,
		"api_version":    1,
		"setup_required": required,
	})
}

func (s *Server) instanceID() string {
	if s.instance == nil {
		return ""
	}
	return s.instance.ID()
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
			"name":            v.Site.Name,
			"timezone":        v.Site.Timezone,
			"currency":        v.Price.Currency,
			"area":            v.Price.Area,
			"show_full_price": v.Site.ShowFullPrice,
		},
		"gates":     s.gates(),
		"prices":    s.priceStatus(ctx),
		"telemetry": s.telemetryStatus(),
		"forecast":  s.forecastStatus(),
		"plan":      s.planStatus(ctx),
		"ev":        s.evStatus(ctx),
		"schedule":  s.scheduleStatus(ctx),
		"checks":    s.checks(),
	}
}

func (s *Server) checks() map[string]string {
	checks := map[string]string{"database": "ok"}
	if s.telemetry != nil {
		if _, _, ok := s.telemetry.Latest(); ok {
			checks["inverter"] = "ok"
		} else {
			checks["inverter"] = "unavailable"
		}
	}
	return checks
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
	if raw, ok := out[settings.SectionSite]; ok {
		if masked, err := maskSiteSecret(raw); err == nil {
			out[settings.SectionSite] = masked
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// maskSiteSecret keeps the registry license out of API responses while telling
// the UI whether one is configured.
func maskSiteSecret(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	var license string
	if value, ok := fields["license"]; ok {
		_ = json.Unmarshal(value, &license)
	}
	fields["license"] = json.RawMessage(`""`)
	fields["license_set"] = json.RawMessage(strconv.FormatBool(strings.TrimSpace(license) != ""))
	return json.Marshal(fields)
}

// registryToken returns the Docker Hub token used for update checks, preferring
// the value stored in settings over the environment.
func (s *Server) registryToken() string {
	if s.settings != nil {
		if token := strings.TrimSpace(s.settings.Get().Site.License); token != "" {
			return token
		}
	}
	return s.cfg.RegistryToken
}

type patchSettingsRequest struct {
	Section string          `json:"section"`
	Values  json.RawMessage `json:"values"`
	// ConfirmSwap acknowledges that replacing the inverter is not an ordinary
	// settings edit: the old schedule is cleared, writes are disarmed and the
	// plan is recomputed for the new hardware.
	ConfirmSwap bool `json:"confirm_swap"`
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
	if req.Section == settings.SectionInverter {
		swap, err := s.inverterSwap(r, req.Values)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "validation_failed", err.Error(), nil)
			return
		}
		if swap != nil && !req.ConfirmSwap {
			writeError(w, http.StatusConflict, "inverter_swap_required",
				"Replacing the inverter clears its schedule, disarms writes and recomputes the plan. Confirm to continue.",
				map[string]any{"from": swap.From, "to": swap.To})
			return
		}
		if swap != nil {
			s.prepareInverterSwap(r.Context(), swap)
		}
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
	if s.forecast != nil && (req.Section == settings.SectionPV || req.Section == settings.SectionLoad) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			if _, err := s.forecast.Refresh(ctx); err != nil {
				s.log.Warn("forecast refresh after settings change failed", "err", err)
			}
		}()
	}
	if s.telemetry != nil && req.Section == settings.SectionInverter {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			if _, err := s.telemetry.PollOnce(ctx); err != nil {
				s.log.Debug("telemetry poll after settings change failed", "err", err)
			}
		}()
	}
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

// inverterIdentity names the device a settings block points at.
type inverterIdentity struct {
	Type   string `json:"type"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Serial string `json:"serial"`
}

func (i inverterIdentity) String() string {
	label := i.Type
	if i.Serial != "" {
		return label + " · " + i.Serial
	}
	return label
}

type inverterSwap struct {
	From inverterIdentity
	To   inverterIdentity
}

// inverterSwap reports whether an inverter patch replaces the device, judged by
// its identity rather than by any settings edit.
func (s *Server) inverterSwap(r *http.Request, values json.RawMessage) (*inverterSwap, error) {
	current := s.settings.Get().Inverter
	from := inverterIdentity{
		Type: current.Type, Host: current.Host, Port: current.Port, Serial: current.Serial,
	}
	var next inverterIdentity
	next.Type = current.Type
	next.Host = current.Host
	next.Port = current.Port
	next.Serial = current.Serial
	if err := json.Unmarshal(values, &next); err != nil {
		return nil, err
	}
	if from == next {
		return nil, nil
	}
	// A change of limits, capacity or signs is an ordinary edit; only a change
	// of which device is being talked to is a swap.
	identityChanged := from.Type != next.Type || from.Host != next.Host ||
		from.Port != next.Port || from.Serial != next.Serial
	if !identityChanged {
		return nil, nil
	}
	return &inverterSwap{From: from, To: next}, nil
}

// prepareInverterSwap runs the ceremony before settings change: clear the old
// inverter's schedule, disarm writes and leave an event behind.
func (s *Server) prepareInverterSwap(ctx context.Context, swap *inverterSwap) {
	if s.telemetry != nil {
		if reader := s.telemetry.Reader(); reader != nil {
			if writer, ok := reader.(inverter.ScheduleWriter); ok {
				clearCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				if _, err := writer.WriteSchedule(clearCtx, domain.Schedule{}); err != nil {
					s.log.Warn("could not clear the old inverter schedule", "err", err)
				}
				cancel()
			}
			_ = reader.Close()
		}
	}
	if s.settings != nil {
		if _, err := s.settings.Patch(ctx, settings.SectionSafety,
			json.RawMessage(`{"writes_armed_ui":false}`)); err != nil {
			s.log.Warn("could not disarm writes after an inverter swap", "err", err)
		}
	}
	details, _ := json.Marshal(map[string]any{"from": swap.From, "to": swap.To})
	if err := s.st.InsertEvent(ctx, store.Event{
		Severity: "warning", Code: "inverter_replaced",
		Message: "Inverter replaced: " + swap.From.String() + " → " + swap.To.String(),
		Details: details,
	}); err != nil {
		s.log.Warn("could not record the inverter swap", "err", err)
	}
	// Recompute for the new hardware once the new settings are in place.
	if s.planner != nil {
		go func() {
			recomputeCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if _, err := s.planner.Compute(recomputeCtx); err != nil {
				s.log.Warn("recompute after an inverter swap failed", "err", err)
			}
		}()
	}
	s.log.Info("inverter replaced", "from", swap.From.String(), "to", swap.To.String())
}
