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
	if cfg.EnableWrites || cfg.EnableEVControl || cfg.Simulator {
		t.Errorf("gates must default to false: %+v", cfg)
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
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := LoadFromEnv(testEnv(map[string]string{
		"AIPTIMIZE_LISTEN":            "127.0.0.1:9000",
		"AIPTIMIZE_DATA_DIR":          t.TempDir(),
		"AIPTIMIZE_LOG_LEVEL":         "debug",
		"AIPTIMIZE_TZ":                "UTC",
		"AIPTIMIZE_ENABLE_WRITES":     "true",
		"AIPTIMIZE_SIMULATOR":         "1",
		"AIPTIMIZE_NORDPOOL_BASE_URL": "http://127.0.0.1:9999/prices",
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
