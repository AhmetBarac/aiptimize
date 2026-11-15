package huawei

import (
	"context"
	"testing"
	"time"
)

// The controller passes the remaining slot time, which has to reach the
// duration register as whole minutes.
func TestSetPassiveForWritesDuration(t *testing.T) {
	fake := &fakeRegisters{regs: map[uint16]uint16{}}
	client := newWithRegisters(fake, nil)

	if err := client.SetPassiveFor(context.Background(), 2, 4000, 45*time.Minute); err != nil {
		t.Fatalf("set passive for: %v", err)
	}

	var duration uint16
	var command uint16
	for _, write := range fake.writes {
		if write.addr == regForcibleDur {
			duration = write.values[0]
		}
		if write.addr == regForcibleCmd {
			command = write.values[0]
		}
	}
	if duration != 45 {
		t.Fatalf("duration = %d minutes, want 45", duration)
	}
	if command != ForcibleCharge {
		t.Fatalf("command = %d, want charge", command)
	}
}

// A short remainder must not round down to zero minutes, and a window longer
// than the inverter accepts is clamped.
func TestSetPassiveForClampsDuration(t *testing.T) {
	fake := &fakeRegisters{regs: map[uint16]uint16{}}
	client := newWithRegisters(fake, nil)

	if err := client.SetPassiveFor(context.Background(), 1, 2000, 20*time.Second); err != nil {
		t.Fatalf("short window: %v", err)
	}
	if got := durationWritten(fake.writes); got != 1 {
		t.Fatalf("short window duration = %d, want 1 minute", got)
	}

	fake.writes = nil
	if err := client.SetPassiveFor(context.Background(), 1, 2000, 48*time.Hour); err != nil {
		t.Fatalf("long window: %v", err)
	}
	if got := durationWritten(fake.writes); got != 1440 {
		t.Fatalf("long window duration = %d, want 1440", got)
	}
}

func durationWritten(writes []struct {
	addr   uint16
	values []uint16
}) uint16 {
	for _, write := range writes {
		if write.addr == regForcibleDur {
			return write.values[0]
		}
	}
	return 0
}
