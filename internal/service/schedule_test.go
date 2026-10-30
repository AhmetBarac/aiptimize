package service

import (
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

func TestScheduleDiffIgnoresGlobalPower(t *testing.T) {
	start := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	slot := domain.ScheduleSlot{Hour: 12, Minute: 0, DurationMin: 30, Discharge: false}
	a := domain.Schedule{Days: map[string][]domain.ScheduleSlot{}, PinW: 6000, PoutW: 3000}
	b := domain.Schedule{Days: map[string][]domain.ScheduleSlot{}, PinW: 1200, PoutW: 800}
	for _, day := range scheduleDayKeys {
		a.Days[day] = nil
		b.Days[day] = nil
	}
	a.Days["Fri"] = []domain.ScheduleSlot{slot}
	b.Days["Fri"] = []domain.ScheduleSlot{slot}
	if diff := ScheduleDiff(a, b); len(diff) != 0 {
		t.Fatalf("setpoints should not be structural diff: %v", diff)
	}
	if ScheduleEqual(a, b) {
		t.Fatal("setpoints must still make the schedules unequal")
	}
	_ = start
}
