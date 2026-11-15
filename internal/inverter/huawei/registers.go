package huawei

import "encoding/binary"

// Register map of the Huawei SUN2000 with a LUNA2000 battery, taken from the
// wlcrs/huawei-solar-lib library. Register numbers are Modbus PDU addresses;
// there is no 40001-style offset. Multi-register values are big-endian with the
// high word first.
const (
	// Identity block, 30000 onwards.
	regModel       = 0x7530 // 15 words, ASCII
	regSerial      = 0x753F // 10 words
	regPartNumber  = 0x7549 // 10 words
	regFirmware    = 0x7553 // 15 words
	regSoftware    = 0x7562 // 15 words
	regPVStrings   = 0x7577 // 1 word, 1..24
	regMPPTrackers = 0x7578 // 1 word
	regRatedPower  = 0x7579 // 2 words, uint32 W

	// PV input.
	regPVString = 0x7D10 // voltage then current per string, 2 words each
	regInputPow = 0x7D40 // 2 words, int32 W
	regPVEnergy = 0x7D6A // 2 words, uint32 0.01 kWh (total AC yield)
	regPVToday  = 0x7D72 // 2 words, uint32 0.01 kWh

	// AC side.
	regGridVoltage = 0x7D42 // 1 word, 0.1 V
	regGridFreq    = 0x7D55 // 1 word, 0.01 Hz
	regActivePower = 0x7D50 // 2 words, int32 W, positive = export
	regDeviceState = 0x7D59 // 1 word, status code
	regFaultCode   = 0x7D5A // 1 word

	// Power meter (DTSU666).
	regMeterStatus = 0x90EC // 1 word, 0 offline, 1 normal
	regMeterPower  = 0x90F9 // 2 words, int32 W
	regMeterEnergy = 0x90FF // 2 words, int32 0.01 kWh (exported)
	regMeterImport = 0x9101 // 2 words, int32 0.01 kWh (consumption)

	// Battery aggregate.
	regBatteryCapacity = 0x937E // 2 words, uint32 Wh
	regBatterySOC      = 0x9380 // 1 word, 0.1 %
	regBatteryStatus   = 0x9382 // 1 word
	regBatteryPower    = 0x9385 // 2 words, int32 W, positive = charging
	regBatteryChargeT  = 0x9388 // 2 words, uint32 0.01 kWh today
	regBatteryDischT   = 0x938A // 2 words, uint32 0.01 kWh today

	// Writable battery configuration.
	regMaxChargePower = 0xB7E3 // 2 words, uint32 W
	regMaxDischPower  = 0xB7E5 // 2 words, uint32 W
	regEndOfCharge    = 0xB7E9 // 1 word, 0.1 %
	regEndOfDischarge = 0xB7EA // 1 word, 0.1 %
	regWorkingMode    = 0xB7EE // 1 word, enum
	regForcibleCmd    = 0xB7FC // 1 word, 0 stop, 1 charge, 2 discharge
	regForcibleSOC    = 0xB7FD // 1 word, 0.1 %
	regForcibleDur    = 0xB7EB // 1 word, minutes
	regForcibleMode   = 0xB88E // 1 word, 0 time, 1 SOC
	regForcibleCharge = 0xB88F // 2 words, uint32 W
	regForcibleDisch  = 0xB891 // 2 words, uint32 W
	regTimeslotStruct = 0xB897 // 43 words, LUNA2000 time-of-use periods
	regHeartbeat      = 0xC34F // 1 word, write 1 every 15 s
)

// Battery working modes, as written to 0xB7EE.
const (
	ModeAdaptive       = 0
	ModeFixedCharge    = 1
	ModeMaxSelfUse     = 2
	ModeTOULG          = 3
	ModeFullyFedToGrid = 4
	ModeTOULuna        = 5
	ModeThirdPartyCtrl = 6
)

// Forcible command values, written to 0xB7FC last.
const (
	ForcibleStop      = 0
	ForcibleCharge    = 1
	ForcibleDischarge = 2
)

// Invalid markers the inverter uses in place of a missing value.
const (
	invalidU16 = 0xFFFF
	invalidI16 = 0x7FFF
	invalidU32 = 0xFFFFFFFF
	invalidI32 = 0x7FFFFFFF
)

func u16(regs []uint16, offset int) uint16 {
	if offset < 0 || offset >= len(regs) {
		return 0
	}
	return regs[offset]
}

func i16(regs []uint16, offset int) int16 { return int16(u16(regs, offset)) }

// u32 joins two registers, high word first.
func u32(regs []uint16, offset int) uint32 {
	return uint32(u16(regs, offset))<<16 | uint32(u16(regs, offset+1))
}

func i32(regs []uint16, offset int) int32 { return int32(u32(regs, offset)) }

// u32Value treats the invalid marker as "no reading".
func u32Value(regs []uint16, offset int) (uint32, bool) {
	value := u32(regs, offset)
	if value == invalidU32 {
		return 0, false
	}
	return value, true
}

func i32Value(regs []uint16, offset int) (int32, bool) {
	value := i32(regs, offset)
	if value == invalidI32 {
		return 0, false
	}
	return value, true
}

func u16Value(regs []uint16, offset int) (uint16, bool) {
	value := u16(regs, offset)
	if value == invalidU16 {
		return 0, false
	}
	return value, true
}

func i16Value(regs []uint16, offset int) (int16, bool) {
	value := i16(regs, offset)
	if value == invalidI16 {
		return 0, false
	}
	return value, true
}

// ascii decodes packed big-endian ASCII, cutting at the first NUL.
func ascii(regs []uint16, offset, words int) string {
	out := make([]byte, 0, words*2)
	for i := 0; i < words; i++ {
		value := u16(regs, offset+i)
		out = append(out, byte(value>>8), byte(value))
	}
	for i, b := range out {
		if b == 0 {
			out = out[:i]
			break
		}
	}
	for len(out) > 0 && out[len(out)-1] == ' ' {
		out = out[:len(out)-1]
	}
	return string(out)
}

// encodeU32 splits a value into high and low words.
func encodeU32(value uint32) (uint16, uint16) {
	return uint16(value >> 16), uint16(value & 0xFFFF)
}

// periodWords packs a time-of-use period the way the LUNA2000 expects: start
// minute, end minute, then a word whose high byte is the direction and whose low
// byte is the weekday mask.
func periodWords(startMin, endMin int, discharge bool, days byte) [3]uint16 {
	direction := byte(0)
	if discharge {
		direction = 1
	}
	return [3]uint16{
		uint16(startMin),
		uint16(endMin),
		binary.BigEndian.Uint16([]byte{direction, days}),
	}
}
