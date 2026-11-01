package settings

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"

	"github.com/AhmetBarac/aiptimize/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestDefaults(t *testing.T) {
	st := newStore(t)
	svc, err := New(context.Background(), st)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	v := svc.Get()
	if v.Site.Timezone != "Europe/Stockholm" || v.Site.MaxImportPowerW != 17250 {
		t.Errorf("site defaults: %+v", v.Site)
	}
	if v.Inverter.CapacityKWh != 15.36 || v.Inverter.MaxChargeW != 6000 || v.Inverter.MaxDischargeW != 3000 {
		t.Errorf("inverter defaults: %+v", v.Inverter)
	}
	if v.Plan.MaxSlotsPerDay != 4 || v.Plan.MaxSlotMin != 240 || v.Plan.HorizonDays != 2 {
		t.Errorf("plan defaults: %+v", v.Plan)
	}
	if v.EV.MinCurrentA != 6 || v.EV.MaxCurrentA != 16 || v.EV.GridChargeAllowed {
		t.Errorf("ev defaults: %+v", v.EV)
	}
	if v.Safety.WritesArmedUI || v.Safety.WatchdogClearAfterS != 1800 || v.Safety.AutoScheduleWrite != "off" {
		t.Errorf("safety defaults: %+v", v.Safety)
	}
	var sum float64
	for _, p := range v.Load.Profile {
		if p < 0 {
			t.Errorf("negative profile value %v", p)
		}
		sum += p
	}
	if math.Abs(sum-1) > 1e-6 {
		t.Errorf("profile sum = %v", sum)
	}

	// All sections must have been seeded.
	all, err := st.AllSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(Sections) {
		t.Errorf("seeded %d sections, want %d", len(all), len(Sections))
	}
}

func TestPatchAndPersist(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	svc, err := New(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := svc.Patch(ctx, SectionSite, json.RawMessage(`{"name":"Cabin","max_import_power_w":11000}`))
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	var site Site
	if err := json.Unmarshal(raw, &site); err != nil {
		t.Fatal(err)
	}
	if site.Name != "Cabin" || site.MaxImportPowerW != 11000 || site.Timezone != "Europe/Stockholm" {
		t.Fatalf("patched site = %+v", site)
	}

	// Reload from disk.
	svc2, err := New(ctx, st)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if svc2.Get().Site.Name != "Cabin" {
		t.Fatalf("patch not persisted: %+v", svc2.Get().Site)
	}

	// Reset restores the default name.
	if _, err := svc2.Reset(ctx, SectionSite); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if svc2.Get().Site.Name != "Home" {
		t.Fatalf("reset failed: %+v", svc2.Get().Site)
	}
}

func TestPatchRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	svc, err := New(ctx, newStore(t))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		section string
		patch   string
	}{
		{"unknown section", "nope", `{"x":1}`},
		{"unknown field", SectionSite, `{"nope":1}`},
		{"bad timezone", SectionSite, `{"timezone":"Mars/Base"}`},
		{"bad latitude", SectionSite, `{"latitude":99}`},
		{"bad port", SectionInverter, `{"port":70000}`},
		{"min above max soc", SectionInverter, `{"min_soc":90,"max_soc":50}`},
		{"bad scheme", SectionInverter, `{"scheme":"ftp"}`},
		{"too many strings", SectionPV, `{"strings":[{"kwp":1},{"kwp":1},{"kwp":1},{"kwp":1}]}`},
		{"bad slot duration", SectionPlan, `{"max_slot_min":45}`},
		{"bad profile", SectionLoad, `{"profile":[1,1]}`},
		{"bad ev current", SectionEV, `{"min_current_a":5}`},
		{"bad ev mode", SectionEV, `{"mode":"turbo"}`},
		{"bad ev provider", SectionEV, `{"provider":"ocpp"}`},
		{"bad watchdog", SectionSafety, `{"watchdog_clear_after_s":10}`},
		{"bad auto schedule write", SectionSafety, `{"auto_schedule_write":"always"}`},
		{"null patch", SectionSite, `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Patch(ctx, tc.section, json.RawMessage(tc.patch)); err == nil {
				t.Fatalf("expected error for %s", tc.patch)
			}
		})
	}
}

func TestPVStrings(t *testing.T) {
	ctx := context.Background()
	svc, err := New(ctx, newStore(t))
	if err != nil {
		t.Fatal(err)
	}
	patch := `{"strings":[{"name":"East","kwp":4.2,"tilt":35,"azimuth":120,"performance_ratio":0.85,"temp_coefficient":-0.004},{"name":"West","kwp":4.2,"tilt":35,"azimuth":240,"performance_ratio":0.85,"temp_coefficient":-0.004}]}`
	if _, err := svc.Patch(ctx, SectionPV, json.RawMessage(patch)); err != nil {
		t.Fatalf("Patch PV: %v", err)
	}
	v := svc.Get().PV
	if len(v.Strings) != 2 || v.Strings[0].Name != "East" || v.Strings[1].KWP != 4.2 {
		t.Fatalf("strings = %+v", v.Strings)
	}
}
