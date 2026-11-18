package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/config"
	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/ev"
	"github.com/AhmetBarac/aiptimize/internal/prices"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

// EV errors map to API codes.
var (
	ErrEVUnavailable  = errors.New("ev: charger is unavailable")
	ErrEVPlanInvalid  = errors.New("ev: invalid charging plan")
	ErrEVPlanNotFound = errors.New("ev: no charging plan")
)

// EVBatteryHold coordinates the house battery while an EV is charging.
type EVBatteryHold interface {
	SetEVCharging(ctx context.Context, charging bool) error
}

// EVService owns the charger adapter, EV plans and the smart-charging loop.
type EVService struct {
	cfg         *settings.Service
	st          *store.Store
	prices      *prices.Service
	forecast    *ForecastService
	charger     ev.Charger
	batteryHold EVBatteryHold
	log         *slog.Logger
	now         func() time.Time

	control config.EVControlMode
	mu      sync.Mutex
	latest  *domain.EVPlanPreview
	lastSet time.Time
	powerW  float64

	lastStatusOK  time.Time
	lastStatusErr time.Time

	provider        string
	restartRequired bool
	reconfiguring   bool

	// blockedReason explains why remote control is refused, e.g. eco mode.
	blockedReason string
	blockedAt     time.Time
}

// NewEVService builds the EV service. The simulator is used when configured;
// no cloud credentials are needed for demos and tests.
func NewEVService(cfg *settings.Service, st *store.Store, priceSvc *prices.Service,
	forecast *ForecastService, log *slog.Logger, simulator bool, control config.EVControlMode,
	now func() time.Time) *EVService {
	var charger ev.Charger
	if simulator {
		if now == nil {
			now = time.Now
		}
		charger = ev.NewSimulator(ev.SimulatorConfig{
			ID: "sim-ev-1", Phases: 3, VoltageV: 230, MinCurrent: 6, MaxCurrent: 16,
			Connected: true, Now: now,
		})
	}
	return NewEVServiceWithCharger(cfg, st, priceSvc, forecast, log, charger, control, now)
}

// NewEVServiceWithCharger builds the EV service with an explicit adapter.
func NewEVServiceWithCharger(cfg *settings.Service, st *store.Store, priceSvc *prices.Service,
	forecast *ForecastService, log *slog.Logger, charger ev.Charger, control config.EVControlMode,
	now func() time.Time) *EVService {
	if now == nil {
		now = time.Now
	}
	if log == nil {
		log = slog.Default()
	}
	provider := ""
	if charger != nil {
		provider = providerForType(charger.Type())
	}
	return &EVService{cfg: cfg, st: st, prices: priceSvc, forecast: forecast,
		charger: charger, log: log, now: now, control: control.Effective(), provider: provider}
}

func providerForType(typ string) string {
	switch typ {
	case "zaptec_cloud":
		return "zaptec"
	case "simulator":
		return "simulator"
	default:
		return typ
	}
}

// Charger returns the adapter (nil when no adapter is configured).
func (s *EVService) Charger() ev.Charger { return s.charger }

// evHealthWindow is how long a successful status poll keeps the automatic
// environment gate open. The controller polls every 15 s, so this tolerates a
// few missed polls before the link is considered lost.
const evHealthWindow = 60 * time.Second

// Healthy reports whether an adapter is configured and its most recent status
// poll succeeded within the health window. A failed poll closes the link until
// the next successful one.
func (s *EVService) Healthy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.charger == nil || s.lastStatusOK.IsZero() || !s.lastStatusErr.IsZero() {
		return false
	}
	return s.now().Sub(s.lastStatusOK) <= evHealthWindow
}

// evControlEnv reports the environment half of the EV gate. In the default
// auto mode it follows the charger link, so a configured charger that answers
// status polls opens the gate without any environment variable.
func (s *EVService) evControlEnv() bool {
	return s.control.Resolve(s.Healthy())
}

// recordStatus feeds one charger status poll into the link health tracker.
func (s *EVService) recordStatus(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if err != nil {
		s.lastStatusErr = now
		return
	}
	s.lastStatusOK = now
	s.lastStatusErr = time.Time{}
}

// SetBatteryHold wires the battery interlock used during EV charging.
func (s *EVService) SetBatteryHold(hold EVBatteryHold) { s.batteryHold = hold }

