// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	if got, want := cfg.MaxRetryDelay, defaultRetryMaxDelay; got != want {
		t.Fatalf("MaxRetryDelay = %v, want %v", got, want)
	}
	if got, want := cfg.JitterFraction, defaultRetryJitterFraction; got != want {
		t.Fatalf("JitterFraction = %v, want %v", got, want)
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
		case "ESHU_PROJECTOR_MAX_RETRY_DELAY":
			return "10m"
		case "ESHU_PROJECTOR_RETRY_JITTER_FRACTION":
			return "0.25"
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
	if got, want := cfg.MaxRetryDelay, 10*time.Minute; got != want {
		t.Fatalf("MaxRetryDelay = %v, want %v", got, want)
	}
	if got, want := cfg.JitterFraction, 0.25; got != want {
		t.Fatalf("JitterFraction = %v, want %v", got, want)
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

	if _, err := LoadRetryPolicyConfig(func(key string) string {
		if key == "ESHU_PROJECTOR_MAX_RETRY_DELAY" {
			return "0s"
		}
		return ""
	}, "PROJECTOR"); err == nil {
		t.Fatal("LoadRetryPolicyConfig() max retry delay error = nil, want non-nil")
	}

	if _, err := LoadRetryPolicyConfig(func(key string) string {
		if key == "ESHU_PROJECTOR_RETRY_JITTER_FRACTION" {
			return "1.5"
		}
		return ""
	}, "PROJECTOR"); err == nil {
		t.Fatal("LoadRetryPolicyConfig() jitter fraction > 1 error = nil, want non-nil")
	}

	if _, err := LoadRetryPolicyConfig(func(key string) string {
		if key == "ESHU_PROJECTOR_RETRY_JITTER_FRACTION" {
			return "-0.1"
		}
		return ""
	}, "PROJECTOR"); err == nil {
		t.Fatal("LoadRetryPolicyConfig() negative jitter fraction error = nil, want non-nil")
	}

	if _, err := LoadRetryPolicyConfig(func(key string) string {
		if key == "ESHU_PROJECTOR_RETRY_JITTER_FRACTION" {
			return "not-a-float"
		}
		return ""
	}, "PROJECTOR"); err == nil {
		t.Fatal("LoadRetryPolicyConfig() unparseable jitter fraction error = nil, want non-nil")
	}

	// NaN passes strconv.ParseFloat (it is a valid IEEE 754 string form), and
	// both `jitterFraction < 0` and `jitterFraction > max` evaluate false for
	// NaN, so the plain range check alone would silently accept it and let
	// computeRetryDelay's `jitterFraction > 0` gate also evaluate false,
	// disabling jitter without any startup error. Reject it explicitly.
	if _, err := LoadRetryPolicyConfig(func(key string) string {
		if key == "ESHU_PROJECTOR_RETRY_JITTER_FRACTION" {
			return "NaN"
		}
		return ""
	}, "PROJECTOR"); err == nil {
		t.Fatal("LoadRetryPolicyConfig() NaN jitter fraction error = nil, want non-nil")
	}
}
