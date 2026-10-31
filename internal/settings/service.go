package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/store"
)

// Service provides concurrency-safe access to the settings document.
type Service struct {
	mu     sync.RWMutex
	st     *store.Store
	values Values
}

// New loads settings from the store, seeding defaults for missing sections and
// validating everything it reads.
func New(ctx context.Context, st *store.Store) (*Service, error) {
	s := &Service{st: st, values: Default()}
	stored, err := st.AllSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	for _, name := range Sections {
		ptr, err := s.values.sectionValue(name)
		if err != nil {
			return nil, err
		}
		raw, ok := stored[name]
		if ok {
			if err := decodeSection(raw, ptr); err != nil {
				return nil, fmt.Errorf("settings section %q: %w", name, err)
			}
		}
		if err := ValidateSection(name, ptr); err != nil {
			return nil, err
		}
		if !ok {
			encoded, err := json.Marshal(ptr)
			if err != nil {
				return nil, err
			}
			if err := st.SetSetting(ctx, name, encoded); err != nil {
				return nil, fmt.Errorf("seed settings %q: %w", name, err)
			}
		}
	}
	return s, nil
}

// Get returns a deep copy of all settings.
func (s *Service) Get() Values {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.values)
}

// JSON returns all sections as raw JSON.
func (s *Service) JSON() (map[string]json.RawMessage, error) {
	v := s.Get()
	out := make(map[string]json.RawMessage, len(Sections))
	for _, name := range Sections {
		ptr, err := v.sectionValue(name)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(ptr)
		if err != nil {
			return nil, err
		}
		out[name] = publicSectionJSON(name, raw)
	}
	return out, nil
}

// Section returns one section as raw JSON.
func (s *Service) Section(name string) (json.RawMessage, error) {
	if !validSection(name) {
		return nil, fmt.Errorf("unknown settings section %q", name)
	}
	v := s.Get()
	ptr, err := v.sectionValue(name)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(ptr)
	if err != nil {
		return nil, err
	}
	return publicSectionJSON(name, raw), nil
}

// Patch applies a partial JSON object to one section, validates the merged
// result, persists it and returns the new section.
func (s *Service) Patch(ctx context.Context, name string, patch json.RawMessage) (json.RawMessage, error) {
	if !validSection(name) {
		return nil, fmt.Errorf("unknown settings section %q", name)
	}
	var fields map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(patch))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&fields); err != nil {
		return nil, &ValidationError{Section: name, Message: "patch must be a JSON object: " + err.Error()}
	}
	if fields == nil {
		return nil, &ValidationError{Section: name, Message: "patch must not be null"}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	next := clone(s.values)
	ptr, err := next.sectionValue(name)
	if err != nil {
		return nil, err
	}
	current, err := json.Marshal(ptr)
	if err != nil {
		return nil, err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(current, &merged); err != nil {
		return nil, err
	}
	for k, v := range fields {
		merged[k] = v
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	if err := decodeSection(encoded, ptr); err != nil {
		return nil, fmt.Errorf("settings %q: %w", name, err)
	}
	if err := ValidateSection(name, ptr); err != nil {
		return nil, err
	}
	newRaw, err := json.Marshal(ptr)
	if err != nil {
		return nil, err
	}
	if err := s.st.SetSetting(ctx, name, newRaw); err != nil {
		return nil, fmt.Errorf("persist settings %q: %w", name, err)
	}
	s.values = next
	return publicSectionJSON(name, newRaw), nil
}

// publicSectionJSON removes write-only secrets from API-facing settings.
func publicSectionJSON(name string, raw []byte) json.RawMessage {
	if name != SectionEV {
		return raw
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw
	}
	delete(fields, "zaptec_password")
	out, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return out
}

// Reset restores one section to its default.
func (s *Service) Reset(ctx context.Context, name string) (json.RawMessage, error) {
	if !validSection(name) {
		return nil, fmt.Errorf("unknown settings section %q", name)
	}
	defaults := Default()
	ptr, err := defaults.sectionValue(name)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(ptr)
	if err != nil {
		return nil, err
	}
	return s.Patch(ctx, name, raw)
}

// Location returns the site timezone.
func (s *Service) Location() *time.Location {
	v := s.Get()
	loc, err := time.LoadLocation(v.Site.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Tariff returns the configured tariff.
func (s *Service) Tariff() Tariff {
	p := s.Get().Price
	return Tariff{GridFee: p.GridFee, EnergyTax: p.EnergyTax, Markup: p.Markup, VAT: p.VAT, ExportFee: p.ExportFee}
}

// Tariff is the subset of price settings used by the planners.
type Tariff struct {
	GridFee   float64
	EnergyTax float64
	Markup    float64
	VAT       float64
	ExportFee float64
}

func decodeSection(raw json.RawMessage, target any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func clone(v Values) Values {
	raw, err := json.Marshal(v)
	if err != nil {
		// Values is always marshalable; this cannot happen.
		panic(err)
	}
	var out Values
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}