// PowerW returns the latest observed EV charging power.
func (s *EVService) PowerW() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.powerW
}

func (s *EVService) rememberPower(powerW float64) {
	s.mu.Lock()
	s.powerW = math.Max(0, powerW)
	s.mu.Unlock()
}

// ID returns the configured charger id.
func (s *EVService) ID() string {
	if s.charger == nil {
		return "ev-1"
	}
	return s.charger.ID()
}

// ActiveProvider returns the provider of the running adapter.
func (s *EVService) ActiveProvider() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.provider
}

// ConfiguredProvider returns the provider persisted in settings.
func (s *EVService) ConfiguredProvider() string {
	provider := strings.ToLower(strings.TrimSpace(s.cfg.Get().EV.Provider))
	if provider == "" {
		return "zaptec"
	}
	return provider
}

// RestartRequired reports whether a provider change is waiting for the process
// to restart.
func (s *EVService) RestartRequired() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restartRequired
}

// MarkRestartRequired records that the running adapter no longer matches the
// persisted provider configuration.
func (s *EVService) MarkRestartRequired() {
	s.mu.Lock()
	s.restartRequired = true
	s.mu.Unlock()
}

// StopForReconfigure stops the active charging session before a provider
// change. It leaves the charger in a safe stopped state.
func (s *EVService) StopForReconfigure(ctx context.Context) error {
	s.mu.Lock()
	if s.reconfiguring {
		s.mu.Unlock()
		return fmt.Errorf("EV reconfiguration is already in progress")
	}
	s.reconfiguring = true
	s.mu.Unlock()

	fail := func(err error) error {
		s.mu.Lock()
		s.reconfiguring = false
		s.mu.Unlock()
		return err
	}
	if s.charger == nil {
		s.mu.Lock()
		s.reconfiguring = false
		s.mu.Unlock()
		return nil
	}
	status, err := s.charger.Status(ctx)
	if err != nil {
		// A newly configured adapter may not authenticate yet. Allow the
		// credential update to proceed; the control loop stays stopped until
		// the adapter is refreshed below.
		s.mu.Lock()
		s.reconfiguring = false
		s.mu.Unlock()
		return nil
	}
	if !status.Charging {
		s.mu.Lock()
		s.reconfiguring = false
		s.mu.Unlock()
		return nil
	}
	stopper, ok := s.charger.(ev.SessionStopper)
	if !ok {
		return fail(fmt.Errorf("charger cannot explicitly stop an active session"))
	}
	if err := stopper.StopSession(ctx); err != nil {
		return fail(fmt.Errorf("stop active EV session: %w", err))
	}
	deadline := s.now().Add(20 * time.Second)
	for {
		current, err := s.charger.Status(ctx)
		if err == nil && !current.Charging {
			_ = s.st.RecordEVCommand(ctx, s.ID(), "stop_session", "{}", "ok", "")
			return nil
		}
		if s.now().After(deadline) {
			return fail(fmt.Errorf("EV session did not stop within 20 seconds"))
		}
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// ApplyProviderConfig stops any active session, persists the selected adapter
// configuration and marks a restart as required.
func (s *EVService) ApplyProviderConfig(ctx context.Context, provider, chargerID, installationID, zaptecUsername, zaptecPassword, easeeUsername, easeePassword, wallboxEmail, wallboxPassword string) (settings.EV, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	chargerID = strings.TrimSpace(chargerID)
	installationID = strings.TrimSpace(installationID)
	zaptecUsername = strings.TrimSpace(zaptecUsername)
	easeeUsername = strings.TrimSpace(easeeUsername)
	wallboxEmail = strings.TrimSpace(wallboxEmail)
	switch provider {
	case "zaptec", "easee", "wallbox", "simulator":
	default:
		return settings.EV{}, fmt.Errorf("provider must be zaptec, easee, wallbox or simulator")
	}
	values := map[string]string{
		"provider":        provider,
		"charger_id":      chargerID,
		"installation_id": installationID,
		"zaptec_username": zaptecUsername,
	}
	if zaptecPassword != "" {
		values["zaptec_password"] = zaptecPassword
	}
	values["easee_username"] = easeeUsername
	values["wallbox_email"] = wallboxEmail
	if wallboxPassword != "" {
		values["wallbox_password"] = wallboxPassword
	}
	if easeePassword != "" {
		values["easee_password"] = easeePassword
	}
	patch, err := json.Marshal(values)
	if err != nil {
		return settings.EV{}, err
	}
	// Validate the new values before stopping the current session.
	validated := s.cfg.Get().EV
	if err := json.Unmarshal(patch, &validated); err != nil {
		return settings.EV{}, err
	}
	if err := settings.ValidateSection(settings.SectionEV, &validated); err != nil {
		return settings.EV{}, err
	}
	if err := s.StopForReconfigure(ctx); err != nil {
		return settings.EV{}, err
	}
	if _, err := s.cfg.Patch(ctx, settings.SectionEV, patch); err != nil {
		s.mu.Lock()
		s.reconfiguring = false
		s.mu.Unlock()
		return settings.EV{}, err
	}
	if provider == "zaptec" {
		if z, ok := s.charger.(*ev.Zaptec); ok {
			updated := s.cfg.Get().EV
			z.UpdateConfig(chargerID, installationID, updated.ZaptecUsername, updated.ZaptecPassword)
			s.mu.Lock()
			s.provider = "zaptec"
			s.restartRequired = false
			s.reconfiguring = false
			s.mu.Unlock()
			return s.cfg.Get().EV, nil
		}
	}
	s.MarkRestartRequired()
	return s.cfg.Get().EV, nil
}

// Status returns the live charger state with the configured mode.
func (s *EVService) Status(ctx context.Context) (domain.ChargerStatus, error) {
	if s.charger == nil {
		return domain.ChargerStatus{}, ErrEVUnavailable
	}
	status, err := s.charger.Status(ctx)
	s.recordStatus(err)
	if err != nil {
		return domain.ChargerStatus{}, err
	}
	status.Mode = s.cfg.Get().EV.Mode
	s.rememberPower(status.PowerW)
	return status, nil
}

// Config returns the EV configuration.
func (s *EVService) Config() settings.EV { return s.cfg.Get().EV }

// PatchConfig applies a validated EV settings patch.
func (s *EVService) PatchConfig(ctx context.Context, raw []byte) (settings.EV, error) {
	if _, err := s.cfg.Patch(ctx, settings.SectionEV, raw); err != nil {
		return settings.EV{}, err
	}
	return s.cfg.Get().EV, nil
}

// SetMode changes the charging mode and applies the immediate adapter state.
func (s *EVService) SetMode(ctx context.Context, mode string) (domain.ChargerStatus, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "off", "now", "smart":
	case "pv", "minpv":
		mode = "smart"
	default:
		return domain.ChargerStatus{}, fmt.Errorf("invalid mode %q", mode)
	}
	if _, err := s.cfg.Patch(ctx, settings.SectionEV, []byte(fmt.Sprintf(`{"mode":%q}`, mode))); err != nil {
		return domain.ChargerStatus{}, err
	}
	if s.charger != nil {
		switch mode {
		case "off":
			_ = s.charger.Pause(ctx)
		case "now":
			_ = s.charger.Resume(ctx)
			_ = s.charger.SetCurrent(ctx, s.Config().MaxCurrentA)
		case "smart":
			_ = s.charger.Resume(ctx)
			_ = s.charger.SetCurrent(ctx, s.Config().MinCurrentA)
		}
	}
	return s.Status(ctx)
}

// SetCurrent applies a manual current limit.
func (s *EVService) SetCurrent(ctx context.Context, amps float64) (domain.ChargerStatus, error) {
	if s.charger == nil {
		return domain.ChargerStatus{}, ErrEVUnavailable
	}
	cfg := s.Config()
	if amps < cfg.MinCurrentA || amps > cfg.MaxCurrentA {
		return domain.ChargerStatus{}, fmt.Errorf("current must be between %.0f and %.0f A", cfg.MinCurrentA, cfg.MaxCurrentA)
	}
	if err := s.charger.SetCurrent(ctx, amps); err != nil {
		if errors.Is(err, ev.ErrEcoMode) {
			s.noteBlocked("eco_mode")
		}
		return domain.ChargerStatus{}, err
	}
	s.clearBlocked()
	s.mu.Lock()
	s.lastSet = s.now()
	s.mu.Unlock()
	_ = s.st.RecordEVCommand(ctx, s.ID(), "set_current", fmt.Sprintf(`{"amps":%.1f}`, amps), "ok", "")
	return s.Status(ctx)
}

// Pause pauses the charger.
func (s *EVService) Pause(ctx context.Context) (domain.ChargerStatus, error) {
	if s.charger == nil {
		return domain.ChargerStatus{}, ErrEVUnavailable
	}
	if err := s.charger.Pause(ctx); err != nil {
		return domain.ChargerStatus{}, err
	}
	_ = s.st.RecordEVCommand(ctx, s.ID(), "pause", "{}", "ok", "")
	return s.Status(ctx)
}

// Resume resumes the charger.
func (s *EVService) Resume(ctx context.Context) (domain.ChargerStatus, error) {
	if s.charger == nil {
		return domain.ChargerStatus{}, ErrEVUnavailable
	}
	if err := s.charger.Resume(ctx); err != nil {
		return domain.ChargerStatus{}, err
	}
	_ = s.st.RecordEVCommand(ctx, s.ID(), "resume", "{}", "ok", "")
	return s.Status(ctx)
}

// PowerLimits returns min/max charging power in watts.
func (s *EVService) PowerLimits() (minW, maxW float64) {
	cfg := s.Config()
	phases := cfg.Phases
	if s.charger != nil && s.charger.Capabilities().Phases > 0 {
		phases = s.charger.Capabilities().Phases
	}
	minW = cfg.MinCurrentA * float64(phases) * cfg.VoltageV
	maxA := cfg.MaxCurrentA
	if s.charger != nil {
		if cap := s.charger.Capabilities(); cap.MaxCurrentA > 0 && cap.MaxCurrentA < maxA {
			maxA = cap.MaxCurrentA
		}
	}
	maxW = maxA * float64(phases) * cfg.VoltageV
	if siteMax := s.cfg.Get().Site.MaxImportPowerW; siteMax > 0 && maxW > siteMax {
		maxW = siteMax
	}
	return minW, maxW
}

// Plans returns the charger's plans.
func (s *EVService) Plans(ctx context.Context) ([]domain.EVPlan, error) {
	return s.st.ListEVPlans(ctx, s.ID())
}

// LatestPreview returns the last computed EV plan preview.
func (s *EVService) LatestPreview() (domain.EVPlanPreview, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest == nil {
		return domain.EVPlanPreview{}, false
	}
	return *s.latest, true
}

// SaveStaticPlan stores a static plan and computes a preview.
func (s *EVService) SaveStaticPlan(ctx context.Context, plan domain.EVPlan) (domain.EVPlan, domain.EVPlanPreview, error) {
	plan.ChargerID = s.ID()
	plan.Kind = "static"
	if plan.DeadlineLocal == "" || plan.TargetKWh <= 0 {
		return plan, domain.EVPlanPreview{}, ErrEVPlanInvalid
	}
	if plan.Strategy == "" {
		plan.Strategy = s.Config().PlanDefaults.Strategy
	}
	saved, err := s.st.SaveEVPlan(ctx, plan)
	if err != nil {
		return plan, domain.EVPlanPreview{}, err
	}
	preview, err := s.ComputePlan(ctx, saved)
	return saved, preview, err
}

// ActivatePlan makes an existing plan the one schedule in force and returns its
// freshly computed preview.
func (s *EVService) ActivatePlan(ctx context.Context, id int64) (domain.EVPlan, domain.EVPlanPreview, error) {
	plans, err := s.st.ListEVPlans(ctx, s.ID())
	if err != nil {
		return domain.EVPlan{}, domain.EVPlanPreview{}, err
	}
	var target domain.EVPlan
	found := false
	for _, plan := range plans {
		if plan.ID == id {
			target = plan
			found = true
			break
		}
	}
	if !found {
		return domain.EVPlan{}, domain.EVPlanPreview{}, ErrEVPlanNotFound
	}
	if err := s.st.ActivateEVPlan(ctx, s.ID(), id); err != nil {
		return domain.EVPlan{}, domain.EVPlanPreview{}, err
	}
	target.Active = true
	preview, err := s.ComputePlan(ctx, target)
	return target, preview, err
}

// SaveRepeatingPlans replaces all repeating plans.
func (s *EVService) SaveRepeatingPlans(ctx context.Context, plans []domain.EVPlan) ([]domain.EVPlan, error) {
	for i := range plans {
		plans[i].ChargerID = s.ID()
		if plans[i].Kind == "" {
			plans[i].Kind = "repeating"
		}
		if plans[i].DeadlineLocal == "" || len(plans[i].Weekdays) == 0 {
			return nil, ErrEVPlanInvalid
		}
	}
	out := make([]domain.EVPlan, 0, len(plans))
	for _, plan := range plans {
		saved, err := s.st.SaveEVPlan(ctx, plan)
		if err != nil {
			return nil, err
		}
		out = append(out, saved)
	}
	return out, nil
}

// DeletePlan removes a plan.
func (s *EVService) DeletePlan(ctx context.Context, id int64) error {
	return s.st.DeleteEVPlan(ctx, id)
}

// ComputePlan builds a preview for a stored plan.
func (s *EVService) ComputePlan(ctx context.Context, plan domain.EVPlan) (domain.EVPlanPreview, error) {
	return s.computePlan(ctx, plan, true)
}

// PreviewPlan computes without saving, so asking for a preview never replaces
// the plan the controller is following.
func (s *EVService) PreviewPlan(ctx context.Context, plan domain.EVPlan) (domain.EVPlanPreview, error) {
	return s.computePlan(ctx, plan, false)
}

func (s *EVService) computePlan(ctx context.Context, plan domain.EVPlan, publish bool) (domain.EVPlanPreview, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return domain.EVPlanPreview{}, err
	}
	deadline, err := s.planDeadline(plan)
	if err != nil {
		return domain.EVPlanPreview{}, err
	}
	cfg := s.Config()
	loc := s.cfg.Location()
	now := s.now().In(loc)
	minW, maxW := s.PowerLimits()
	// Start at the current quarter hour, not at now: the slot that is already
	// delivering energy must reach the optimiser so it can pin it.
	priceSnap, err := s.prices.Snapshot(ctx, now.Truncate(15*time.Minute), deadline)
	if err != nil {
		return domain.EVPlanPreview{}, err
	}
	pvByHalf := map[int64]float64{}
	if s.forecast != nil {
		if snap, ok := s.forecast.Latest(); ok {
			for _, point := range snap.Points {
				pvByHalf[point.Start.Unix()] = point.PVW - point.LoadW
			}
		}
	}
	slots := make([]ev.Slot, 0, len(priceSnap.Points))
	for _, point := range priceSnap.Points {
		// Keep a slot that has already begun but not finished; the optimizer
		// clamps it to now.
		if !point.DeliveryEnd.After(now) || !point.DeliveryStart.Before(deadline) {
			continue
		}
		surplus := pvByHalf[point.DeliveryStart.UTC().Truncate(30*time.Minute).Unix()]
		slots = append(slots, ev.Slot{
			Start: point.DeliveryStart, End: point.DeliveryEnd,
			ImportPrice: priceSnap.Tariff.ImportPrice(point.PriceKWh),
			ExportPrice: priceSnap.Tariff.ExportPrice(point.PriceKWh),
			PVSurplusW:  math.Max(0, surplus),
		})
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].Start.Before(slots[j].Start) })
	if len(slots) == 0 {
		return domain.EVPlanPreview{}, fmt.Errorf("%w: no tariff coverage before the deadline", ErrEVPlanInvalid)
	}
	inputs := ev.PlanInputs{
		Now: now, Deadline: deadline, TargetKWh: plan.TargetKWh, SessionKWh: status.SessionKWh,
		PowerW: maxW, MinPowerW: minW, GridAllowed: cfg.GridChargeAllowed,
		Strategy: plan.Strategy, PreconditionMin: plan.PreconditionMin, Slots: slots,
	}
	preview := ev.Optimize(inputs)
	preview.ChargerID = s.ID()
	if cfg.Mode != "smart" {
		preview.ModeMismatch = true
		preview.Warnings = append(preview.Warnings, "Plan only applies in smart mode.")
	}
	if !cfg.GridChargeAllowed {
		preview.Warnings = append(preview.Warnings, "Grid charging is disabled; the plan uses PV surplus only.")
	}
	if !publish {
		return preview, nil
	}
	planID := plan.ID
	if err := s.st.SaveEVPlanRun(ctx, &planID, preview); err != nil {
		s.log.Warn("save EV plan run failed", "err", err)
	}
	s.mu.Lock()
	s.latest = &preview
	s.mu.Unlock()
	return preview, nil
}

