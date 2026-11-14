package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func testEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := LoadFromEnv(testEnv(nil))
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Listen != "0.0.0.0:8080" {
		t.Errorf("Listen = %q", cfg.Listen)
	}
	if cfg.DataDir != "/data" {
		t.Errorf("DataDir = %q", cfg.DataDir)
	}
	if cfg.Timezone != "Europe/Stockholm" {
		t.Errorf("Timezone = %q", cfg.Timezone)
	}
	if cfg.EnableWrites || cfg.EVControlMode != EVControlAuto || cfg.Simulator {
		t.Errorf("gates must default to closed: %+v", cfg)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v", cfg.LogLevel)
	}
	if cfg.DatabasePath != filepath.Join("/data", "aiptimize.db") {
		t.Errorf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.NordPoolBaseURL != "https://dataportal-api.nordpoolgroup.com/api/DayAheadPrices" {
		t.Errorf("NordPoolBaseURL = %q", cfg.NordPoolBaseURL)
	}
	if cfg.OpenMeteoBaseURL != "https://api.open-meteo.com/v1/forecast" {
		t.Errorf("OpenMeteoBaseURL = %q", cfg.OpenMeteoBaseURL)
	}
}

func TestEVControlMode(t *testing.T) {
	cases := map[string]EVControlMode{
		"":      EVControlAuto,
		"auto":  EVControlAuto,
		"on":    EVControlOn,
		"true":  EVControlOn,
		"1":     EVControlOn,
		"off":   EVControlOff,
		"false": EVControlOff,
	}
	for in, want := range cases {
		cfg, err := LoadFromEnv(testEnv(map[string]string{"AIPTIMIZE_ENABLE_EV_CONTROL": in}))
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if cfg.EVControlMode != want {
			t.Errorf("%q = %q, want %q", in, cfg.EVControlMode, want)
		}
	}
	if _, err := LoadFromEnv(testEnv(map[string]string{"AIPTIMIZE_ENABLE_EV_CONTROL": "maybe"})); err == nil {
		t.Error("invalid EV control mode was accepted")
	}
	if !EVControlAuto.Resolve(true) || EVControlAuto.Resolve(false) {
		t.Error("auto mode must follow charger health")
	}
	if !EVControlOn.Resolve(false) || EVControlOff.Resolve(true) {
		t.Error("forced modes must ignore charger health")
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := LoadFromEnv(testEnv(map[string]string{
		"AIPTIMIZE_LISTEN":             "127.0.0.1:9000",
		"AIPTIMIZE_DATA_DIR":           t.TempDir(),
		"AIPTIMIZE_LOG_LEVEL":          "debug",
		"AIPTIMIZE_TZ":                 "UTC",
		"AIPTIMIZE_ENABLE_WRITES":      "true",
		"AIPTIMIZE_SIMULATOR":          "1",
		"AIPTIMIZE_NORDPOOL_BASE_URL":  "http://127.0.0.1:9999/prices",
		"AIPTIMIZE_OPENMETEO_BASE_URL": "http://127.0.0.1:9999/weather",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Listen != "127.0.0.1:9000" || !cfg.EnableWrites || !cfg.Simulator {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if cfg.NordPoolBaseURL != "http://127.0.0.1:9999/prices" {
		t.Errorf("NordPoolBaseURL = %q", cfg.NordPoolBaseURL)
	}
	if cfg.OpenMeteoBaseURL != "http://127.0.0.1:9999/weather" {
		t.Errorf("OpenMeteoBaseURL = %q", cfg.OpenMeteoBaseURL)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v", cfg.LogLevel)
	}
}

func TestLoadInvalid(t *testing.T) {
	cases := map[string]map[string]string{
		"bad listen":        {"AIPTIMIZE_LISTEN": "nope"},
		"bad tz":            {"AIPTIMIZE_TZ": "Mars/Olympus"},
		"short password":    {"AIPTIMIZE_ADMIN_PASSWORD": "short"},
		"bad bool":          {"AIPTIMIZE_ENABLE_WRITES": "yes please"},
		"bad level":         {"AIPTIMIZE_LOG_LEVEL": "verbose"},
		"bad cookie secure": {"AIPTIMIZE_COOKIE_SECURE": "sometimes"},
		"bad prices url":    {"AIPTIMIZE_NORDPOOL_BASE_URL": "nordpool"},
		"bad weather url":   {"AIPTIMIZE_OPENMETEO_BASE_URL": "weather"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadFromEnv(testEnv(env)); err == nil {
				t.Fatalf("expected error for %v", env)
			}
		})
	}
}

func TestEnsureDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	cfg := &Config{DataDir: dir}
	if err := cfg.EnsureDataDir(); err != nil {
		t.Fatalf("EnsureDataDir: %v", err)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("data dir not created: %v", err)
	}
}
