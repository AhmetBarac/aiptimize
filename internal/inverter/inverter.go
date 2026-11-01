// Package inverter defines the read-only contract shared by the Solplanet
// dongle client and the simulator. Phase 2 never writes to hardware.
package inverter

import (
	"context"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// Signs calibrates vendor sign conventions. Defaults are 1; commissioning may
// flip a value to -1.
type Signs struct {
	PV      float64
	Grid    float64
	Battery float64
}

// Identity is the static description of the inverter and battery.
type Identity struct {
	Serial      string  `json:"serial"`
	Model       string  `json:"model"`
	Firmware    string  `json:"firmware"`
	RatedPowerW float64 `json:"rated_power_w"`
	Phases      int     `json:"phases"`
	SafetyCode  int     `json:"safety_code"`
	CapacityKWh float64 `json:"capacity_kwh"`
	WorkMode    int     `json:"work_mode"`
	MinSOC      float64 `json:"min_soc"`
	MaxSOC      float64 `json:"max_soc"`
}

// Reading is one normalized telemetry sample. Powers are in watts:
// PV is generation, grid is positive import, battery is positive charging.
// Load is derived from the energy balance.
type Reading struct {
	At        time.Time      `json:"at"`
	PVW       float64        `json:"pv_w"`
	PVStrings []float64      `json:"pv_strings"`
	GridW     float64        `json:"grid_w"`
	BatteryW  float64        `json:"battery_w"`
	LoadW     float64        `json:"load_w"`
	SOC       float64        `json:"soc"`
	SOH       float64        `json:"soh"`
	Mode      int            `json:"mode"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// ScheduleWriter is the optional write contract used only after the arm gate.
type ScheduleWriter interface {
	WriteSchedule(ctx context.Context, schedule domain.Schedule) (domain.Schedule, error)
}

// WorkModeWriter selects a battery work mode after the arm gate.
type WorkModeWriter interface {
	SetWorkMode(ctx context.Context, mode int) error
}

// ChargeMaxWriter updates the battery charge ceiling after the arm gate.
type ChargeMaxWriter interface {
	SetChargeMax(ctx context.Context, percent float64) error
}

// Reader is the read-only inverter contract.
type Reader interface {
	Identity(ctx context.Context) (Identity, error)
	Read(ctx context.Context) (Reading, error)
	Schedule(ctx context.Context) (domain.Schedule, error)
	Close() error
}
