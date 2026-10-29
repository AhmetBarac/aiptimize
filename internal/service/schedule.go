package service

import (
	"fmt"
	"sort"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/settings"
)

var scheduleDayKeys = []string{"Mon", "Tus", "Wen", "Thu", "Fri", "Sat", "Sun"}

// BuildSchedule constructs the full seven-day inverter payload. Only the local
// day of the manual plan and the adjacent auto day are populated; every other
// day is explicitly cleared.
func BuildSchedule(values settings.Values, day string, manual, auto *domain.Plan) (domain.Schedule, error) {
	loc, err := time.LoadLocation(values.Site.Timezone)
	if err != nil {
		return domain.Schedule{}, fmt.Errorf("schedule: invalid timezone: %w", err)
	}
	date, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return domain.Schedule{}, fmt.Errorf("schedule: invalid day %q", day)
	}
	out := domain.Schedule{Days: map[string][]domain.ScheduleSlot{}, PinW: values.Plan.ChargeW, PoutW: values.Plan.DischargeW}
	for _, key := range scheduleDayKeys {
		out.Days[key] = []domain.ScheduleSlot{}
	}
	for offset := 0; offset < 2; offset++ {
		localDay := date.AddDate(0, 0, offset).Format("2006-01-02")
		key := scheduleDayKey(date.AddDate(0, 0, offset))
		var slots []domain.Slot
		if manual != nil && manual.LockedDay == localDay {
			slots = manual.Slots
		} else if auto != nil {
			for _, slot := range auto.Slots {
				if slot.DayLocal == localDay {
					slots = append(slots, slot)
				}
			}
		}
		sort.Slice(slots, func(i, j int) bool { return slots[i].Start.Before(slots[j].Start) })
		for _, slot := range slots {
			start := slot.Start.In(loc)
			duration := int(slot.End.Sub(slot.Start).Minutes())
			if duration < 30 || duration > 240 || duration%30 != 0 {
				return domain.Schedule{}, fmt.Errorf("schedule: %s slot has invalid duration %d", key, duration)
			}
			if start.Hour() < 0 || start.Hour() > 23 || (start.Minute() != 0 && start.Minute() != 30) {
				return domain.Schedule{}, fmt.Errorf("schedule: %s slot does not start on :00/:30", key)
			}
			out.Days[key] = append(out.Days[key], domain.ScheduleSlot{
				Hour: start.Hour(), Minute: start.Minute(), DurationMin: duration,
				Discharge: slot.Direction == domain.DirectionDischarge,
			})
		}
		if len(out.Days[key]) > 6 {
			return domain.Schedule{}, fmt.Errorf("schedule: %s has more than 6 slots", key)
		}
	}
	return out, nil
}

func scheduleDayKey(t time.Time) string {
	switch t.Weekday() {
	case time.Monday:
		return "Mon"
	case time.Tuesday:
		return "Tus"
	case time.Wednesday:
		return "Wen"
	case time.Thursday:
		return "Thu"
	case time.Friday:
		return "Fri"
	case time.Saturday:
		return "Sat"
	default:
		return "Sun"
	}
}

// ScheduleDiff returns human-readable differences from desired to actual.
func ScheduleDiff(desired, actual domain.Schedule) []string {
	var out []string
	for _, day := range scheduleDayKeys {
		a := desired.Days[day]
		b := actual.Days[day]
		if len(a) != len(b) {
			out = append(out, fmt.Sprintf("%s: %d slot(s) desired, %d in inverter", day, len(a), len(b)))
			continue
		}
		for i := range a {
			if a[i] != b[i] {
				out = append(out, fmt.Sprintf("%s: slot %d differs", day, i+1))
			}
		}
	}
	if desired.PinW != actual.PinW {
		out = append(out, fmt.Sprintf("Pin: %.0f desired, %.0f in inverter", desired.PinW, actual.PinW))
	}
	if desired.PoutW != actual.PoutW {
		out = append(out, fmt.Sprintf("Pout: %.0f desired, %.0f in inverter", desired.PoutW, actual.PoutW))
	}
	return out
}

// ScheduleEqual compares the seven days and the setpoints.
func ScheduleEqual(a, b domain.Schedule) bool {
	return len(ScheduleDiff(a, b)) == 0
}
