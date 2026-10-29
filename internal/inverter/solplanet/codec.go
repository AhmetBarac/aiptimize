package solplanet

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// DayKeys are the exact keys the dongle expects. "Tus" and "Wen" are the
// firmware's misspellings and must not be corrected.
var DayKeys = []string{"Mon", "Tus", "Wen", "Thu", "Fri", "Sat", "Sun"}

// DecodeSchedule parses a getdefine.cgi payload into a domain schedule.
// Invalid slot codes are skipped and reported as warnings; the caller can show
// a foreign or corrupt schedule without failing the whole read.
func DecodeSchedule(payload []byte) (domain.Schedule, []string, error) {
	var fields map[string]json.RawMessage
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.UseNumber()
	if err := dec.Decode(&fields); err != nil {
		return domain.Schedule{}, nil, fmt.Errorf("schedule: invalid JSON: %w", err)
	}
	return decodeScheduleFields(fields)
}

func decodeScheduleFields(fields map[string]json.RawMessage) (domain.Schedule, []string, error) {
	out := domain.Schedule{Days: make(map[string][]domain.ScheduleSlot, len(DayKeys))}
	var warnings []string
	for _, day := range DayKeys {
		raw, ok := fields[day]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("missing day %s", day))
			out.Days[day] = []domain.ScheduleSlot{}
			continue
		}
		codes, err := decodeCodes(raw)
		if err != nil {
			return domain.Schedule{}, nil, fmt.Errorf("schedule: day %s: %w", day, err)
		}
		slots := make([]domain.ScheduleSlot, 0, len(codes))
		for _, code := range codes {
			if code == 0 {
				continue
			}
			slot, err := DecodeSlot(code)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", day, err))
				continue
			}
			slots = append(slots, slot)
		}
		sort.Slice(slots, func(i, j int) bool {
			a := slots[i].Hour*60 + slots[i].Minute
			b := slots[j].Hour*60 + slots[j].Minute
			return a < b
		})
		out.Days[day] = slots
	}
	if v, ok := fields["Pin"]; ok {
		out.PinW = parseNumber(v)
	}
	if v, ok := fields["Pout"]; ok {
		out.PoutW = parseNumber(v)
	}
	return out, warnings, nil
}

// DecodeSlot decodes one packed schedule entry.
//
//	slot = (hour << 24) | (minute << 16) | (durationMinutes << 8) | 0x02 | discharge
func DecodeSlot(code uint32) (domain.ScheduleSlot, error) {
	if code == 0 {
		return domain.ScheduleSlot{}, nil
	}
	hour := int((code >> 24) & 0xFF)
	minute := int((code >> 16) & 0xFF)
	dur := int((code >> 8) & 0xFF)
	discharge := code&1 == 1
	if hour > 23 {
		return domain.ScheduleSlot{}, fmt.Errorf("slot %#08x: invalid hour %d", code, hour)
	}
	if minute != 0 && minute != 30 {
		return domain.ScheduleSlot{}, fmt.Errorf("slot %#08x: start must be on :00/:30", code)
	}
	if dur < 30 || dur > 240 || dur%30 != 0 {
		return domain.ScheduleSlot{}, fmt.Errorf("slot %#08x: invalid duration %d", code, dur)
	}
	if marker := (code >> 1) & 0x7F; marker != 1 {
		return domain.ScheduleSlot{}, fmt.Errorf("slot %#08x: unexpected marker bits %d", code, marker)
	}
	return domain.ScheduleSlot{
		Hour:        hour,
		Minute:      minute,
		DurationMin: dur,
		Discharge:   discharge,
		Code:        code,
	}, nil
}

// SlotStartMinutes returns the start of day in minutes.
func SlotStartMinutes(s domain.ScheduleSlot) int { return s.Hour*60 + s.Minute }

func decodeCodes(raw json.RawMessage) ([]uint32, error) {
	var numbers []json.Number
	if err := json.Unmarshal(raw, &numbers); err != nil {
		return nil, err
	}
	out := make([]uint32, 0, len(numbers))
	for _, n := range numbers {
		v, err := strconv.ParseUint(n.String(), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid slot code %q", n.String())
		}
		out = append(out, uint32(v))
	}
	return out, nil
}

func parseNumber(raw json.RawMessage) float64 {
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		v, _ := n.Float64()
		return v
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return v
	}
	return 0
}