func (s *EVService) planDeadline(plan domain.EVPlan) (time.Time, error) {
	loc := s.cfg.Location()
	if plan.Kind == "repeating" && len(plan.Weekdays) > 0 {
		return nextRepeatingDeadline(s.now().In(loc), plan.Weekdays, plan.DeadlineLocal, loc)
	}
	deadline, err := time.ParseInLocation("2006-01-02T15:04", plan.DeadlineLocal, loc)
	if err != nil {
		if deadline, err = time.Parse(time.RFC3339, plan.DeadlineLocal); err != nil {
			return time.Time{}, ErrEVPlanInvalid
		}
	}
	return deadline, nil
}

func nextRepeatingDeadline(now time.Time, weekdays []int, hhmm string, loc *time.Location) (time.Time, error) {
	parts := strings.Split(hhmm, ":")
	if len(parts) != 2 {
		return time.Time{}, ErrEVPlanInvalid
	}
	var hour, minute int
	if _, err := fmt.Sscanf(hhmm, "%d:%d", &hour, &minute); err != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return time.Time{}, ErrEVPlanInvalid
	}
	allowed := map[int]bool{}
	for _, d := range weekdays {
		allowed[d] = true
	}
	for offset := 0; offset < 8; offset++ {
		day := now.AddDate(0, 0, offset)
		iso := int(day.Weekday())
		if iso == 0 {
			iso = 7
		}
		if !allowed[iso] {
			continue
		}
		candidate := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, loc)
		if candidate.After(now) {
			return candidate, nil
		}
	}
	return time.Time{}, ErrEVPlanInvalid
}

