package saj

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/inverter"
)

// ErrScheduleUnsupported is returned by WriteSchedule: the H1 has no weekly
// slot table to write. It is controlled by an app mode plus an instantaneous
// passive charge or discharge power.
var ErrScheduleUnsupported = errors.New(
	"saj: the H1 has no slot schedule; drive it with app mode and passive power")

// Config configures the inverter connection.
type Config struct {
	// Address is the Modbus TCP endpoint, for example 192.168.0.40:502.
	Address string
	// UnitID is the Modbus station address; the H1 answers on 1.
	UnitID byte
	// Timeout bounds one request.
	Timeout time.Duration
	// Now is injectable for tests.
	Now func() time.Time
}

// registers is the slice of the transport the client needs, so tests can stand
// in a fake device.
type registers interface {
	readHolding(ctx context.Context, addr, count uint16) ([]uint16, error)
	writeRegisters(ctx context.Context, addr uint16, values []uint16) error
	close() error
}

// Client is a SAJ H1 inverter.
type Client struct {
	regs       registers
	now        func() time.Time
	cachedID   *inverter.Identity
	cachedAt   time.Time
	timeout    time.Duration
	retries    int
	retryDelay time.Duration
}

// New builds a client for the configured inverter.
func New(cfg Config) *Client {
	unit := cfg.UnitID
	if unit == 0 {
		unit = 1
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		regs:       newTransport(cfg.Address, unit, cfg.Timeout),
		now:        now,
		timeout:    cfg.Timeout,
		retries:    3,
		retryDelay: 50 * time.Millisecond,
	}
}

func newWithRegisters(regs registers, now func() time.Time) *Client {
	if now == nil {
		now = time.Now
	}
	return &Client{regs: regs, now: now, retries: 1}
}

// read fetches one block with the integration's retry policy: three attempts
// spaced 50 ms apart before the read is reported as failed.
func (c *Client) read(ctx context.Context, addr, count uint16) ([]uint16, error) {
	var lastErr error
	attempts := c.retries
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.retryDelay):
			}
		}
		regs, err := c.regs.readHolding(ctx, addr, count)
		if err == nil {
			return regs, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("saj: read 0x%04x: %w", addr, lastErr)
}

// Read polls the inverter and normalises it into the shared reading shape:
// grid positive when importing and battery positive when charging, which is the
// opposite of the inverter's own convention.
func (c *Client) Read(ctx context.Context) (inverter.Reading, error) {
	// One block per group of related registers; each stays under the
	// 100-register chunk limit.
	system, err := c.read(ctx, regClock, 5)
	if err != nil {
		return inverter.Reading{}, err
	}
	power, err := c.read(ctx, regGridVoltage, 0x46)
	if err != nil {
		return inverter.Reading{}, err
	}
	summary, err := c.read(ctx, regSystemLoad, 0x0E)
	if err != nil {
		return inverter.Reading{}, err
	}
	energy, err := c.read(ctx, regPVEnergyToday, 0x3A)
	if err != nil {
		return inverter.Reading{}, err
	}

	// Address helpers, each relative to the block that holds the register.
	at := func(regs []uint16, base, addr uint16) uint16 {
		offset := int(addr - base)
		if offset < 0 || offset >= len(regs) {
			return 0
		}
		return regs[offset]
	}
	signed := func(regs []uint16, base, addr uint16) float64 {
		return float64(int16(at(regs, base, addr)))
	}
	kwh := func(addr uint16) float64 {
		return float64(u32(energy, addr-regPVEnergyToday)) * 0.01
	}

	pvW := float64(at(summary, regSystemLoad, regSummaryPV))
	// 0x40AD is inverted at source: negate it so positive means import.
	gridW := -signed(summary, regSystemLoad, regMeter2Power)
	if gridW == 0 {
		gridW = -signed(summary, regSystemLoad, regSummaryGrid)
	}
	// The inverter reports battery power positive while discharging.
	batteryW := -signed(summary, regSystemLoad, regSummaryBattery)
	if batteryW == 0 {
		batteryW = -signed(power, regGridVoltage, regBatteryPower)
	}
	loadW := float64(at(summary, regSystemLoad, regSystemLoad))
	if loadW == 0 {
		// Energy balance fallback when the meter register is absent.
		loadW = pvW + gridW - batteryW
	}

	socRaw := float64(at(power, regGridVoltage, regBatterySOC))
	soc := socRaw * 0.01
	if socRaw <= 100 {
		// Some firmware reports whole percent in this register.
		soc = socRaw
	}

	return inverter.Reading{
		At:  c.now().UTC(),
		PVW: pvW,
		PVStrings: []float64{
			float64(at(power, regGridVoltage, regPV1Power)),
			float64(at(power, regGridVoltage, regPV2Power)),
		},
		GridW:    gridW,
		BatteryW: batteryW,
		LoadW:    loadW,
		SOC:      soc,
		SOH:      100,
		Mode:     int(at(system, regClock, regWorkingMode)),
		Extra: map[string]any{
			"grid_voltage_v":        float64(at(power, regGridVoltage, regGridVoltage)) * 0.1,
			"grid_frequency_hz":     float64(at(power, regGridVoltage, regGridFrequency)) * 0.01,
			"battery_voltage_v":     float64(at(power, regGridVoltage, regBatteryVoltage)) * 0.1,
			"inverter_power_w":      signed(power, regGridVoltage, regInverterPower),
			"pv_today_kwh":          kwh(regPVEnergyToday),
			"battery_charge_kwh":    kwh(regBatteryChargeToday),
			"battery_discharge_kwh": kwh(regBatteryDischargeToday),
			"grid_import_kwh":       kwh(regGridImportToday),
			"grid_export_kwh":       kwh(regGridExportToday),
		},
	}, nil
}

