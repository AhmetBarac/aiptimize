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
	"runtime"
	"syscall"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/api"
	"github.com/AhmetBarac/aiptimize/internal/auth"
	"github.com/AhmetBarac/aiptimize/internal/config"
	"github.com/AhmetBarac/aiptimize/internal/prices"
	"github.com/AhmetBarac/aiptimize/internal/service"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
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
	plannerSvc := service.NewPlannerService(st, settingsSvc, priceSvc, forecastSvc, telemetryPoller, logger, broker.Publish)

	srv := api.New(api.Deps{
		Config:    cfg,
		Store:     st,
		Settings:  settingsSvc,
		Prices:    priceSvc,
		Telemetry: telemetryPoller,
		Forecast:  forecastSvc,
		Solar:     solarSvc,
		Planner:   plannerSvc,
		Broker:    broker,
		Logger:    logger,
		WebFS:     webui.FS(),
	})
	go srv.RunBackground(ctx)
	go service.NewPricePoller(priceSvc, settingsSvc, logger, broker).Run(ctx)
	go telemetryPoller.Run(ctx)
	go forecastSvc.Run(ctx)
	go plannerSvc.Run(ctx)

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
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
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

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
