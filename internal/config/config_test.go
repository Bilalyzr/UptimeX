package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DB_DRIVER", "postgres")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.WorkerCount != 20 {
		t.Errorf("WorkerCount = %d, want 20", cfg.WorkerCount)
	}
	if cfg.SchedulerTick != time.Second {
		t.Errorf("SchedulerTick = %s, want 1s", cfg.SchedulerTick)
	}
	if cfg.Security.AllowPrivateTargets {
		t.Error("AllowPrivateTargets must default to false (SSRF protection)")
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.WorkerCount = 0
	cfg.DB.Driver = "mysql"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() should reject worker count 0 and unknown driver")
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("WORKER_COUNT", "64")
	t.Setenv("ALERT_COOLDOWN", "30s")
	t.Setenv("DB_DRIVER", "sqlite")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.WorkerCount != 64 {
		t.Errorf("WorkerCount = %d, want 64", cfg.WorkerCount)
	}
	if cfg.Alerts.Cooldown != 30*time.Second {
		t.Errorf("Cooldown = %s, want 30s", cfg.Alerts.Cooldown)
	}
	if cfg.DB.Driver != DriverSQLite {
		t.Errorf("Driver = %s, want sqlite", cfg.DB.Driver)
	}
}