// Session returns the latest session, if any.
func (s *EVService) Session(ctx context.Context) (domain.EVSession, error) {
	session, err := s.st.LatestEVSession(ctx, s.ID())
	if errors.Is(err, store.ErrNotFound) {
		return domain.EVSession{}, ErrEVPlanNotFound
	}
	return session, err
}

// EVLoadW returns the planned EV load at a 30-minute block start.
func (s *EVService) EVLoadW(start time.Time) float64 {
	if s.cfg.Get().EV.Mode != "smart" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest == nil {
		return 0
	}
	for _, window := range s.latest.Windows {
		if !start.Before(window.Start) && start.Before(window.End) {
			return window.PowerW
		}
		if start.Before(window.End) && start.Add(30*time.Minute).After(window.Start) {
			return window.PowerW
		}
	}
	return 0
}

// EVGridWindows marks blocks covered by a grid-sourced EV charging window.
func (s *EVService) EVGridWindows(from, to time.Time) []bool {
	if s.cfg.Get().EV.Mode != "smart" {
		return make([]bool, int(to.Sub(from)/(30*time.Minute)))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := int(to.Sub(from) / (30 * time.Minute))
	out := make([]bool, n)
	if s.latest == nil {
		return out
	}
	for i := 0; i < n; i++ {
		start := from.Add(time.Duration(i) * 30 * time.Minute)
		end := start.Add(30 * time.Minute)
		for _, window := range s.latest.Windows {
			if window.Source == "grid" && start.Before(window.End) && end.After(window.Start) {
				out[i] = true
				break
			}
		}
	}
	return out
}

// Run evaluates the smart-charging controller every 15 seconds. It only sends
// commands when the EV environment gate (automatic by default) and the in-app
// UI gate are both armed.
func (s *EVService) Run(ctx context.Context) {
	// Poll once at startup so the link health (and the automatic environment
	// gate) has a value before the first 15 s tick.
	s.tick(ctx)
	s.refreshLatest(ctx)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	refreshes := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
			refreshes++
			if refreshes%20 == 0 {
				s.refreshLatest(ctx)
			}
		}
	}
}

