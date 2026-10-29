package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/inverter"
	"github.com/AhmetBarac/aiptimize/internal/inverter/simulator"
	"github.com/AhmetBarac/aiptimize/internal/inverter/solplanet"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

// TelemetryOptions controls how the poller builds a reader.
type TelemetryOptions struct {
	Simulator bool
	Version   string
	// NewReader is an injection point for tests. When nil, the real Solplanet
	// client or the in-process simulator is used.
	NewReader func(values settings.Values) (inverter.Reader, error)
}

// TelemetryPoller polls the inverter every configured interval, stores each
// sample and keeps the latest snapshot for /status and SSE.
type TelemetryPoller struct {
	cfg  *settings.Service
	st   *store.Store
	log  *slog.Logger
	pub  Publisher
	opts TelemetryOptions
	now  func() time.Time

	mu        sync.Mutex
	reader    inverter.Reader
	signature string
	latest    domain.Telemetry
	latestAt  time.Time
	failures  int
}

// NewTelemetryPoller builds the poller.
func NewTelemetryPoller(cfg *settings.Service, st *store.Store, log *slog.Logger, pub Publisher, opts TelemetryOptions) *TelemetryPoller {
	if log == nil {
		log = slog.Default()
	}
	return &TelemetryPoller{cfg: cfg, st: st, log: log, pub: pub, opts: opts, now: time.Now}
}

// Run polls until ctx is cancelled.
func (p *TelemetryPoller) Run(ctx context.Context) {
	defer p.closeReader()
	p.poll(ctx)
	for {
		interval := time.Duration(p.cfg.Get().Inverter.TelemetryIntervalS) * time.Second
		if interval < 5*time.Second {
			interval = 30 * time.Second
		}
		if interval > time.Hour {
			interval = time.Hour
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			p.poll(ctx)
		}
	}
}

// ReadSchedule reads the inverter's weekly schedule without writing.
func (p *TelemetryPoller) ReadSchedule(ctx context.Context) (domain.Schedule, error) {
	reader, err := p.readerFor(p.cfg.Get())
	if err != nil {
		return domain.Schedule{}, err
	}
	return reader.Schedule(ctx)
}

// ApplySchedule writes a schedule when the reader supports writes, returning
// the readback and whether a write was needed. The caller enforces the gates.
func (p *TelemetryPoller) ApplySchedule(ctx context.Context, desired domain.Schedule) (domain.Schedule, bool, error) {
	reader, err := p.readerFor(p.cfg.Get())
	if err != nil {
		return domain.Schedule{}, false, err
	}
	current, err := reader.Schedule(ctx)
	if err != nil {
		return domain.Schedule{}, false, err
	}
	if ScheduleEqual(current, desired) {
		return current, false, nil
	}
	writer, ok := reader.(inverter.ScheduleWriter)
	if !ok {
		return domain.Schedule{}, false, errors.New("inverter does not support schedule writes")
	}
	written, err := writer.WriteSchedule(ctx, desired)
	if err != nil {
		return domain.Schedule{}, false, err
	}
	return written, true, nil
}

// Latest returns the newest sample and the time it was received.
func (p *TelemetryPoller) Latest() (domain.Telemetry, time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.latest.TS.IsZero() {
		return domain.Telemetry{}, time.Time{}, false
	}
	return p.latest, p.latestAt, true
}

// Test performs a read-only connectivity check with the current settings.
func (p *TelemetryPoller) Test(ctx context.Context) (inverter.Identity, inverter.Reading, error) {
	values := p.cfg.Get()
	reader, err := p.readerFor(values)
	if err != nil {
		return inverter.Identity{}, inverter.Reading{}, err
	}
	identity, err := reader.Identity(ctx)
	if err != nil {
		return inverter.Identity{}, inverter.Reading{}, err
	}
	reading, err := reader.Read(ctx)
	if err != nil {
		return inverter.Identity{}, inverter.Reading{}, err
	}
	return identity, reading, nil
}

func (p *TelemetryPoller) poll(ctx context.Context) {
	_, _ = p.PollOnce(ctx)
}

