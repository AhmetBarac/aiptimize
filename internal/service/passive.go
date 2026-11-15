package service

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/settings"
)

// PassiveDriver is satisfied by inverters that take an instantaneous charge or
// discharge power instead of a weekly slot table. The SAJ H1 works this way: it
// has an app mode plus a passive power, so a slot schedule has to be applied by
// following the plan while it runs.
type PassiveDriver interface {
	SetPassive(ctx context.Context, mode int, watts float64) error
}

// PassiveWindowDriver is implemented by inverters whose passive command expires
// after a while (Huawei's forcible charge takes a duration in minutes). The
// controller passes the remaining slot time so the inverter's own timer matches
// the plan, and renews before it runs out.
type PassiveWindowDriver interface {
	SetPassiveFor(ctx context.Context, mode int, watts float64, window time.Duration) error
}

// Passive modes, matching the values the SAJ accepts on register 0x3636.
const (
	passiveStandby   = 0
	passiveDischarge = 1
	passiveCharge    = 2
)

// PassiveController keeps a passive-mode inverter in step with the active plan.
//
// It runs on a short tick rather than writing a schedule ahead of time, so the
// inverter only follows the plan while this process is alive; that is the
// trade-off of driving passive mode instead of programming the inverter's own
// time-of-use windows.
type PassiveController struct {
	values func() settings.Values
	latest func(ctx context.Context) (domain.Plan, error)
	driver func() PassiveDriver
	now    func() time.Time
	log    *slog.Logger
	step   time.Duration

	writesEnv bool

	mu     sync.Mutex
	last   string
	lastAt time.Time
	// refresh re-sends an unchanged state so a command with an expiry never
	// lapses while its slot is still running.
	refresh time.Duration
}

// NewPassiveController builds the controller. driver is a getter because the
// telemetry poller may rebuild the inverter connection when settings change.
func NewPassiveController(
	values func() settings.Values,
	latest func(ctx context.Context) (domain.Plan, error),
	driver func() PassiveDriver,
	log *slog.Logger,
	now func() time.Time,
) *PassiveController {
	if log == nil {
		log = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	return &PassiveController{
		values:  values,
		latest:  latest,
		driver:  driver,
		now:     now,
		log:     log,
		step:    30 * time.Second,
		refresh: 10 * time.Minute,
	}
}

// SetWriteGate mirrors the planner: writes need the environment gate too.
func (c *PassiveController) SetWriteGate(enabled bool) { c.writesEnv = enabled }

// Run applies the plan until the context is cancelled.
func (c *PassiveController) Run(ctx context.Context) {
	ticker := time.NewTicker(c.step)
	defer ticker.Stop()
	c.apply(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.apply(ctx)
		}
	}
}

// apply reads the current plan and sets the inverter to the slot covering now,
// or to standby when the plan has nothing for this moment.
func (c *PassiveController) apply(ctx context.Context) {
	driver := c.driver()
	if driver == nil {
		return
	}
	values := c.values()
	if !c.writesEnv || !values.Safety.WritesArmedUI {
		return
	}

	plan, err := c.latest(ctx)
	if err != nil || plan.Status != domain.PlanStatusApplied {
		// No applied plan means the battery should sit idle rather than follow
		// a suggestion the user has not committed to.
		c.set(ctx, driver, passiveStandby, 0)
		return
	}

	now := c.now()
	for _, slot := range plan.Slots {
		if now.Before(slot.Start) || !now.Before(slot.End) {
			continue
		}
		// The inverter reports power in whole watts and clamps internally; keep
		// the request inside the configured battery limits anyway.
		remaining := slot.End.Sub(now)
		watts := slot.PowerW
		if slot.Direction == domain.DirectionCharge {
			watts = math.Min(watts, values.Inverter.MaxChargeW)
			c.setWindow(ctx, driver, passiveCharge, math.Max(0, watts), remaining)
			return
		}
		watts = math.Min(watts, values.Inverter.MaxDischargeW)
		c.setWindow(ctx, driver, passiveDischarge, math.Max(0, watts), remaining)
		return
	}
	c.set(ctx, driver, passiveStandby, 0)
}

// set only writes when the requested state changed, so the inverter is not
// rewritten every tick.
func (c *PassiveController) set(ctx context.Context, driver PassiveDriver, mode int, watts float64) {
	c.setWindow(ctx, driver, mode, watts, 0)
}

// setWindow writes the state, and also renews an unchanged one once the refresh
// interval has passed so a command with an expiry does not lapse mid-slot.
func (c *PassiveController) setWindow(ctx context.Context, driver PassiveDriver, mode int, watts float64, window time.Duration) {
	state := stateKey(mode, watts)
	c.mu.Lock()
	if state == c.last && c.now().Sub(c.lastAt) < c.refresh {
		c.mu.Unlock()
		return
	}
	c.last = state
	c.lastAt = c.now()
	c.mu.Unlock()

	err := c.write(ctx, driver, mode, watts, window)
	if err != nil {
		// Forget the state so the next tick retries.
		c.mu.Lock()
		c.last = ""
		c.mu.Unlock()
		c.log.Warn("passive mode update failed", "mode", mode, "watts", watts, "err", err)
		return
	}
	c.log.Info("passive mode applied", "mode", mode, "watts", watts, "window", window)
}

// write picks the driver method that can carry an expiry.
func (c *PassiveController) write(ctx context.Context, driver PassiveDriver, mode int, watts float64, window time.Duration) error {
	if window > 0 {
		if timed, ok := driver.(PassiveWindowDriver); ok {
			return timed.SetPassiveFor(ctx, mode, watts, window)
		}
	}
	return driver.SetPassive(ctx, mode, watts)
}

func stateKey(mode int, watts float64) string {
	switch mode {
	case passiveCharge:
		return "charge:" + formatWatts(watts)
	case passiveDischarge:
		return "discharge:" + formatWatts(watts)
	default:
		return "standby"
	}
}

func formatWatts(watts float64) string {
	return strconv.Itoa(int(math.Round(watts)))
}
