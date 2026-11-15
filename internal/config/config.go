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

// EVControlMode decides how the environment half of the EV control gate is
// resolved. Auto derives it from the charger link: the gate opens once an
// adapter is configured and its status polls succeed, and closes again as soon
// as a poll fails. On/off pin it for operators who need a hard override.
type EVControlMode string

const (
	EVControlAuto EVControlMode = "auto"
	EVControlOn   EVControlMode = "on"
	EVControlOff  EVControlMode = "off"
)

// Effective returns the configured mode, treating the zero value as auto.
func (m EVControlMode) Effective() EVControlMode {
	if m == "" {
		return EVControlAuto
	}
	return m
}

// Resolve returns the effective environment gate for the given charger health.
func (m EVControlMode) Resolve(healthy bool) bool {
	switch m.Effective() {
	case EVControlOn:
		return true
	case EVControlOff:
		return false
	default:
		return healthy
	}
}

// Config is the effective process configuration.
type Config struct {
	Listen        string
	DataDir       string
	DatabasePath  string
	LogLevel      slog.Level
	LogFormat     string
	Timezone      string
	AdminUsername string
	AdminPassword string
	SessionSecret string
	CookieSecure  CookieSecureMode
	TrustProxy    bool
	// EnableWrites pins the environment half of the inverter write gate on. It
	// is derived from WriteMode and kept for callers that only care about the
	// pinned case.
	EnableWrites bool
	// WriteMode decides how the environment half of the inverter gate is
	// resolved: auto follows the inverter link, on/off pin it.
	WriteMode         EVControlMode
	EVControlMode     EVControlMode
	Simulator         bool
	NordPoolBaseURL   string
	OpenMeteoBaseURL  string
	Zaptec            ZaptecConfig
	StaticDir         string
	Version           string
	Commit            string
	BuildDate         string
	RegistryRepo      string
	RegistryUsername  string
	RegistryToken     string
	UpdateRequestFile string
	Discovery         bool
	DiscoveryPort     int
}

// ZaptecConfig holds the Zaptec cloud credentials (environment only).
type ZaptecConfig struct {
	Username       string
	Password       string
	InstallationID string
	ChargerID      string
	BaseURL        string
}

// Load reads configuration from the process environment.
func Load() (*Config, error) {
	return LoadFromEnv(os.Getenv)
}