// PollOnce performs one poll immediately. It is used by tests and the CLI.
func (p *TelemetryPoller) PollOnce(ctx context.Context) (domain.Telemetry, error) {
	values := p.cfg.Get()
	reader, err := p.readerFor(values)
	if err != nil {
		p.fail(err, "reader")
		return domain.Telemetry{}, err
	}
	reading, err := reader.Read(ctx)
	if err != nil {
		p.fail(err, "read")
		return domain.Telemetry{}, err
	}
	sample := domain.Telemetry{
		TS:           reading.At.UTC(),
		SOC:          reading.SOC,
		PVW:          reading.PVW,
		PVStrings:    reading.PVStrings,
		GridW:        reading.GridW,
		BatteryW:     reading.BatteryW,
		LoadW:        reading.LoadW,
		InverterMode: reading.Mode,
		Extra:        reading.Extra,
	}
	if sample.PVStrings == nil {
		sample.PVStrings = []float64{}
	}
	if sample.Extra == nil {
		sample.Extra = map[string]any{}
	}
	if sample.TS.IsZero() {
		sample.TS = p.now().UTC()
	}

	storeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = p.st.InsertTelemetry(storeCtx, sample)
	cancel()
	if err != nil {
		p.log.Warn("store telemetry failed", "err", err)
	}

	p.mu.Lock()
	p.latest = sample
	p.latestAt = p.now()
	p.failures = 0
	p.mu.Unlock()

	if p.pub != nil {
		p.pub.Publish("telemetry", map[string]any{
			"soc":       sample.SOC,
			"pv_w":      sample.PVW,
			"grid_w":    sample.GridW,
			"battery_w": sample.BatteryW,
			"load_w":    sample.LoadW,
			"ts":        sample.TS,
		})
	}
	return sample, nil
}

func (p *TelemetryPoller) fail(err error, stage string) {
	p.mu.Lock()
	p.failures++
	failures := p.failures
	p.mu.Unlock()
	if failures == 1 || failures%10 == 0 {
		p.log.Warn("telemetry poll failed", "stage", stage, "failures", failures, "err", err)
	}
	if failures == 3 {
		eventCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = p.st.InsertEvent(eventCtx, store.Event{
			Severity: "warning",
			Code:     "telemetry_unavailable",
			Message:  "Inverter telemetry is unavailable: " + err.Error(),
		})
	}
}

func (p *TelemetryPoller) closeReader() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reader != nil {
		_ = p.reader.Close()
		p.reader = nil
		p.signature = ""
	}
}

// readerFor returns the cached reader, rebuilding it when the connection
// parameters or simulator configuration changed.
func (p *TelemetryPoller) readerFor(values settings.Values) (inverter.Reader, error) {
	signature := p.signatureFor(values)
	p.mu.Lock()
	if p.reader != nil && p.signature == signature {
		reader := p.reader
		p.mu.Unlock()
		return reader, nil
	}
	old := p.reader
	p.reader = nil
	p.signature = ""
	p.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}

	reader, err := p.buildReader(values)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.reader = reader
	p.signature = signature
	p.mu.Unlock()
	return reader, nil
}

func (p *TelemetryPoller) buildReader(values settings.Values) (inverter.Reader, error) {
	if p.opts.NewReader != nil {
		return p.opts.NewReader(values)
	}
	if p.opts.Simulator {
		peak := 0.0
		for _, str := range values.PV.Strings {
			peak += str.KWP * 1000
		}
		if peak <= 0 {
			peak = 6000
		}
		return simulator.New(simulator.Config{
			Serial:        "SIM-ASW08KH-0001",
			Strings:       len(values.PV.Strings),
			PeakW:         peak,
			CapacityKWh:   values.Inverter.CapacityKWh,
			MaxChargeW:    values.Inverter.MaxChargeW,
			MaxDischargeW: values.Inverter.MaxDischargeW,
			StartSOC:      55,
			Location:      p.cfg.Location(),
			Signs:         signs(values),
		}), nil
	}
	inv := values.Inverter
	if strings.TrimSpace(inv.Host) == "" {
		return nil, errors.New("inverter host is not configured")
	}
	return solplanet.New(solplanet.Config{
		Scheme:    inv.Scheme,
		Host:      inv.Host,
		Port:      inv.Port,
		Serial:    inv.Serial,
		UserAgent: fmt.Sprintf("aiptimize/%s (+https://github.com/AhmetBarac/aiptimize)", p.opts.Version),
		Signs:     signs(values),
		Logger:    p.log,
	}), nil
}

func signs(values settings.Values) inverter.Signs {
	return inverter.Signs{
		PV:      values.Inverter.SignOverrides.PV,
		Grid:    values.Inverter.SignOverrides.Grid,
		Battery: values.Inverter.SignOverrides.Battery,
	}
}

func (p *TelemetryPoller) signatureFor(values settings.Values) string {
	inv := values.Inverter
	var stringsCount int
	var kwp float64
	for _, str := range values.PV.Strings {
		stringsCount++
		kwp += str.KWP
	}
	return fmt.Sprintf("%s|%s|%d|%s|%t|%d|%.3f|%.0f|%.0f|%.0f|%s|%d|%.2f|%.2f|%.2f",
		inv.Scheme, inv.Host, inv.Port, inv.Serial, p.opts.Simulator,
		stringsCount, kwp, inv.CapacityKWh, inv.MaxChargeW, inv.MaxDischargeW,
		values.Site.Timezone,
		inv.TelemetryIntervalS, inv.SignOverrides.PV, inv.SignOverrides.Grid, inv.SignOverrides.Battery)
}
