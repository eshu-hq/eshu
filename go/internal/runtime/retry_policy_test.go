package runtime

import (
	"testing"
	"time"
)

func TestLoadRetryPolicyConfigUsesDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := LoadRetryPolicyConfig(func(string) string { return "" }, "PROJECTOR")
	if err != nil {
		t.Fatalf("LoadRetryPolicyConfig() error = %v, want nil", err)
	}
	if got, want := cfg.MaxAttempts, defaultRetryMaxAttempts; got != want {
		t.Fatalf("MaxAttempts = %d, want %d", got, want)
	}
	if got, want := cfg.RetryDelay, defaultRetryDelay; got != want {
		t.Fatalf("RetryDelay = %v, want %v", got, want)
	}
}

func TestLoadRetryPolicyConfigReadsOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := LoadRetryPolicyConfig(func(key string) string {
		switch key {
		case "ESHU_PROJECTOR_MAX_ATTEMPTS":
			return "5"
		case "ESHU_PROJECTOR_RETRY_DELAY":
			return "45s"
		default:
			return ""
		}
	}, "PROJECTOR")
	if err != nil {
		t.Fatalf("LoadRetryPolicyConfig() error = %v, want nil", err)
	}
	if got, want := cfg.MaxAttempts, 5; got != want {
		t.Fatalf("MaxAttempts = %d, want %d", got, want)
	}
	if got, want := cfg.RetryDelay, 45*time.Second; got != want {
		t.Fatalf("RetryDelay = %v, want %v", got, want)
	}
}

func TestLoadRetryPolicyConfigRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	if _, err := LoadRetryPolicyConfig(func(key string) string {
		if key == "ESHU_PROJECTOR_MAX_ATTEMPTS" {
			return "0"
		}
		return ""
	}, "PROJECTOR"); err == nil {
		t.Fatal("LoadRetryPolicyConfig() max attempts error = nil, want non-nil")
	}

	if _, err := LoadRetryPolicyConfig(func(key string) string {
		if key == "ESHU_PROJECTOR_RETRY_DELAY" {
			return "not-a-duration"
		}
		return ""
	}, "PROJECTOR"); err == nil {
		t.Fatal("LoadRetryPolicyConfig() retry delay error = nil, want non-nil")
	}
}
