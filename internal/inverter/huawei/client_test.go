package huawei

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// fakeRegisters serves reads from a map so decoding can be checked without a
// device.
type fakeRegisters struct {
	regs   map[uint16]uint16
	writes []struct {
		addr   uint16
		values []uint16
	}
}

func (f *fakeRegisters) ReadHolding(ctx context.Context, addr, count uint16) ([]uint16, error) {
	out := make([]uint16, count)
	for i := uint16(0); i < count; i++ {
		out[i] = f.regs[addr+i]
	}
	return out, nil
}

func (f *fakeRegisters) WriteRegisters(ctx context.Context, addr uint16, values []uint16) error {
	f.writes = append(f.writes, struct {
		addr   uint16
		values []uint16
	}{addr, append([]uint16(nil), values...)})
	return nil
}

func (f *fakeRegisters) Close() error { return nil }

func setU32(regs map[uint16]uint16, addr uint16, value uint32) {
	regs[addr] = uint16(value >> 16)
	regs[addr+1] = uint16(value & 0xFFFF)
}

func setI32(regs map[uint16]uint16, addr uint16, value int32) {
	setU32(regs, addr, uint32(value))
}

func packASCII(regs map[uint16]uint16, addr uint16, text string) {
	for i := 0; i < len(text); i += 2 {
		var word uint16 = uint16(text[i]) << 8
		if i+1 < len(text) {
			word |= uint16(text[i+1])
		}
		regs[addr+uint16(i/2)] = word
	}
}

func TestReadDecodesAndDerivesLoad(t *testing.T) {
	regs := map[uint16]uint16{}
	// Two PV strings: 300 V / 5 A and 280 V / 4 A.
	regs[regPVString] = uint16(3000)
	regs[regPVString+1] = 500
	regs[regPVString+2] = 2800
	regs[regPVString+3] = 400
	setI32(regs, regInputPow, 4000)
	setI32(regs, regActivePower, 1500) // exporting 1500 W
	regs[regDeviceState] = 0x0200
	regs[regPVStrings] = 2
	setU32(regs, regPVToday, 1234) // 12.34 kWh
	// Battery: 5120 Wh, SOC 95.5 %, charging 800 W.
	setU32(regs, regBatteryCapacity, 5120)
	regs[regBatterySOC] = 955
	setI32(regs, regBatteryPower, 800)
	setU32(regs, regBatteryChargeT, 250) // 2.50 kWh

	client := newWithRegisters(&fakeRegisters{regs: regs}, func() time.Time { return time.Unix(0, 0) })
	reading, err := client.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if reading.PVW != 4000 {
		t.Errorf("PVW = %v, want 4000", reading.PVW)
	}
	if reading.GridW != -1500 {
		t.Errorf("GridW = %v, want -1500 (export)", reading.GridW)
	}
	if reading.BatteryW != 800 {
		t.Errorf("BatteryW = %v, want 800 (charging positive)", reading.BatteryW)
	}
	// load = pv + grid - battery = 4000 - 1500 - 800
	if reading.LoadW != 1700 {
		t.Errorf("LoadW = %v, want 1700", reading.LoadW)
	}
	if reading.SOC != 95.5 {
		t.Errorf("SOC = %v, want 95.5", reading.SOC)
	}
	if reading.Mode != 2 {
		t.Errorf("Mode = %v, want 2 (on grid)", reading.Mode)
	}
	if got := reading.Extra["pv_today_kwh"]; got != 12.34 {
		t.Errorf("pv_today_kwh = %v, want 12.34", got)
	}
	if got := reading.Extra["capacity_kwh"]; got != 5.12 {
		t.Errorf("capacity_kwh = %v, want 5.12", got)
	}
	if len(reading.PVStrings) != 2 {
		t.Fatalf("PVStrings = %v", reading.PVStrings)
	}
	if reading.PVStrings[0] != 1500 {
		t.Errorf("string 1 = %v, want 1500", reading.PVStrings[0])
	}
}

func TestReadUsesMeterWhenOnline(t *testing.T) {
	regs := map[uint16]uint16{}
	setI32(regs, regInputPow, 3000)
	setI32(regs, regActivePower, 2000)
	regs[regPVStrings] = 1
	// Meter online, importing 500 W.
	regs[regMeterStatus] = 1
	setI32(regs, regMeterPower, 500)

	client := newWithRegisters(&fakeRegisters{regs: regs}, nil)
	reading, err := client.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if reading.GridW != 500 {
		t.Fatalf("GridW = %v, want 500 from the meter", reading.GridW)
	}
}

