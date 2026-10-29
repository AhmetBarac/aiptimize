// Package config loads and validates the process configuration from the
// environment. Secrets are only ever read from the environment; they are never
// persisted in the database or returned by the HTTP API.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // embed the IANA timezone database
)

// CookieSecureMode controls the Secure flag on auth cookies.
type CookieSecureMode string

const (
	// CookieSecureAuto sets Secure when the request arrived over TLS or with a
	// trusted X-Forwarded-Proto: https header. This keeps plain-LAN deployments
	// working while still using Secure behind a reverse proxy.
	CookieSecureAuto   CookieSecureMode = "auto"
	CookieSecureAlways CookieSecureMode = "always"
	CookieSecureNever  CookieSecureMode = "never"
)

// Config is the effective process configuration.
type Config struct {
	Listen           string
	DataDir          string
	DatabasePath     string
	LogLevel         slog.Level
	LogFormat        string
	Timezone         string
	AdminUsername    string
	AdminPassword    string
	SessionSecret    string
	CookieSecure     CookieSecureMode
	TrustProxy       bool
	EnableWrites     bool
	EnableEVControl  bool
	Simulator        bool
	NordPoolBaseURL  string
	OpenMeteoBaseURL string
	Zaptec           ZaptecConfig
	StaticDir        string
	Version          string
	Commit           string
	BuildDate        string
}

// ZaptecConfig holds the Zaptec cloud credentials (environment only).
type ZaptecConfig struct {
	Username       string
	Password       string
	InstallationID string
}

// Load reads configuration from the process environment.
func Load() (*Config, error) {
	return LoadFromEnv(os.Getenv)
}

// LoadFromEnv is the testable implementation of Load.
func LoadFromEnv(getenv func(string) string) (*Config, error) {
	cfg := &Config{
		Listen:           env(getenv, "AIPTIMIZE_LISTEN", "0.0.0.0:8080"),
		DataDir:          env(getenv, "AIPTIMIZE_DATA_DIR", "/data"),
		LogFormat:        strings.ToLower(env(getenv, "AIPTIMIZE_LOG_FORMAT", "json")),
		Timezone:         env(getenv, "AIPTIMIZE_TZ", "Europe/Stockholm"),
		AdminUsername:    env(getenv, "AIPTIMIZE_ADMIN_USERNAME", "admin"),
		AdminPassword:    getenv("AIPTIMIZE_ADMIN_PASSWORD"),
		SessionSecret:    getenv("AIPTIMIZE_SESSION_SECRET"),
		CookieSecure:     CookieSecureMode(strings.ToLower(env(getenv, "AIPTIMIZE_COOKIE_SECURE", string(CookieSecureAuto)))),
		StaticDir:        getenv("AIPTIMIZE_STATIC_DIR"),
		NordPoolBaseURL:  env(getenv, "AIPTIMIZE_NORDPOOL_BASE_URL", "https://dataportal-api.nordpoolgroup.com/api/DayAheadPrices"),
		OpenMeteoBaseURL: env(getenv, "AIPTIMIZE_OPENMETEO_BASE_URL", "https://api.open-meteo.com/v1/forecast"),
		Version:          env(getenv, "AIPTIMIZE_VERSION", "dev"),
		Commit:           getenv("AIPTIMIZE_COMMIT"),
		BuildDate:        getenv("AIPTIMIZE_BUILD_DATE"),
		Zaptec: ZaptecConfig{
			Username:       getenv("AIPTIMIZE_ZAPTEC_USERNAME"),
			Password:       getenv("AIPTIMIZE_ZAPTEC_PASSWORD"),
			InstallationID: getenv("AIPTIMIZE_ZAPTEC_INSTALLATION_ID"),
		},
	}

	level, err := parseLogLevel(env(getenv, "AIPTIMIZE_LOG_LEVEL", "info"))
	if err != nil {
		return nil, err
	}
	cfg.LogLevel = level

	if cfg.EnableWrites, err = parseBool(getenv("AIPTIMIZE_ENABLE_WRITES")); err != nil {
		return nil, fmt.Errorf("AIPTIMIZE_ENABLE_WRITES: %w", err)
	}
	if cfg.EnableEVControl, err = parseBool(getenv("AIPTIMIZE_ENABLE_EV_CONTROL")); err != nil {
		return nil, fmt.Errorf("AIPTIMIZE_ENABLE_EV_CONTROL: %w", err)
	}
	if cfg.Simulator, err = parseBool(getenv("AIPTIMIZE_SIMULATOR")); err != nil {
		return nil, fmt.Errorf("AIPTIMIZE_SIMULATOR: %w", err)
	}
	if cfg.TrustProxy, err = parseBool(env(getenv, "AIPTIMIZE_TRUST_PROXY", "false")); err != nil {
		return nil, fmt.Errorf("AIPTIMIZE_TRUST_PROXY: %w", err)
	}

	cfg.DatabasePath = filepath.Join(cfg.DataDir, "aiptimize.db")
	if v := getenv("AIPTIMIZE_DATABASE_PATH"); v != "" {
		cfg.DatabasePath = v
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks the configuration for consistency.
func (c *Config) Validate() error {
	if c.Listen == "" {
		return errors.New("listen address must not be empty")
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("invalid listen address %q: %w", c.Listen, err)
	}
	if c.DataDir == "" {
		return errors.New("data directory must not be empty")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("invalid timezone %q: %w", c.Timezone, err)
	}
	switch c.CookieSecure {
	case CookieSecureAuto, CookieSecureAlways, CookieSecureNever:
	default:
		return fmt.Errorf("invalid cookie secure mode %q", c.CookieSecure)
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		return fmt.Errorf("invalid log format %q (want json or text)", c.LogFormat)
	}
	if c.AdminPassword != "" && len(c.AdminPassword) < 8 {
		return errors.New("AIPTIMIZE_ADMIN_PASSWORD must be at least 8 characters")
	}
	if c.SessionSecret != "" && len(c.SessionSecret) < 32 {
		return errors.New("AIPTIMIZE_SESSION_SECRET must be at least 32 characters")
	}
	if c.AdminUsername == "" {
		return errors.New("AIPTIMIZE_ADMIN_USERNAME must not be empty")
	}
	if c.NordPoolBaseURL == "" {
		return errors.New("AIPTIMIZE_NORDPOOL_BASE_URL must not be empty")
	}
	if u, err := url.Parse(c.NordPoolBaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid AIPTIMIZE_NORDPOOL_BASE_URL %q", c.NordPoolBaseURL)
	}
	if u, err := url.Parse(c.OpenMeteoBaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid AIPTIMIZE_OPENMETEO_BASE_URL %q", c.OpenMeteoBaseURL)
	}
	return nil
}

// Location returns the configured timezone.
func (c *Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Logger builds the process logger.
func (c *Config) Logger() *slog.Logger {
	opts := &slog.HandlerOptions{Level: c.LogLevel}
	var h slog.Handler
	if c.LogFormat == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h).With("version", c.Version)
}

// EnsureDataDir creates the data directory if it does not exist.
func (c *Config) EnsureDataDir() error {
	if err := os.MkdirAll(c.DataDir, 0o750); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	return nil
}

func env(getenv func(string) string, key, def string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return def
}

func parseBool(v string) (bool, error) {
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return false, fmt.Errorf("invalid boolean %q", v)
	}
	return b, nil
}

func parseLogLevel(v string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q", v)
	}
}
