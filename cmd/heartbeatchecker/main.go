// Command heartbeatchecker is the external checker-of-checkers. It is a
// separate process from the monitor (its own container in docker-compose)
// so it stays outside the monitor's failure domain. When the monitor's
// /health heartbeat disappears it fires a webhook alert through its own
// independent configuration.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"uptimex/internal/alert"
	"uptimex/internal/heartbeatchecker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := heartbeatchecker.Config{
		MonitorURL:      envOr("MONITOR_HEALTH_URL", "http://localhost:8080/health"),
		Interval:        envDur("HC_INTERVAL", 5*time.Second),
		MissesThreshold: intEnvOr("HC_MISSES_THRESHOLD", 3),
		Timeout:         envDur("HC_TIMEOUT", 3*time.Second),
		Logger:          logger,
	}

	// Separate alert path: HC_* webhook settings, distinct from the
	// monitor's own ALERT_WEBHOOK_URL by design.
	var channels []alert.Channel
	if url := os.Getenv("HC_ALERT_WEBHOOK_URL"); url != "" {
		channels = append(channels, alert.NewWebhookChannel(alert.WebhookConfig{
			URL:      url,
			Secret:   os.Getenv("HC_ALERT_WEBHOOK_SECRET"),
			Timeout:  envDur("HC_ALERT_TIMEOUT", 5*time.Second),
			Attempts: 3,
			Backoff:  2 * time.Second,
		}))
	}
	monitorName := envOr("HC_MONITOR_NAME", "uptimex-1")

	var manager *alert.Manager
	if len(channels) > 0 {
		manager = alert.NewManager(channels, 0, logger)
	} else {
		logger.Warn("no HC_ALERT_WEBHOOK_URL configured; monitor-down events will only be logged")
	}

	cfg.OnMonitorDown = func(ctx context.Context, reason string, misses int) {
		a := heartbeatchecker.BuildDownAlert(cfg.MonitorURL, reason, misses, monitorName)
		if manager != nil {
			manager.Dispatch(ctx, a)
		} else {
			logger.Error("monitor_down", "alert", a.Message)
		}
	}
	cfg.OnMonitorRecovered = func(ctx context.Context, down time.Duration) {
		a := heartbeatchecker.BuildRecoveredAlert(cfg.MonitorURL, down)
		if manager != nil {
			manager.Dispatch(ctx, a)
		} else {
			logger.Info("monitor_recovered", "alert", a.Message)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	heartbeatchecker.New(cfg).Run(ctx)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func intEnvOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
