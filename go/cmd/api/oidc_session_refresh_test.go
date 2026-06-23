package main

import (
	"strings"
	"testing"
	"time"
)

func TestLoadOIDCSessionRefreshConfigDefaults(t *testing.T) {
	t.Parallel()

	config, err := loadOIDCSessionRefreshConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadOIDCSessionRefreshConfig() error = %v", err)
	}
	if config.Interval != defaultOIDCSessionRefreshInterval {
		t.Fatalf("interval = %v, want default %v", config.Interval, defaultOIDCSessionRefreshInterval)
	}
	if config.BatchSize != defaultOIDCSessionRefreshBatchSize {
		t.Fatalf("batch size = %d, want default %d", config.BatchSize, defaultOIDCSessionRefreshBatchSize)
	}
}

func TestLoadOIDCSessionRefreshConfigOverrides(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envAuthOIDCSessionRefreshInterval:  "45s",
		envAuthOIDCSessionRefreshBatchSize: "50",
	}
	config, err := loadOIDCSessionRefreshConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadOIDCSessionRefreshConfig() error = %v", err)
	}
	if config.Interval != 45*time.Second {
		t.Fatalf("interval = %v, want 45s", config.Interval)
	}
	if config.BatchSize != 50 {
		t.Fatalf("batch size = %d, want 50", config.BatchSize)
	}
}

func TestLoadOIDCSessionRefreshConfigRejectsNonPositiveInterval(t *testing.T) {
	t.Parallel()

	_, err := loadOIDCSessionRefreshConfig(func(key string) string {
		if key == envAuthOIDCSessionRefreshInterval {
			return "0s"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), envAuthOIDCSessionRefreshInterval) {
		t.Fatalf("loadOIDCSessionRefreshConfig() error = %v, want interval validation", err)
	}
}

func TestLoadOIDCSessionRefreshConfigRejectsNonPositiveBatchSize(t *testing.T) {
	t.Parallel()

	_, err := loadOIDCSessionRefreshConfig(func(key string) string {
		if key == envAuthOIDCSessionRefreshBatchSize {
			return "-1"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), envAuthOIDCSessionRefreshBatchSize) {
		t.Fatalf("loadOIDCSessionRefreshConfig() error = %v, want batch size validation", err)
	}
}

func TestLoadOIDCSessionRefreshConfigRejectsBadDuration(t *testing.T) {
	t.Parallel()

	_, err := loadOIDCSessionRefreshConfig(func(key string) string {
		if key == envAuthOIDCSessionRefreshInterval {
			return "not-a-duration"
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadOIDCSessionRefreshConfig() with bad duration must error")
	}
}
