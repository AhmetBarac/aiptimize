// Package huawei reads and controls a Huawei SUN2000 inverter with a LUNA2000
// battery over Modbus TCP through the SDongle.
//
// Register numbers are Modbus PDU addresses with no 40001 offset, values are
// big-endian with the high word first, and the battery and power meter are
// served by the inverter at the same unit id. The map is taken from the
// wlcrs/huawei-solar-lib library; see docs/huawei.md for the full table.
package huawei

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/inverter"
	"github.com/AhmetBarac/aiptimize/internal/modbus"
)

// ErrScheduleUnsupported is returned by WriteSchedule: the SUN2000 has no plain
// slot table. It schedules through time-of-use periods or a forcible command,
// and Aiptimize drives the latter.
var ErrScheduleUnsupported = errors.New(
	"huawei: the SUN2000 has no slot schedule; drive it with a forcible charge or discharge")

// Config configures the inverter connection.
type Config struct {
	// Address is the Modbus TCP endpoint, for example 192.168.0.50:502.
	Address string
	// FallbackAddress is tried when Address cannot be reached. Huawei dongles
	// listen on 502 or 6607 depending on firmware and plant configuration, so
	// the adapter switches over once and remembers which one answered.
	FallbackAddress string
	// UnitID is the station address; 0 is the default, 100 the SDongle fast path.
	UnitID byte
	// Timeout bounds one request.
	Timeout time.Duration
	// Signs calibrates the meter polarity, which depends on the CT installation.
	Signs inverter.Signs
	// Now is injectable for tests.
	Now func() time.Time
}

// registers is the slice of the transport the client needs.
type registers interface {
	ReadHolding(ctx context.Context, addr, count uint16) ([]uint16, error)
	WriteRegisters(ctx context.Context, addr uint16, values []uint16) error
	Close() error
}

// Client is a Huawei SUN2000.
type Client struct {
	regs     registers
	now      func() time.Time
	signs    inverter.Signs
	cfg      Config
	address  string
	fallback string
	strings  int
	cachedID *inverter.Identity
	cachedAt time.Time
}

// New builds a client for the configured inverter.
func New(cfg Config) *Client {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	signs := cfg.Signs
	if signs.PV == 0 {
		signs.PV = 1
	}
	if signs.Grid == 0 {
		signs.Grid = 1
	}
	if signs.Battery == 0 {
		signs.Battery = 1
	}
	return &Client{
		regs:     modbus.New(modbus.Config{Address: cfg.Address, UnitID: cfg.UnitID, Timeout: cfg.Timeout}),
		now:      now,
		signs:    signs,
		cfg:      cfg,
		address:  cfg.Address,
		fallback: cfg.FallbackAddress,
	}
}

func newWithRegisters(regs registers, now func() time.Time) *Client {
	if now == nil {
		now = time.Now
	}
	return &Client{regs: regs, now: now, signs: inverter.Signs{PV: 1, Grid: 1, Battery: 1}}
}

