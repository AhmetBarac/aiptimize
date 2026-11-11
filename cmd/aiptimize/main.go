// Command aiptimize runs the battery and EV energy scheduler.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/api"
	"github.com/AhmetBarac/aiptimize/internal/auth"
	"github.com/AhmetBarac/aiptimize/internal/config"
	"github.com/AhmetBarac/aiptimize/internal/ev"
	"github.com/AhmetBarac/aiptimize/internal/prices"
	"github.com/AhmetBarac/aiptimize/internal/service"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
	"github.com/AhmetBarac/aiptimize/internal/update"
	"github.com/AhmetBarac/aiptimize/internal/webui"
)

// Injected with -ldflags at build time.
var (
	version   = "dev"
	commit    = ""
	buildDate = ""
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "serve":
		return runServe()
	case "plan":
		return notYet("battery planning", "phase 4")
	case "apply":
		return notYet("schedule actuation", "phase 5")
	case "clear":
		return notYet("schedule clearing", "phase 5")
	case "status":
		return runStatus()
	case "update-check":
		return runUpdateCheck()
	case "healthcheck":
		return runHealthcheck()
	case "version", "-v", "--version":
		fmt.Printf("aiptimize %s (commit %s, built %s, %s)\n", version, orUnknown(commit), orUnknown(buildDate), runtime.Version())
		return 0
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage(os.Stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `aiptimize — battery and EV energy scheduler

Usage:
  aiptimize serve          Run the HTTP server (default)
  aiptimize status         Print server metadata as JSON
  aiptimize update-check   Ask the registry for a newer build (JSON)
  aiptimize healthcheck    Probe /healthz (for Docker)
  aiptimize plan           Compute a dry-run plan (phase 4)
  aiptimize apply          Apply the plan to the inverter (phase 5)
  aiptimize clear          Clear the inverter schedule (phase 5)
  aiptimize version        Print version information

Configuration is read from AIPTIMIZE_* environment variables.
`)
}

func notYet(feature, phase string) int {
	fmt.Fprintf(os.Stderr, "%s is not available in this build yet (%s).\n", feature, phase)
	return 2
}

