package solplanet

import (
	"encoding/json"
	"strconv"
	"strings"
)

// floatValue extracts a numeric field that may arrive as a JSON number, a
// numeric string, or null.
func floatValue(m map[string]json.RawMessage, key string) (float64, bool) {
	raw, ok := m[key]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		v, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return v, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

func intValue(m map[string]json.RawMessage, key string) (int, bool) {
	v, ok := floatValue(m, key)
	return int(v), ok
}

func stringValue(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

func floatSlice(m map[string]json.RawMessage, key string) []float64 {
	raw, ok := m[key]
	if !ok || string(raw) == "null" {
		return nil
	}
	var numbers []json.Number
	if err := json.Unmarshal(raw, &numbers); err != nil {
		// Some firmware returns strings inside arrays; try that shape.
		var stringsList []string
		if err2 := json.Unmarshal(raw, &stringsList); err2 != nil {
			return nil
		}
		out := make([]float64, 0, len(stringsList))
		for _, s := range stringsList {
			v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err == nil {
				out = append(out, v)
			}
		}
		return out
	}
	out := make([]float64, 0, len(numbers))
	for _, n := range numbers {
		v, err := n.Float64()
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

func objectValue(m map[string]json.RawMessage, key string) map[string]json.RawMessage {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}
