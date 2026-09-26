// Command monitor is the distributed health & uptime monitoring service.
// It hosts the scheduling engine, bounded worker pool, REST API and the
// heartbeat endpoint consumed by the external checker-of-checkers
// (cmd/heartbeatchecker). See docs/ARCHITECTURE.md for the component map.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"uptimex/api/handlers"
	"uptimex/api/routes"
	"uptimex/internal/alert"
	"uptimex/internal/checker"
	"uptimex/internal/config"
	"uptimex/internal/logging"
	"uptimex/internal/metrics"
	"uptimex/internal/models"
	"uptimex/internal/monitor"
	"uptimex/internal/storage"
)

func main() {
	if err := run(); err != nil {
		os.Stderr.WriteString("monitor: fatal: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := logging.New(cfg.Env)
	startedAt := time.Now().UTC()
	logger.Info("starting", "service", "uptimex", "env", cfg.Env, "db_driver", cfg.DB.Driver)

	// Storage (PostgreSQL production / SQLite development).
	store, err := storage.Open(context.Background(), storage.Options{
		Driver:          string(cfg.DB.Driver),
		PostgresDSN:     cfg.DB.PostgresDSN,
		SQLitePath:      cfg.DB.SQLitePath,
		MaxOpenConns:    cfg.DB.MaxOpenConns,
		MaxIdleConns:    cfg.DB.MaxIdleConns,
		ConnMaxLifetime: cfg.DB.ConnMaxLifetime,
	})
	if err != nil {
		return err
	}
	defer store.Close()
	logger.Info("storage_ready", "driver", cfg.DB.Driver)

	// Alerting channels.
	var channels []alert.Channel
	if cfg.Alerts.WebhookURL != "" {
		channels = append(channels, alert.NewWebhookChannel(alert.WebhookConfig{
			URL:      cfg.Alerts.WebhookURL,
			Secret:   cfg.Alerts.WebhookSecret,
			Timeout:  cfg.Alerts.WebhookTimeout,
			Attempts: cfg.Alerts.RetryAttempts,
			Backoff:  cfg.Alerts.RetryBackoff,
		}))
	}
	if cfg.Alerts.SMTPHost != "" {
		channels = append(channels, alert.NewEmailChannel(alert.EmailConfig{
			Host:     cfg.Alerts.SMTPHost,
			Port:     cfg.Alerts.SMTPPort,
			Username: cfg.Alerts.SMTPUsername,
			Password: cfg.Alerts.SMTPPassword,
			From:     cfg.Alerts.SMTPFrom,
			To:       cfg.Alerts.SMTPTo,
		}))
	}
	alerts := alert.NewManager(channels, cfg.Alerts.Cooldown, logger)
	if len(channels) == 0 {
		logger.Info("alerting_disabled", "reason", "no channels configured")
	}

	// Monitoring engine.
	engine := monitor.New(store, checker.NewHTTPChecker(checker.NewGuard(cfg.Security.AllowPrivateTargets)),
		alerts, monitor.Config{
			WorkerCount:   cfg.WorkerCount,
			JobQueueSize:  cfg.JobQueueSize,
			SchedulerTick: cfg.SchedulerTick,
			HighLatencyMs: cfg.Alerts.HighLatencyMs,
		}, logger)

	if cfg.SeedDemo {
		seedDemoEndpoints(logger, store, cfg)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		return err
	}

	// HTTP surface.
	metricsSvc := &metrics.Service{Repo: store}
	root := routes.New(routes.Deps{
		Logger:    logger,
		APIConfig: &cfg.API,
		Health: handlers.NewHealthHandler(startedAt, cfg.MonitorName, func() error {
			pingCtx, pingCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer pingCancel()
			return store.Ping(pingCtx)
		}),
		Endpoints: &handlers.EndpointHandler{
			Repo:    store,
			Engine:  engine,
			Guard:   checker.NewGuard(cfg.Security.AllowPrivateTargets),
			Metrics: metricsSvc,
			Logger:  logger,
		},
		Metrics: &handlers.MetricsHandler{
			Metrics:     metricsSvc,
			Repo:        store,
			EngineStats: func() any { return engine.Stats() },
			Logger:      logger,
		},
		Incidents: &handlers.IncidentHandler{Repo: store, Logger: logger},
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http_server_started", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stop:
		logger.Info("shutdown_signal_received", "signal", sig.String())
	case err := <-errCh:
		return err
	}

	// Graceful shutdown: stop accepting HTTP, drain the engine pipeline,
	// then release the database.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.API.ShutdownTimeout)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http_shutdown_error", "error", err)
	}
	engine.Stop()
	logger.Info("shutdown_complete")
	return nil
}

// seedDemoEndpoints registers a starter endpoint on first boot so
// `docker compose up` shows a working dashboard immediately.
func seedDemoEndpoints(logger *slog.Logger, store storage.Repository, cfg *config.Config) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	count, err := store.CountEndpoints(ctx)
	if err != nil {
		logger.Warn("seed_count_failed", "error", err)
		return
	}
	if count > 0 {
		return
	}
	selfURL := os.Getenv("SEED_SELF_URL")
	if selfURL == "" {
		selfURL = "http://localhost:8080/health"
	}
	demo := []models.Endpoint{
		{Name: "Monitor Self (heartbeat)", URL: selfURL, Method: "GET", IntervalSeconds: 15,
			TimeoutMs: 5000, FailureThreshold: 3, ExpectedStatusMin: 200, ExpectedStatusMax: 299, Enabled: true},
	}
	for _, e := range demo {
		if err := store.CreateEndpoint(ctx, &e); err != nil {
			logger.Warn("seed_create_failed", "name", e.Name, "error", err)
			continue
		}
		logger.Info("seeded_demo_endpoint", "id", e.ID, "name", e.Name, "url", e.URL)
	}
}
