package ev

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// SimulatorConfig configures the deterministic EVSE.
type SimulatorConfig struct {
	ID         string
	Phases     int
	VoltageV   float64
	MinCurrent float64
	MaxCurrent float64
	Connected  bool
	Now        func() time.Time
}

// Simulator is a deterministic charger used in demos and tests.
type Simulator struct {
	cfg SimulatorConfig

	mu         sync.Mutex
	connected  bool
	paused     bool
	currentA   float64
	sessionKWh float64
	last       time.Time
}

// NewSimulator builds a simulator.
func NewSimulator(cfg SimulatorConfig) *Simulator {
	if cfg.ID == "" {
		cfg.ID = "sim-ev-1"
	}
	if cfg.Phases == 0 {
		cfg.Phases = 3
	}
	if cfg.VoltageV == 0 {
		cfg.VoltageV = 230
	}
	if cfg.MinCurrent == 0 {
		cfg.MinCurrent = 6
	}
	if cfg.MaxCurrent == 0 {
		cfg.MaxCurrent = 16
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Simulator{cfg: cfg, connected: cfg.Connected, currentA: cfg.MinCurrent, last: cfg.Now()}
}

// ID implements Charger.
func (s *Simulator) ID() string { return s.cfg.ID }

// Type implements Charger.
func (s *Simulator) Type() string { return "simulator" }

// Capabilities implements Charger.
func (s *Simulator) Capabilities() Capabilities {
	return Capabilities{
		MinCurrentA: s.cfg.MinCurrent, MaxCurrentA: s.cfg.MaxCurrent,
		Phases: s.cfg.Phases, CurrentScope: "charger", SupportsPause: true,
		ControlLatency: 0,
	}
}

// Status implements Charger.
func (s *Simulator) Status(context.Context) (domain.ChargerStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.cfg.Now()
	power := 0.0
	if s.connected && !s.paused {
		power = s.currentA * float64(s.cfg.Phases) * s.cfg.VoltageV
	}
	if power > 0 {
		elapsed := now.Sub(s.last).Hours()
		if elapsed > 0 {
			s.sessionKWh += power * elapsed / 1000
		}
	}
	s.last = now
	return domain.ChargerStatus{
		ID: s.cfg.ID, Type: "simulator", Online: true, Connected: s.connected,
		Charging: power > 0, PowerW: round1(power), CurrentA: round1(s.currentA),
		Phases: s.cfg.Phases, SessionKWh: round2(s.sessionKWh), Paused: s.paused,
		UpdatedAt: now.UTC(),
	}, nil
}

// SetCurrent implements Charger.
func (s *Simulator) SetCurrent(_ context.Context, amps float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentA = math.Max(s.cfg.MinCurrent, math.Min(s.cfg.MaxCurrent, amps))
	return nil
}

// Pause implements Charger.
func (s *Simulator) Pause(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = true
	return nil
}

// Resume implements Charger.
func (s *Simulator) Resume(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = false
	return nil
}

// StopSession implements SessionStopper.
func (s *Simulator) StopSession(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = true
	s.currentA = 0
	return nil
}

// Close implements Charger.
func (s *Simulator) Close() error { return nil }

// Plug changes the connected state (tests/demos).
func (s *Simulator) Plug(connected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = connected
	if !connected {
		s.paused = false
		s.currentA = s.cfg.MinCurrent
	}
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
