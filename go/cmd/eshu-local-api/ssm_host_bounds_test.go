// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"
)

func TestLoadSSMHostBoundsRequiresEveryBound(t *testing.T) {
	t.Parallel()

	full := map[string]string{
		ssmHostMaxCaptureAgeEnv:             "2h",
		ssmHostMaxCrossTypeSkewEnv:          "10m",
		ssmHostMaxPlatformObservationAgeEnv: "24h",
	}
	bounds := loadSSMHostBounds(func(key string) string { return full[key] })
	if err := bounds.Validate(); err != nil {
		t.Fatalf("full env Bounds.Validate() = %v, want nil", err)
	}
	if bounds.MaxCaptureAge != 2*time.Hour {
		t.Fatalf("MaxCaptureAge = %v, want 2h", bounds.MaxCaptureAge)
	}

	for name, env := range map[string]map[string]string{
		"missing age": {
			ssmHostMaxCrossTypeSkewEnv:          "10m",
			ssmHostMaxPlatformObservationAgeEnv: "24h",
		},
		"invalid duration": {
			ssmHostMaxCaptureAgeEnv:             "yesterday",
			ssmHostMaxCrossTypeSkewEnv:          "10m",
			ssmHostMaxPlatformObservationAgeEnv: "24h",
		},
	} {
		if err := loadSSMHostBounds(func(key string) string { return env[key] }).Validate(); err == nil {
			t.Fatalf("%s: Bounds.Validate() = nil, want a fail-closed error", name)
		}
	}
	if err := loadSSMHostBounds(nil).Validate(); err == nil {
		t.Fatalf("nil getenv: Bounds.Validate() = nil, want a fail-closed error")
	}
}