func TestReadIgnoresInvalidMarkers(t *testing.T) {
	regs := map[uint16]uint16{}
	regs[regPVStrings] = 1
	regs[regBatterySOC] = invalidU16
	setI32(regs, regBatteryPower, invalidI32)

	client := newWithRegisters(&fakeRegisters{regs: regs}, nil)
	reading, err := client.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if reading.SOC != 0 {
		t.Errorf("SOC = %v, want 0 for the invalid marker", reading.SOC)
	}
	if reading.BatteryW != 0 {
		t.Errorf("BatteryW = %v, want 0", reading.BatteryW)
	}
}

func TestIdentityDecodesStrings(t *testing.T) {
	regs := map[uint16]uint16{}
	packASCII(regs, regModel, "SUN2000-8KTL-M1")
	packASCII(regs, regSerial, "HV21A0000001")
	packASCII(regs, regFirmware, "V200R001C00")
	setU32(regs, regRatedPower, 8000)
	regs[regPVStrings] = 2

	client := newWithRegisters(&fakeRegisters{regs: regs}, nil)
	identity, err := client.Identity(context.Background())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if identity.Serial != "HV21A0000001" {
		t.Errorf("serial = %q", identity.Serial)
	}
	if identity.Model != "Huawei SUN2000 SUN2000-8KTL-M1" {
		t.Errorf("model = %q", identity.Model)
	}
	if identity.Firmware != "V200R001C00" {
		t.Errorf("firmware = %q", identity.Firmware)
	}
	if identity.RatedPowerW != 8000 {
		t.Errorf("rated = %v", identity.RatedPowerW)
	}
}

func TestSetPassiveChargeOrder(t *testing.T) {
	fake := &fakeRegisters{regs: map[uint16]uint16{}}
	client := newWithRegisters(fake, nil)

	if err := client.SetPassive(context.Background(), 2, 3000); err != nil {
		t.Fatalf("set passive: %v", err)
	}
	if len(fake.writes) != 4 {
		t.Fatalf("writes = %+v, want four", fake.writes)
	}
	if fake.writes[0].addr != regForcibleCharge {
		t.Errorf("first write = 0x%04X, want the charge power", fake.writes[0].addr)
	}
	// 3000 W as high/low words.
	if fake.writes[0].values[0] != 0 || fake.writes[0].values[1] != 3000 {
		t.Errorf("power words = %v", fake.writes[0].values)
	}
	last := fake.writes[len(fake.writes)-1]
	if last.addr != regForcibleCmd || last.values[0] != ForcibleCharge {
		t.Errorf("last write = 0x%04X %v, want the command", last.addr, last.values)
	}
}

func TestSetPassiveStopClearsSetpoints(t *testing.T) {
	fake := &fakeRegisters{regs: map[uint16]uint16{}}
	client := newWithRegisters(fake, nil)

	if err := client.SetPassive(context.Background(), 0, 0); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if fake.writes[0].addr != regForcibleCmd || fake.writes[0].values[0] != ForcibleStop {
		t.Fatalf("first write = %+v", fake.writes[0])
	}
}

func TestSetChargeMaxScaling(t *testing.T) {
	fake := &fakeRegisters{regs: map[uint16]uint16{}}
	client := newWithRegisters(fake, nil)

	if err := client.SetChargeMax(context.Background(), 95); err != nil {
		t.Fatalf("set charge max: %v", err)
	}
	if fake.writes[0].addr != regEndOfCharge || fake.writes[0].values[0] != 950 {
		t.Fatalf("write = %+v, want 950 at 0xB7E9", fake.writes[0])
	}
	if err := client.SetChargeMax(context.Background(), 50); err == nil {
		t.Fatal("out-of-range SOC accepted")
	}
}

func TestWriteScheduleIsUnsupported(t *testing.T) {
	client := newWithRegisters(&fakeRegisters{regs: map[uint16]uint16{}}, nil)
	_, err := client.WriteSchedule(context.Background(), domain.Schedule{})
	if !errors.Is(err, ErrScheduleUnsupported) {
		t.Fatalf("err = %v", err)
	}
}
