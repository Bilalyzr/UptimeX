// Package config loads and validates runtime configuration from environment
// variables. Secrets never have production defaults; they must be injected via
// the environment or a secret manager (PRD §26).
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// DBDriver names a supported storage backend.
type DBDriver string

const (
	DriverPostgres DBDriver = "postgres"
	DriverSQLite   DBDriver = "sqlite"
)

// DBConfig configures the storage layer.
type DBConfig struct {
	Driver          DBDriver
	PostgresDSN     string
	SQLitePath      string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// AlertConfig configures the alert dispatcher.
type AlertConfig struct {
	WebhookURL     string
	WebhookSecret  string // optional HMAC-SHA256 signing secret
	WebhookTimeout time.Duration
	SMTPHost       string // optional email channel
	SMTPPort       int
	SMTPUsername   string
	SMTPPassword   string
	SMTPFrom       string
	SMTPTo         string
	Cooldown       time.Duration // per endpoint+type dedup window
	HighLatencyMs  int64         // 0 disables HIGH_LATENCY alerts
	RetryAttempts  int
	RetryBackoff   time.Duration
}

// APIConfig configures the REST API surface.
type APIConfig struct {
	APIKey            string        // when set, mutating routes require X-API-Key
	RateLimitRPS      float64       // token bucket refill rate for admin APIs
	RateLimitBurst    int           // token bucket capacity
	CORSAllowedOrigin string        // dashboard origin permitted to call the API
	ShutdownTimeout   time.Duration // graceful HTTP shutdown budget
	// SAASMode enables the multi-tenant surface: signup/login sessions,
	// per-org data scoping, plan quotas and public status pages. Anonymous
	// requests are rejected except on auth/public endpoints.
	SAASMode      bool
	SessionTTL    time.Duration // login session lifetime
	SecureCookies bool          // set the Secure flag on session cookies (HTTPS)
}

// SecurityConfig controls outbound probing safeguards (SSRF mitigation).
type SecurityConfig struct {
	// AllowPrivateTargets permits probing loopback/private/link-local and
	// cloud-metadata targets. Intended for local development and self-hosted
	// internal monitoring only; must stay false in multi-tenant production.
	AllowPrivateTargets bool
}

// Config is the fully validated runtime configuration.
type Config struct {
	Env           string // development | production
	HTTPAddr      string
	DB            DBConfig
	WorkerCount   int
	SchedulerTick time.Duration
	JobQueueSize  int
	Alerts        AlertConfig
	API           APIConfig
	Security      SecurityConfig
	SeedDemo      bool // register demo endpoints on first boot when DB is empty
	MonitorName   string
}

// Load reads the environment and returns a validated configuration.
func Load() (*Config, error) {
	cfg := &Config{
		Env:      strings.ToLower(envStr("APP_ENV", "development")),
		HTTPAddr: envStr("HTTP_ADDR", ":8080"),
		DB: DBConfig{
			Driver:          DBDriver(strings.ToLower(envStr("DB_DRIVER", string(DriverPostgres)))),
			PostgresDSN:     envStr("POSTGRES_DSN", "postgres://monitor:monitor@localhost:5432/healthmonitor?sslmode=disable"),
			SQLitePath:      envStr("SQLITE_PATH", "./healthmonitor.db"),
			MaxOpenConns:    envInt("DB_MAX_OPEN_CONNS", 20),
			MaxIdleConns:    envInt("DB_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: envDur("DB_CONN_MAX_LIFETIME", 30*time.Minute),
		},
		WorkerCount:   envInt("WORKER_COUNT", 20),
		SchedulerTick: envDur("SCHEDULER_TICK", 1*time.Second),
		JobQueueSize:  envInt("JOB_QUEUE_SIZE", 512),
		Alerts: AlertConfig{
			WebhookURL:     envStr("ALERT_WEBHOOK_URL", ""),
			WebhookSecret:  envStr("ALERT_WEBHOOK_SECRET", ""),
			WebhookTimeout: envDur("ALERT_WEBHOOK_TIMEOUT", 5*time.Second),
			SMTPHost:       envStr("SMTP_HOST", ""),
			SMTPPort:       envInt("SMTP_PORT", 587),
			SMTPUsername:   envStr("SMTP_USERNAME", ""),
			SMTPPassword:   envStr("SMTP_PASSWORD", ""),
			SMTPFrom:       envStr("SMTP_FROM", ""),
			SMTPTo:         envStr("SMTP_TO", ""),
			Cooldown:       envDur("ALERT_COOLDOWN", 5*time.Minute),
			HighLatencyMs:  int64(envInt("ALERT_HIGH_LATENCY_MS", 0)),
			RetryAttempts:  envInt("ALERT_RETRY_ATTEMPTS", 2),
			RetryBackoff:   envDur("ALERT_RETRY_BACKOFF", 2*time.Second),
		},
		API: APIConfig{
			APIKey:            envStr("API_KEY", ""),
			RateLimitRPS:      envFloat("RATE_LIMIT_RPS", 20),
			RateLimitBurst:    envInt("RATE_LIMIT_BURST", 40),
			CORSAllowedOrigin: envStr("CORS_ALLOWED_ORIGIN", ""),
			ShutdownTimeout:   envDur("API_SHUTDOWN_TIMEOUT", 15*time.Second),
			SAASMode:          envBool("SAAS_MODE", false),
			SessionTTL:        envDur("SESSION_TTL", 7*24*time.Hour),
			SecureCookies:     envBool("SESSION_COOKIE_SECURE", false),
		},
		Security: SecurityConfig{
			AllowPrivateTargets: envBool("ALLOW_PRIVATE_TARGETS", false),
		},
		SeedDemo:    envBool("SEED_DEMO_ENDPOINTS", false),
		MonitorName: envStr("MONITOR_NAME", "uptimex-1"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate enforces production-safe bounds.
func (c *Config) Validate() error {
	var errs []error
	switch c.Env {
	case "development", "production", "test":
	default:
		errs = append(errs, fmt.Errorf("APP_ENV must be development|production|test, got %q", c.Env))
	}
	switch c.DB.Driver {
	case DriverPostgres, DriverSQLite:
	default:
		errs = append(errs, fmt.Errorf("DB_DRIVER must be postgres or sqlite, got %q", c.DB.Driver))
	}
	if c.WorkerCount < 1 || c.WorkerCount > 1024 {
		errs = append(errs, fmt.Errorf("WORKER_COUNT must be between 1 and 1024, got %d", c.WorkerCount))
	}
	if c.SchedulerTick < 100*time.Millisecond || c.SchedulerTick > time.Minute {
		errs = append(errs, fmt.Errorf("SCHEDULER_TICK must be between 100ms and 1m, got %s", c.SchedulerTick))
	}
	if c.JobQueueSize < 1 {
		errs = append(errs, fmt.Errorf("JOB_QUEUE_SIZE must be >= 1, got %d", c.JobQueueSize))
	}
	if c.DB.MaxOpenConns < 1 || c.DB.MaxIdleConns < 1 {
		errs = append(errs, errors.New("DB_MAX_OPEN_CONNS and DB_MAX_IDLE_CONNS must be >= 1"))
	}
	if c.DB.MaxIdleConns > c.DB.MaxOpenConns {
		errs = append(errs, errors.New("DB_MAX_IDLE_CONNS cannot exceed DB_MAX_OPEN_CONNS"))
	}
	if c.API.RateLimitRPS <= 0 || c.API.RateLimitBurst < 1 {
		errs = append(errs, errors.New("RATE_LIMIT_RPS must be > 0 and RATE_LIMIT_BURST >= 1"))
	}
	if c.Alerts.HighLatencyMs < 0 {
		errs = append(errs, errors.New("ALERT_HIGH_LATENCY_MS cannot be negative"))
	}
	return errors.Join(errs...)
}

func envStr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envInt(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return n
}

func envFloat(key string, def float64) float64 {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return def
	}
	return f
}

func envDur(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return d
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return b
}