// readSpan fetches registers and reindexes them from base.
func (c *Client) readSpan(ctx context.Context, base, count uint16) ([]uint16, error) {
	regs, err := c.regs.ReadHolding(ctx, base, count)
	if err != nil && c.switchPort(err) {
		if retry, retryErr := c.regs.ReadHolding(ctx, base, count); retryErr == nil {
			return retry, nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("huawei: read 0x%04x: %w", base, err)
	}
	return regs, nil
}

// switchPort moves the connection to the other known Modbus port after an
// unreachable error. It reports whether a retry makes sense.
func (c *Client) switchPort(err error) bool {
	if c.fallback == "" || !errors.Is(err, modbus.ErrUnreachable) {
		return false
	}
	_ = c.regs.Close()
	c.regs = modbus.New(modbus.Config{Address: c.fallback, UnitID: c.cfg.UnitID, Timeout: c.cfg.Timeout})
	c.address, c.fallback = c.fallback, c.address
	return true
}

// stringCount reads how many PV strings the model reports, cached afterwards.
func (c *Client) stringCount(ctx context.Context) int {
	if c.strings > 0 {
		return c.strings
	}
	regs, err := c.readSpan(ctx, regPVStrings, 1)
	if err != nil || len(regs) == 0 {
		return 2
	}
	count := int(regs[0])
	if count < 1 || count > 24 {
		count = 2
	}
	c.strings = count
	return count
}

// Identity reads the static block once and caches it.
func (c *Client) Identity(ctx context.Context) (inverter.Identity, error) {
	if c.cachedID != nil && c.now().Sub(c.cachedAt) < 10*time.Minute {
		return *c.cachedID, nil
	}
	// 0x7530..0x7562 covers model, serial, part number, firmware and software.
	regs, err := c.readSpan(ctx, regModel, 0x33)
	if err != nil {
		return inverter.Identity{}, err
	}
	// The ratings sit further along; 0x7577..0x7583 holds the string count,
	// the tracker count and the rated power.
	rating, err := c.readSpan(ctx, regPVStrings, 0x0D)
	if err != nil {
		return inverter.Identity{}, err
	}
	model := strings.TrimSpace(ascii(regs, 0, 15))
	identity := inverter.Identity{
		Serial:   strings.TrimSpace(ascii(regs, regSerial-regModel, 10)),
		Model:    "Huawei SUN2000 " + model,
		Firmware: strings.TrimSpace(ascii(regs, regFirmware-regModel, 15)),
		Phases:   3,
	}
	if rated, ok := u32Value(rating, regRatedPower-regPVStrings); ok && rated < 100000 {
		identity.RatedPowerW = float64(rated)
	}
	if reading, err := c.Read(ctx); err == nil {
		identity.WorkMode = reading.Mode
		identity.CapacityKWh = reading.Extra["capacity_kwh"].(float64)
	}
	c.cachedID = &identity
	c.cachedAt = c.now()
	return identity, nil
}

// Read polls the inverter. Grid is positive when importing and battery power is
// positive when charging, which the inverter already reports that way.
func (c *Client) Read(ctx context.Context) (inverter.Reading, error) {
	// Keep the SDongle's watchdog happy; a failure here is not fatal.
	_ = c.regs.WriteRegisters(ctx, regHeartbeat, []uint16{1})

	strings := c.stringCount(ctx)
	if strings > 12 {
		strings = 12
	}

	// One span per group, each inside the 64-register request limit.
	pvRegs, err := c.readSpan(ctx, regPVString, uint16(strings*2))
	if err != nil {
		return inverter.Reading{}, err
	}
	dcRegs, err := c.readSpan(ctx, regInputPow, 0x14) // 0x7D40..0x7D53
	if err != nil {
		return inverter.Reading{}, err
	}
	acRegs, err := c.readSpan(ctx, regActivePower, 0x0B) // 0x7D50..0x7D5A
	if err != nil {
		return inverter.Reading{}, err
	}
	yieldRegs, err := c.readSpan(ctx, regPVEnergy, 0x0A) // 0x7D6A..0x7D73
	if err != nil {
		return inverter.Reading{}, err
	}
	batteryRegs, err := c.readSpan(ctx, regBatteryCapacity, 0x0E) // 0x937E..0x938B
	if err != nil {
		return inverter.Reading{}, err
	}

	pvStrings := make([]float64, 0, strings)
	pvTotal := 0.0
	for i := 0; i < strings; i++ {
		volts, _ := i16Value(pvRegs, i*2)
		amps, _ := i16Value(pvRegs, i*2+1)
		power := float64(volts) * 0.1 * float64(amps) * 0.01
		pvStrings = append(pvStrings, power)
		pvTotal += power
	}

	inputPower, _ := i32Value(dcRegs, regInputPow-regInputPow)
	if inputPower > 0 {
		pvTotal = float64(inputPower) * c.signs.PV
	}

	batteryW := 0.0
	if power, ok := i32Value(batteryRegs, regBatteryPower-regBatteryCapacity); ok {
		batteryW = float64(power) * c.signs.Battery
	}
	soc := 0.0
	if raw, ok := u16Value(batteryRegs, regBatterySOC-regBatteryCapacity); ok {
		soc = float64(raw) * 0.1
	}
	capacityKWh := 0.0
	if capacity, ok := u32Value(batteryRegs, 0); ok && capacity > 0 {
		capacityKWh = float64(capacity) / 1000
	}
	chargeToday := 0.0
	if raw, ok := u32Value(batteryRegs, regBatteryChargeT-regBatteryCapacity); ok {
		chargeToday = float64(raw) * 0.01
	}
	dischargeToday := 0.0
	if raw, ok := u32Value(batteryRegs, regBatteryDischT-regBatteryCapacity); ok {
		dischargeToday = float64(raw) * 0.01
	}

	// The meter is authoritative for the grid when it is online; otherwise fall
	// back to the inverter's AC output, which is positive while exporting.
	gridW := 0.0
	meterPower, meterOK := float64(0), false
	if meterRegs, err := c.readSpan(ctx, regMeterStatus, 0x17); err == nil {
		if status, ok := u16Value(meterRegs, 0); ok && status == 1 {
			if power, ok := i32Value(meterRegs, regMeterPower-regMeterStatus); ok {
				meterPower = float64(power) * c.signs.Grid
				meterOK = true
			}
		}
	}
	if meterOK {
		gridW = meterPower
	} else if active, ok := i32Value(acRegs, regActivePower-regActivePower); ok {
		gridW = -float64(active)
	}

	status, _ := u16Value(acRegs, regDeviceState-regActivePower)
	fault, _ := u16Value(acRegs, regFaultCode-regActivePower)
	freq, _ := u16Value(acRegs, regGridFreq-regActivePower)
	gridVolts, _ := u16Value(acRegs, regGridVoltage-regActivePower)

	// No load register exists on the SUN2000; derive it from the balance.
	loadW := pvTotal + gridW - batteryW
	if loadW < 0 {
		loadW = 0
	}

	pvToday := 0.0
	if raw, ok := u32Value(yieldRegs, regPVToday-regPVEnergy); ok {
		pvToday = float64(raw) * 0.01
	}

	return inverter.Reading{
		At:        c.now().UTC(),
		PVW:       pvTotal,
		PVStrings: pvStrings,
		GridW:     gridW,
		BatteryW:  batteryW,
		LoadW:     loadW,
		SOC:       soc,
		SOH:       100,
		Mode:      deviceMode(status),
		Extra: map[string]any{
			"device_state":          float64(status),
			"fault_code":            float64(fault),
			"grid_voltage_v":        float64(gridVolts) * 0.1,
			"grid_frequency_hz":     float64(freq) * 0.01,
			"capacity_kwh":          capacityKWh,
			"pv_today_kwh":          pvToday,
			"battery_charge_kwh":    chargeToday,
			"battery_discharge_kwh": dischargeToday,
			"meter_power_w":         meterPower,
			"meter_online":          meterOK,
		},
	}, nil
}

// deviceMode maps the Huawei status code onto the coarse mode the app stores.
func deviceMode(state uint16) int {
	switch {
	case state == 0x0200 || state == 0x0201 || state == 0x0202:
		return 2 // on grid
	case state == 0x0203 || state == 0x0A00:
		return 3 // off grid
	case state == 0x0300 || state == 0x0301 || state == 0x0302:
		return 5 // shutdown / fault
	default:
		return 1 // standby or transitional
	}
}

// Schedule reports that the inverter has no slot schedule to read.
func (c *Client) Schedule(ctx context.Context) (domain.Schedule, error) {
	return domain.Schedule{Days: map[string][]domain.ScheduleSlot{}}, nil
}

// WriteSchedule is not supported: the plan is applied as forcible commands.
func (c *Client) WriteSchedule(ctx context.Context, schedule domain.Schedule) (domain.Schedule, error) {
	return domain.Schedule{}, ErrScheduleUnsupported
}

// SetWorkMode writes the battery working mode (0 adaptive, 1 fixed charge, 2
// self-consumption, 3 TOU LG, 4 fully fed to grid, 5 TOU LUNA, 6 third party).
func (c *Client) SetWorkMode(ctx context.Context, mode int) error {
	if mode < ModeAdaptive || mode > ModeThirdPartyCtrl {
		return fmt.Errorf("huawei: unsupported working mode %d", mode)
	}
	return c.regs.WriteRegisters(ctx, regWorkingMode, []uint16{uint16(mode)})
}

// SetChargeMax sets the end-of-charge SOC in percent.
func (c *Client) SetChargeMax(ctx context.Context, percent float64) error {
	if percent < 90 || percent > 100 {
		return fmt.Errorf("huawei: end-of-charge SOC %v out of range", percent)
	}
	return c.regs.WriteRegisters(ctx, regEndOfCharge, []uint16{uint16(percent * 10)})
}

// BatteryConfig returns the working mode and the end-of-charge SOC.
func (c *Client) BatteryConfig(ctx context.Context) (int, float64, error) {
	regs, err := c.readSpan(ctx, regEndOfCharge, 6) // 0xB7E9..0xB7EE
	if err != nil {
		return 0, 0, err
	}
	soc := float64(u16(regs, regEndOfCharge-regEndOfCharge)) * 0.1
	mode := int(u16(regs, regWorkingMode-regEndOfCharge))
	return mode, soc, nil
}

// setPassive drives the battery with a forcible charge or discharge. The energy
// management registers require the mode and duration to be set before the
// command, so the order below matters: a half-applied sequence does nothing.
func (c *Client) setPassive(ctx context.Context, mode int, watts float64, minutes int) error {
	switch mode {
	case 0: // standby: stop any forcible action and clear the setpoints
		if err := c.regs.WriteRegisters(ctx, regForcibleCmd, []uint16{ForcibleStop}); err != nil {
			return err
		}
		if err := c.regs.WriteRegisters(ctx, regForcibleDisch, []uint16{0, 0}); err != nil {
			return err
		}
		if err := c.regs.WriteRegisters(ctx, regForcibleCharge, []uint16{0, 0}); err != nil {
			return err
		}
		return c.regs.WriteRegisters(ctx, regForcibleMode, []uint16{0})
	case 1, 2: // discharge, charge
	default:
		return fmt.Errorf("huawei: unsupported passive mode %d", mode)
	}

	if watts < 0 {
		watts = 0
	}
	if watts > 10000 {
		watts = 10000
	}
	high, low := encodeU32(uint32(watts + 0.5))

	powerReg := uint16(regForcibleCharge)
	command := uint16(ForcibleCharge)
	if mode == 1 {
		powerReg = regForcibleDisch
		command = ForcibleDischarge
	}
	// Duration-based forcible command, one hour at a time; the controller
	// refreshes it while the slot lasts.
	if err := c.regs.WriteRegisters(ctx, powerReg, []uint16{high, low}); err != nil {
		return err
	}
	if err := c.regs.WriteRegisters(ctx, regForcibleDur, []uint16{uint16(minutes)}); err != nil {
		return err
	}
	if err := c.regs.WriteRegisters(ctx, regForcibleMode, []uint16{0}); err != nil {
		return err
	}
	return c.regs.WriteRegisters(ctx, regForcibleCmd, []uint16{command})
}

// SetPassiveFor runs a forcible charge or discharge for the given window. The
// inverter takes whole minutes, so the duration is rounded up and clamped to the
// supported 1..1440 range.
func (c *Client) SetPassiveFor(ctx context.Context, mode int, watts float64, window time.Duration) error {
	minutes := int((window + time.Minute - 1) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	if minutes > 1440 {
		minutes = 1440
	}
	return c.setPassive(ctx, mode, watts, minutes)
}

// SetPassive runs a forcible charge or discharge with the default one-hour
// window.
func (c *Client) SetPassive(ctx context.Context, mode int, watts float64) error {
	return c.setPassive(ctx, mode, watts, 60)
}

// Close releases the connection.
func (c *Client) Close() error { return c.regs.Close() }