// refreshLatest recomputes the earliest active static plan for the controller.
func (s *EVService) refreshLatest(ctx context.Context) {
	if s.charger == nil {
		return
	}
	plans, err := s.Plans(ctx)
	if err != nil {
		return
	}
	var candidate *domain.EVPlan
	for i := range plans {
		plan := plans[i]
		if !plan.Active || plan.Kind != "static" {
			continue
		}
		deadline, err := s.planDeadline(plan)
		if err != nil || !deadline.After(s.now()) {
			continue
		}
		if candidate == nil || deadline.Before(mustDeadline(s, *candidate)) {
			candidate = &plan
		}
	}
	if candidate == nil {
		return
	}
	if _, err := s.ComputePlan(ctx, *candidate); err != nil {
		s.log.Warn("EV plan refresh failed", "err", err)
	}
}

func mustDeadline(s *EVService, plan domain.EVPlan) time.Time {
	deadline, err := s.planDeadline(plan)
	if err != nil {
		return time.Time{}
	}
	return deadline
}

func (s *EVService) tick(ctx context.Context) {
	if s.charger == nil {
		return
	}
	s.mu.Lock()
	reconfiguring := s.reconfiguring
	s.mu.Unlock()
	if reconfiguring {
		return
	}
	status, err := s.charger.Status(ctx)
	s.recordStatus(err)
	if err != nil {
		s.log.Warn("EV status failed", "err", err)
		return
	}
	s.rememberPower(status.PowerW)
	if s.batteryHold != nil {
		if err := s.batteryHold.SetEVCharging(ctx, status.Charging); err != nil {
			s.log.Warn("EV battery hold failed", "err", err)
		}
	}
	cfg := s.cfg.Get()
	if !s.evControlEnv() || !cfg.Safety.EVControlArmedUI {
		return
	}
	now := s.now()
	switch cfg.EV.Mode {
	case "off":
		if !status.Paused {
			_ = s.charger.Pause(ctx)
		}
	case "now":
		if status.Paused {
			_ = s.charger.Resume(ctx)
		}
		s.setCurrentLimited(ctx, cfg.EV.MaxCurrentA)
	case "smart":
		if !status.Connected {
			return
		}
		s.mu.Lock()
		preview := s.latest
		s.mu.Unlock()
		if preview == nil {
			return
		}
		active := false
		for _, window := range preview.Windows {
			if !now.Before(window.Start) && now.Before(window.End) {
				active = true
				break
			}
		}
		remaining := preview.RemainingKWh
		if remaining <= 0 {
			if !status.Paused {
				_ = s.charger.Pause(ctx)
				s.log.Info("EV target reached; paused")
			}
			return
		}
		if !active {
			if !status.Paused && status.Charging {
				_ = s.charger.Pause(ctx)
			}
			return
		}
		if status.Paused {
			_ = s.charger.Resume(ctx)
		}
		source := preview.Windows[0].Source
		for _, window := range preview.Windows {
			if !now.Before(window.Start) && now.Before(window.End) {
				source = window.Source
				break
			}
		}
		if source == "pv" {
			s.setCurrentLimited(ctx, cfg.EV.MinCurrentA)
		} else {
			s.setCurrentLimited(ctx, cfg.EV.MaxCurrentA)
		}
	}
}

// ControlBlocked reports why charger control is refused, and since when. An
// empty reason means control is available.
func (s *EVService) ControlBlocked() (string, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blockedReason, s.blockedAt
}

func (s *EVService) noteBlocked(reason string) {
	s.mu.Lock()
	s.blockedReason = reason
	s.blockedAt = s.now()
	s.mu.Unlock()
}

func (s *EVService) clearBlocked() {
	s.mu.Lock()
	s.blockedReason = ""
	s.blockedAt = time.Time{}
	s.mu.Unlock()
}

func (s *EVService) setCurrentLimited(ctx context.Context, amps float64) {
	s.mu.Lock()
	if !s.lastSet.IsZero() && s.now().Sub(s.lastSet) < 15*time.Minute {
		s.mu.Unlock()
		return
	}
	s.lastSet = s.now()
	s.mu.Unlock()
	if err := s.charger.SetCurrent(ctx, amps); err != nil {
		if errors.Is(err, ev.ErrEcoMode) {
			s.noteBlocked("eco_mode")
			s.log.Warn("EV current change blocked by eco mode", "err", err)
			return
		}
		s.log.Warn("EV current change failed", "err", err)
		return
	}
	s.clearBlocked()
}