// LoadFromEnv is the testable implementation of Load.
func LoadFromEnv(getenv func(string) string) (*Config, error) {
	cfg := &Config{
		Listen:            env(getenv, "AIPTIMIZE_LISTEN", "0.0.0.0:8080"),
		DataDir:           env(getenv, "AIPTIMIZE_DATA_DIR", "/data"),
		LogFormat:         strings.ToLower(env(getenv, "AIPTIMIZE_LOG_FORMAT", "json")),
		Timezone:          env(getenv, "AIPTIMIZE_TZ", "Europe/Stockholm"),
		AdminUsername:     env(getenv, "AIPTIMIZE_ADMIN_USERNAME", "admin"),
		AdminPassword:     getenv("AIPTIMIZE_ADMIN_PASSWORD"),
		SessionSecret:     getenv("AIPTIMIZE_SESSION_SECRET"),
		CookieSecure:      CookieSecureMode(strings.ToLower(env(getenv, "AIPTIMIZE_COOKIE_SECURE", string(CookieSecureAuto)))),
		StaticDir:         getenv("AIPTIMIZE_STATIC_DIR"),
		NordPoolBaseURL:   env(getenv, "AIPTIMIZE_NORDPOOL_BASE_URL", "https://dataportal-api.nordpoolgroup.com/api/DayAheadPrices"),
		OpenMeteoBaseURL:  env(getenv, "AIPTIMIZE_OPENMETEO_BASE_URL", "https://api.open-meteo.com/v1/forecast"),
		Version:           env(getenv, "AIPTIMIZE_VERSION", "dev"),
		Commit:            getenv("AIPTIMIZE_COMMIT"),
		BuildDate:         getenv("AIPTIMIZE_BUILD_DATE"),
		RegistryRepo:      env(getenv, "AIPTIMIZE_REGISTRY_REPO", "docker.io/ahmetbarac/aiptimize"),
		RegistryUsername:  getenv("AIPTIMIZE_REGISTRY_USERNAME"),
		RegistryToken:     getenv("AIPTIMIZE_REGISTRY_TOKEN"),
		UpdateRequestFile: env(getenv, "AIPTIMIZE_UPDATE_REQUEST_FILE", "/var/lib/aiptimize/update-request"),
		Zaptec: ZaptecConfig{
			Username:       getenv("AIPTIMIZE_ZAPTEC_USERNAME"),
			Password:       getenv("AIPTIMIZE_ZAPTEC_PASSWORD"),
			InstallationID: getenv("AIPTIMIZE_ZAPTEC_INSTALLATION_ID"),
			ChargerID:      getenv("AIPTIMIZE_ZAPTEC_CHARGER_ID"),
			BaseURL:        env(getenv, "AIPTIMIZE_ZAPTEC_BASE_URL", "https://api.zaptec.com"),
		},
	}

	level, err := parseLogLevel(env(getenv, "AIPTIMIZE_LOG_LEVEL", "info"))
	if err != nil {
		return nil, err
	}
	cfg.LogLevel = level

	if cfg.WriteMode, err = parseGateMode(getenv("AIPTIMIZE_ENABLE_WRITES")); err != nil {
		return nil, fmt.Errorf("AIPTIMIZE_ENABLE_WRITES: %w", err)
	}
	cfg.EnableWrites = cfg.WriteMode.Effective() == EVControlOn
	if cfg.EVControlMode, err = parseGateMode(getenv("AIPTIMIZE_ENABLE_EV_CONTROL")); err != nil {
		return nil, fmt.Errorf("AIPTIMIZE_ENABLE_EV_CONTROL: %w", err)
	}
	if cfg.Simulator, err = parseBool(getenv("AIPTIMIZE_SIMULATOR")); err != nil {
		return nil, fmt.Errorf("AIPTIMIZE_SIMULATOR: %w", err)
	}
	if cfg.Discovery, err = parseBool(env(getenv, "AIPTIMIZE_DISCOVERY", "true")); err != nil {
		return nil, fmt.Errorf("AIPTIMIZE_DISCOVERY: %w", err)
	}
	port, err := strconv.Atoi(env(getenv, "AIPTIMIZE_DISCOVERY_PORT", "8099"))
	if err != nil {
		return nil, fmt.Errorf("AIPTIMIZE_DISCOVERY_PORT: %w", err)
	}
	cfg.DiscoveryPort = port
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
	switch c.EVControlMode {
	case "", EVControlAuto, EVControlOn, EVControlOff:
	default:
		return fmt.Errorf("invalid EV control mode %q (want auto, on or off)", c.EVControlMode)
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		return fmt.Errorf("invalid log format %q (want json or text)", c.LogFormat)
	}
	if c.AdminPassword != "" && len(c.AdminPassword) < 8 {
		return errors.New("AIPTIMIZE_ADMIN_PASSWORD must be at least 8 characters")
	}
	if c.Discovery && (c.DiscoveryPort < 1 || c.DiscoveryPort > 65535) {
		return fmt.Errorf("AIPTIMIZE_DISCOVERY_PORT must be between 1 and 65535, got %d", c.DiscoveryPort)
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

// WritesEnv resolves the environment half of the inverter write gate. The
// pinned value wins; otherwise the gate follows whether the inverter answered
// recently, so configuring a working inverter is enough to allow writes.
func (c *Config) WritesEnv(healthy bool) bool {
	if c.EnableWrites {
		return true
	}
	if c.WriteMode.Effective() == EVControlOff {
		return false
	}
	return healthy
}

// parseGateMode accepts auto/on/off plus the boolean values of the original
// opt-in variables, so existing deployments keep working. Unset means auto.
func parseGateMode(v string) (EVControlMode, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "auto":
		return EVControlAuto, nil
	case "on":
		return EVControlOn, nil
	case "off":
		return EVControlOff, nil
	}
	b, err := parseBool(v)
	if err != nil {
		return EVControlAuto, fmt.Errorf("invalid value %q (want auto, true or false)", v)
	}
	if b {
		return EVControlOn, nil
	}
	return EVControlOff, nil
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
