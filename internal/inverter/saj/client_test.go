package saj

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// fakeRegisters serves reads from a map, so decoding can be checked without a
// device or a socket.
type fakeRegisters struct {
	regs   map[uint16]uint16
	writes []struct {
		addr   uint16
		values []uint16
	}
}

func (f *fakeRegisters) readHolding(ctx context.Context, addr, count uint16) ([]uint16, error) {
	out := make([]uint16, count)
	for i := uint16(0); i < count; i++ {
		out[i] = f.regs[addr+i]
	}
	return out, nil
}

func (f *fakeRegisters) writeRegisters(ctx context.Context, addr uint16, values []uint16) error {
	f.writes = append(f.writes, struct {
		addr   uint16
		values []uint16
	}{addr, append([]uint16(nil), values...)})
	return nil
}

func (f *fakeRegisters) close() error { return nil }

// signedReg stores a signed reading in the unsigned register map.
func signedReg(value int16) uint16 { return uint16(value) }

func TestReadDecodesSignsAndScaling(t *testing.T) {
	fake := &fakeRegisters{regs: map[uint16]uint16{
		regWorkingMode:    2,
		regSummaryPV:      4200,
		regMeter2Power:    signedReg(-1500), // raw inverted: means 1500 W import
		regSummaryBattery: signedReg(800),   // raw positive: means 800 W discharge
		regSystemLoad:     2500,
		regPV1Power:       3000,
		regPV2Power:       1200,
		regBatterySOC:     6530, // 65.30 %
		regPVEnergyToday:  0,
	}}
	// Daily counters are uint32 over two registers: 12.34 kWh.
	fake.regs[regPVEnergyToday+1] = 1234

	client := newWithRegisters(fake, func() time.Time { return time.Unix(0, 0) })
	reading, err := client.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if reading.PVW != 4200 {
		t.Errorf("PVW = %v, want 4200", reading.PVW)
	}
	if reading.GridW != 1500 {
		t.Errorf("GridW = %v, want 1500 (import positive)", reading.GridW)
	}
	if reading.BatteryW != -800 {
		t.Errorf("BatteryW = %v, want -800 (charging negative)", reading.BatteryW)
	}
	if reading.LoadW != 2500 {
		t.Errorf("LoadW = %v, want 2500", reading.LoadW)
	}
	if reading.SOC != 65.3 {
		t.Errorf("SOC = %v, want 65.3", reading.SOC)
	}
	if reading.Mode != 2 {
		t.Errorf("Mode = %v, want 2", reading.Mode)
	}
	if got := reading.Extra["pv_today_kwh"]; got != 12.34 {
		t.Errorf("pv_today_kwh = %v, want 12.34", got)
	}
}

func TestReadAcceptsWholePercentSOC(t *testing.T) {
	fake := &fakeRegisters{regs: map[uint16]uint16{regBatterySOC: 47}}
	client := newWithRegisters(fake, nil)
	reading, err := client.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if reading.SOC != 47 {
		t.Fatalf("SOC = %v, want 47", reading.SOC)
	}
}

func TestIdentityDecodesSerialAndFirmware(t *testing.T) {
	fake := &fakeRegisters{regs: map[uint16]uint16{}}
	serial := "SAJ1234567"
	for i, char := range []byte(serial) {
		reg := regSerial + uint16(i/2)
		if i%2 == 0 {
			fake.regs[reg] |= uint16(char) << 8
		} else {
			fake.regs[reg] |= uint16(char)
		}
	}
	fake.regs[regFirmware] = 1234 // 1.234

	client := newWithRegisters(fake, nil)
	identity, err := client.Identity(context.Background())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if identity.Serial != serial {
		t.Errorf("serial = %q, want %q", identity.Serial, serial)
	}
	if identity.Firmware != "1.234" {
		t.Errorf("firmware = %q, want 1.234", identity.Firmware)
	}
}

func TestSetPassiveWritesAppModeAndPower(t *testing.T) {
	fake := &fakeRegisters{regs: map[uint16]uint16{}}
	client := newWithRegisters(fake, nil)

	if err := client.SetPassive(context.Background(), PassiveCharge, 3000); err != nil {
		t.Fatalf("set passive: %v", err)
	}

	want := map[uint16]uint16{
		regAppMode:       AppPassive,
		regPassiveCharge: 3000,
		regPassiveMode:   PassiveCharge,
	}
	for addr, value := range want {
		found := false
		for _, write := range fake.writes {
			if write.addr == addr && len(write.values) == 1 && write.values[0] == value {
				found = true
			}
		}
		if !found {
			t.Errorf("no write of %d to 0x%04X; writes = %+v", value, addr, fake.writes)
		}
	}
}

func TestWriteScheduleIsExplicitlyUnsupported(t *testing.T) {
	client := newWithRegisters(&fakeRegisters{regs: map[uint16]uint16{}}, nil)
	_, err := client.WriteSchedule(context.Background(), domain.Schedule{
		Days: map[string][]domain.ScheduleSlot{},
	})
	if !errors.Is(err, ErrScheduleUnsupported) {
		t.Fatalf("err = %v, want ErrScheduleUnsupported", err)
	}
}
