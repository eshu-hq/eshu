// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"
)

// TestSSMHostInventoryBoundsParsesPositiveDurations pins the env parsing:
// every bound must be a positive duration.
func TestSSMHostInventoryBoundsParsesPositiveDurations(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "ESHU_SSM_HOST_MAX_CAPTURE_AGE":
			return "1h"
		case "ESHU_SSM_HOST_MAX_CROSS_TYPE_SKEW":
			return "30m"
		case "ESHU_SSM_HOST_MAX_PLATFORM_OBSERVATION_AGE":
			return "24h"
		}
		return ""
	}
	bounds := ssmHostInventoryBounds(getenv)
	if bounds.MaxCaptureAge != time.Hour {
		t.Errorf("MaxCaptureAge = %v, want 1h", bounds.MaxCaptureAge)
	}
	if bounds.MaxCrossTypeSkew != 30*time.Minute {
		t.Errorf("MaxCrossTypeSkew = %v, want 30m", bounds.MaxCrossTypeSkew)
	}
	if bounds.MaxPlatformObservationAge != 24*time.Hour {
		t.Errorf("MaxPlatformObservationAge = %v, want 24h", bounds.MaxPlatformObservationAge)
	}
}

// TestSSMHostInventoryBoundsFailClosed pins the fail-closed rule: missing,
// unparsable, or non-positive values zero the bounds so the handler answers
// 503 rather than deriving status unbounded.
func TestSSMHostInventoryBoundsFailClosed(t *testing.T) {
	cases := map[string]func(string) string{
		"nil getenv": nil,
		"missing":    func(string) string { return "" },
		"invalid":    func(string) string { return "not-a-duration" },
		"zero":       func(string) string { return "0s" },
		"negative":   func(string) string { return "-5m" },
	}
	for name, getenv := range cases {
		bounds := ssmHostInventoryBounds(getenv)
		if bounds.MaxCaptureAge != 0 || bounds.MaxCrossTypeSkew != 0 || bounds.MaxPlatformObservationAge != 0 {
			t.Errorf("%s: bounds = %+v, want all zero", name, bounds)
		}
	}
}