func runServe() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		return 2
	}
	if version != "dev" && cfg.Version == "dev" {
		cfg.Version = version
	}
	if cfg.Commit == "" {
		cfg.Commit = commit
	}
	if cfg.BuildDate == "" {
		cfg.BuildDate = buildDate
	}

	logger := cfg.Logger()
	if err := cfg.EnsureDataDir(); err != nil {
		logger.Error("cannot prepare data directory", "err", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabasePath)
	if err != nil {
		logger.Error("cannot open database", "err", err)
		return 1
	}
	defer st.Close()

	settingsSvc, err := settings.New(ctx, st)
	if err != nil {
		logger.Error("cannot load settings", "err", err)
		return 1
	}

	if err := seedAdmin(ctx, cfg, st, logger); err != nil {
		logger.Error("cannot seed administrator", "err", err)
		return 1
	}

	var priceFetcher prices.Fetcher
	if cfg.Simulator {
		logger.Info("using price simulator")
		priceFetcher = &prices.SimulatorFetcher{}
	} else {
		priceFetcher = prices.NewClient(cfg.NordPoolBaseURL,
			fmt.Sprintf("aiptimize/%s (+https://github.com/AhmetBarac/aiptimize)", cfg.Version))
	}
	priceSvc := prices.New(st, settingsSvc, priceFetcher, logger)

	broker := api.NewBroker()
	forecastSvc := service.NewForecastService(settingsSvc, st, logger, broker, service.ForecastOptions{
		Simulator:        cfg.Simulator,
		Version:          cfg.Version,
		OpenMeteoBaseURL: cfg.OpenMeteoBaseURL,
	})
	telemetryPoller := service.NewTelemetryPoller(settingsSvc, st, logger, broker, service.TelemetryOptions{
		Simulator: cfg.Simulator,
		Version:   cfg.Version,
	})
	solarSvc := service.NewSolarService(settingsSvc, st, forecastSvc)
	evSvc := service.NewEVService(settingsSvc, st, priceSvc, forecastSvc, logger,
		false, cfg.EnableEVControl, time.Now)
	evSettings := settingsSvc.Get().EV
	provider := strings.ToLower(strings.TrimSpace(evSettings.Provider))
	if provider == "" {
		provider = "zaptec"
	}
	switch {
	case !evSettings.Enabled:
		logger.Info("EV adapter disabled in settings")
	case cfg.Simulator || provider == "simulator":
		evSvc = service.NewEVService(settingsSvc, st, priceSvc, forecastSvc, logger,
			true, cfg.EnableEVControl, time.Now)
		logger.Info("EV adapter selected", "provider", "simulator")
	case provider == "zaptec":
		chargerID := evSettings.ChargerID
		if chargerID == "" {
			chargerID = cfg.Zaptec.ChargerID
		}
		installationID := evSettings.InstallationID
		if installationID == "" {
			installationID = cfg.Zaptec.InstallationID
		}
		username := evSettings.ZaptecUsername
		if username == "" {
			username = cfg.Zaptec.Username
		}
		password := evSettings.ZaptecPassword
		if password == "" {
			password = cfg.Zaptec.Password
		}
		charger := ev.NewZaptec(ev.ZaptecConfig{
			BaseURL: cfg.Zaptec.BaseURL, Username: username,
			Password: password, InstallationID: installationID,
			ChargerID: chargerID,
			UserAgent: "aiptimize/" + cfg.Version + " (+https://github.com/AhmetBarac/aiptimize)",
			Now:       time.Now,
		})
		evSvc = service.NewEVServiceWithCharger(settingsSvc, st, priceSvc, forecastSvc, logger,
			charger, cfg.EnableEVControl, time.Now)
		logger.Info("EV adapter selected", "provider", "zaptec", "charger_id", chargerID,
			"has_credentials", username != "" && password != "")
	default:
		logger.Warn("unsupported EV provider", "provider", provider)
	}
	plannerSvc := service.NewPlannerService(st, settingsSvc, priceSvc, forecastSvc, telemetryPoller, logger, broker.Publish)
	plannerSvc.SetEVProvider(evSvc)
	plannerSvc.SetWriteGate(cfg.EnableWrites)
	if evSvc != nil {
		evSvc.SetBatteryHold(plannerSvc)
		telemetryPoller.SetEVPowerProvider(evSvc.PowerW)
	}
	historySvc := service.NewHistoryService(st, settingsSvc, priceSvc, plannerSvc)

	// The process cannot replace its own container, so an update request is a
	// file the host's systemd path unit watches for.
	requestUpdate := func() error {
		if cfg.UpdateRequestFile == "" {
			return errors.New("no update request file configured")
		}
		if err := os.MkdirAll(filepath.Dir(cfg.UpdateRequestFile), 0o755); err != nil {
			return err
		}
		payload := []byte(time.Now().UTC().Format(time.RFC3339Nano) + "\n")
		tmp := cfg.UpdateRequestFile + ".tmp"
		if err := os.WriteFile(tmp, payload, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, cfg.UpdateRequestFile)
	}

	restartCh := make(chan struct{}, 1)
	requestRestart := func() {
		select {
		case restartCh <- struct{}{}:
		default:
		}
	}
	srv := api.New(api.Deps{
		Config:         cfg,
		Store:          st,
		Settings:       settingsSvc,
		Prices:         priceSvc,
		Telemetry:      telemetryPoller,
		Forecast:       forecastSvc,
		Solar:          solarSvc,
		Planner:        plannerSvc,
		EV:             evSvc,
		History:        historySvc,
		RequestRestart: requestRestart,
		Updates:        update.New(cfg.RegistryRepo, cfg.RegistryUsername, cfg.RegistryToken),
		RequestUpdate:  requestUpdate,
		Broker:         broker,
		Logger:         logger,
		WebFS:          webui.FS(),
	})
	go srv.RunBackground(ctx)
	go service.NewPricePoller(priceSvc, settingsSvc, logger, broker).Run(ctx)
	go telemetryPoller.Run(ctx)
	go forecastSvc.Run(ctx)
	go plannerSvc.Run(ctx)
	go plannerSvc.RunWatchdog(ctx)
	go plannerSvc.RunSlotPower(ctx)
	go evSvc.Run(ctx)

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // SSE streams must not be cut by a write deadline
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server listening",
			"addr", cfg.Listen,
			"data_dir", cfg.DataDir,
			"timezone", cfg.Timezone,
			"writes_env", cfg.EnableWrites,
			"ev_control_env", cfg.EnableEVControl,
			"simulator", cfg.Simulator,
		)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "err", err)
			return 1
		}
	case <-ctx.Done():
		logger.Info("shutdown requested")
	case <-restartCh:
		logger.Info("restart requested")
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	broker.Close()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("graceful shutdown incomplete", "err", err)
	}
	if err := st.Checkpoint(context.Background()); err != nil {
		logger.Warn("wal checkpoint failed", "err", err)
	}
	return 0
}

func seedAdmin(ctx context.Context, cfg *config.Config, st *store.Store, logger *slog.Logger) error {
	if cfg.AdminPassword == "" {
		return nil
	}
	count, err := st.UserCount(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return err
	}
	user, err := st.CreateUser(ctx, cfg.AdminUsername, hash)
	if err != nil {
		return err
	}
	if err := st.InsertEvent(ctx, store.Event{
		Severity: "info", Code: "admin_seeded",
		Message: "Administrator account seeded from the environment.",
	}); err != nil {
		logger.Warn("could not record seed event", "err", err)
	}
	logger.Info("seeded administrator from environment", "username", user.Username)
	return nil
}

func baseURL() string {
	if v := os.Getenv("AIPTIMIZE_HEALTHCHECK_URL"); v != "" {
		return v
	}
	listen := "0.0.0.0:8080"
	if cfg, err := config.Load(); err == nil {
		listen = cfg.Listen
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://127.0.0.1:8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func runHealthcheck() int {
	url := baseURL() + "/healthz"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck failed: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck failed: status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}

func runStatus() int {
	url := baseURL() + "/api/v1/meta"
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "status failed: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "status failed: %v\n", err)
		return 1
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "status failed: %s: %s\n", resp.Status, string(body))
		return 1
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		fmt.Fprintln(os.Stdout, string(body))
		return 0
	}
	out, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(out))
	return 0
}

// runUpdateCheck asks the configured registry for a newer build without
// touching the running process. Useful on the host and in support sessions.
func runUpdateCheck() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		return 2
	}
	if version != "dev" && cfg.Version == "dev" {
		cfg.Version = version
	}
	checker := update.New(cfg.RegistryRepo, cfg.RegistryUsername, cfg.RegistryToken)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	status, err := checker.Check(ctx, cfg.Version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update check failed: %v\n", err)
		return 1
	}
	out, _ := json.MarshalIndent(status, "", "  ")
	fmt.Println(string(out))
	return 0
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
