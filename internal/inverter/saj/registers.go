package saj

import "encoding/binary"

// Register map of the SAJ H1 series over Modbus TCP, taken from the Home
// Assistant integration h3llrais3r/ha-saj-h1-mqtt. Addresses are absolute
// holding registers; write only the ones marked writable.
const (
	// System block, read from 0x4000.
	regClock          = 0x4000 // 4 registers, year then packed month/day/hour/minute/second
	regWorkingMode    = 0x4004 // 2 = on grid, 3 = off grid, 4 = on grid load, 5 = fault
	regGridVoltage    = 0x4031 // x0.1 V
	regGridCurrent    = 0x4032 // x0.01 A, signed
	regGridFrequency  = 0x4033 // x0.01 Hz
	regGridPowerPort  = 0x4035 // signed W at the grid port
	regInverterPower  = 0x4049 // signed W
	regBatteryVoltage = 0x4069 // x0.1 V
	regBatteryCurrent = 0x406A // x0.01 A, signed
	regBatteryPower   = 0x406D // signed W, positive = discharging
	regBatterySOC     = 0x406F // x0.01 %, some firmware reports plain percent
	regPV1Voltage     = 0x4071 // x0.1 V
	regPV1Current     = 0x4072 // x0.01 A, unsigned
	regPV1Power       = 0x4073 // W
	regPV2Voltage     = 0x4074
	regPV2Current     = 0x4075
	regPV2Power       = 0x4076
	regSystemLoad     = 0x40A0 // W, unsigned
	regMeter1Power    = 0x40A1 // signed W
	regSummaryPV      = 0x40A5 // W, unsigned
	regSummaryBattery = 0x40A6 // signed W, positive = discharging
	regSummaryGrid    = 0x40A7 // signed W, positive = exporting
	regSummaryInv     = 0x40A9 // signed W
	regBackupLoad     = 0x40AB // signed W
	regMeter2Power    = 0x40AD // signed W, inverted: negate to get import positive

	// Daily energy counters, each a uint32 of two registers scaled by 0.01 kWh.
	regPVEnergyToday         = 0x40BF
	regBatteryChargeToday    = 0x40C7
	regBatteryDischargeToday = 0x40CF
	regGridExportToday       = 0x40EF
	regGridImportToday       = 0x40F7

	// Writable configuration.
	regAppMode          = 0x3247 // 0 self use, 1 time of use, 2 backup, 3 passive
	regGridChargeLimit  = 0x3248 // W
	regGridFeedLimit    = 0x3249 // W
	regSOCBackup        = 0x3271 // %
	regSOCHigh          = 0x3273 // %
	regSOCLow           = 0x3274 // %
	regPassiveMode      = 0x3636 // 0 standby, 1 discharge, 2 charge
	regPassiveCharge    = 0x3637 // W
	regPassiveDischarge = 0x3638 // W

	// Identity block.
	regInverterType = 0x8F00
	regSerial       = 0x8F03 // 10 registers, ASCII
	regFirmware     = 0x8F18 // master control software, x0.001
)

// App modes accepted by register 0x3247.
const (
	AppSelfUse   = 0
	AppTimeOfUse = 1
	AppBackup    = 2
	AppPassive   = 3
)

// Passive modes accepted by register 0x3636. Note that the inverter swaps the
// documented order: 1 discharges and 2 charges.
const (
	PassiveStandby   = 0
	PassiveDischarge = 1
	PassiveCharge    = 2
)

// readBlocks are the ranges one poll reads. Each is well under the 100-register
// chunk limit, so a poll costs five round trips.
var pollBlocks = []struct {
	addr  uint16
	count uint16
}{
	{regClock, 5},            // 0x4000-0x4004
	{regGridVoltage, 0x46},   // 0x4031-0x4076: grid, battery, PV strings
	{regSystemLoad, 0x0E},    // 0x40A0-0x40AD: summary powers
	{regPVEnergyToday, 0x3A}, // 0x40BF-0x40F8: daily energy counters
}

func u16(regs []uint16, addr uint16) uint16 {
	if int(addr) < 0 || int(addr) >= len(regs) {
		return 0
	}
	return regs[addr]
}

func i16(regs []uint16, addr uint16) int16 { return int16(u16(regs, addr)) }

// u32 joins two registers, high word first.
func u32(regs []uint16, addr uint16) uint32 {
	return uint32(u16(regs, addr))<<16 | uint32(u16(regs, addr+1))
}

// ascii decodes a run of registers holding packed ASCII, trimming trailing NULs.
func ascii(regs []uint16, addr, words uint16) string {
	out := make([]byte, 0, words*2)
	for i := uint16(0); i < words; i++ {
		value := u16(regs, addr+i)
		out = append(out, byte(value>>8), byte(value))
	}
	for len(out) > 0 && (out[len(out)-1] == 0 || out[len(out)-1] == ' ') {
		out = out[:len(out)-1]
	}
	return string(out)
}

// clock decodes the four clock registers into a time value.
func clockWords(year uint16, month, day, hour, minute, second byte) [4]uint16 {
	return [4]uint16{
		year,
		binary.BigEndian.Uint16([]byte{month, day}),
		binary.BigEndian.Uint16([]byte{hour, minute}),
		binary.BigEndian.Uint16([]byte{second, 0}),
	}
}