// Identity reads the static block once and caches it.
func (c *Client) Identity(ctx context.Context) (inverter.Identity, error) {
	if c.cachedID != nil && c.now().Sub(c.cachedAt) < 10*time.Minute {
		return *c.cachedID, nil
	}
	regs, err := c.read(ctx, regInverterType, 0x1E)
	if err != nil {
		return inverter.Identity{}, err
	}
	identity := inverter.Identity{
		Serial:   strings.TrimSpace(ascii(regs, regSerial-regInverterType, 10)),
		Model:    "SAJ H1",
		Firmware: fmt.Sprintf("%.3f", float64(u16(regs, regFirmware-regInverterType))*0.001),
		Phases:   3,
	}
	if reading, err := c.Read(ctx); err == nil {
		identity.WorkMode = reading.Mode
	}
	c.cachedID = &identity
	c.cachedAt = c.now()
	return identity, nil
}

// Schedule reports that the inverter has no slot schedule to read.
func (c *Client) Schedule(ctx context.Context) (domain.Schedule, error) {
	return domain.Schedule{Days: map[string][]domain.ScheduleSlot{}}, nil
}

// WriteSchedule is not supported: see ErrScheduleUnsupported.
func (c *Client) WriteSchedule(ctx context.Context, schedule domain.Schedule) (domain.Schedule, error) {
	return domain.Schedule{}, ErrScheduleUnsupported
}

// SetWorkMode selects the app mode: 0 self use, 1 time of use, 2 backup,
// 3 passive.
func (c *Client) SetWorkMode(ctx context.Context, mode int) error {
	if mode < AppSelfUse || mode > AppPassive {
		return fmt.Errorf("saj: unsupported app mode %d", mode)
	}
	return c.regs.writeRegisters(ctx, regAppMode, []uint16{uint16(mode)})
}

// SetChargeMax sets the upper SOC limit in percent.
func (c *Client) SetChargeMax(ctx context.Context, percent float64) error {
	if percent < 50 || percent > 100 {
		return fmt.Errorf("saj: SOC high limit %v out of range", percent)
	}
	return c.regs.writeRegisters(ctx, regSOCHigh, []uint16{uint16(percent)})
}

// BatteryConfig returns the app mode and the upper SOC limit.
func (c *Client) BatteryConfig(ctx context.Context) (int, float64, error) {
	regs, err := c.read(ctx, regAppMode, 1)
	if err != nil {
		return 0, 0, err
	}
	soc, err := c.read(ctx, regSOCHigh, 1)
	if err != nil {
		return 0, 0, err
	}
	return int(regs[0]), float64(soc[0]), nil
}

// SetPassive drives the inverter in passive mode: charge or discharge at the
// given power. It selects the passive app mode as well, so a single call puts
// the battery where the planner wants it.
func (c *Client) SetPassive(ctx context.Context, mode int, watts float64) error {
	if mode != PassiveStandby && mode != PassiveDischarge && mode != PassiveCharge {
		return fmt.Errorf("saj: unsupported passive mode %d", mode)
	}
	if watts < 0 {
		return fmt.Errorf("saj: passive power must not be negative")
	}
	if watts > 5000 {
		watts = 5000
	}
	if err := c.regs.writeRegisters(ctx, regAppMode, []uint16{AppPassive}); err != nil {
		return err
	}
	reg := uint16(regPassiveCharge)
	if mode == PassiveDischarge {
		reg = regPassiveDischarge
	}
	if err := c.regs.writeRegisters(ctx, reg, []uint16{uint16(watts)}); err != nil {
		return err
	}
	return c.regs.writeRegisters(ctx, regPassiveMode, []uint16{uint16(mode)})
}

// SyncClock writes the inverter clock from the given time.
func (c *Client) SyncClock(ctx context.Context, at time.Time) error {
	words := clockWords(uint16(at.Year()), uint8(at.Month()), uint8(at.Day()),
		uint8(at.Hour()), uint8(at.Minute()), uint8(at.Second()))
	return c.regs.writeRegisters(ctx, 0x8020, words[:])
}

// Close releases the connection.
func (c *Client) Close() error { return c.regs.close() }
