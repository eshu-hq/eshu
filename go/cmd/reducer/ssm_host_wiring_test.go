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
	if bounds.MaxCaptureAge != 2*time.Hour || bounds.MaxCrossTypeSkew != 10*time.Minute || bounds.MaxPlatformObservationAge != 24*time.Hour {
		t.Fatalf("bounds = %+v, want the configured durations", bounds)
	}

	for name, env := range map[string]map[string]string{
		"missing age": {
			ssmHostMaxCrossTypeSkewEnv:          "10m",
			ssmHostMaxPlatformObservationAgeEnv: "24h",
		},
		"missing skew": {
			ssmHostMaxCaptureAgeEnv:             "2h",
			ssmHostMaxPlatformObservationAgeEnv: "24h",
		},
		"missing observation age": {
			ssmHostMaxCaptureAgeEnv:    "2h",
			ssmHostMaxCrossTypeSkewEnv: "10m",
		},
		"invalid duration": {
			ssmHostMaxCaptureAgeEnv:             "yesterday",
			ssmHostMaxCrossTypeSkewEnv:          "10m",
			ssmHostMaxPlatformObservationAgeEnv: "24h",
		},
		"non-positive duration": {
			ssmHostMaxCaptureAgeEnv:             "0s",
			ssmHostMaxCrossTypeSkewEnv:          "10m",
			ssmHostMaxPlatformObservationAgeEnv: "24h",
		},
		"nil getenv": nil,
	} {
		bounds := loadSSMHostBounds(func(key string) string { return env[key] })
		if name == "nil getenv" {
			bounds = loadSSMHostBounds(nil)
		}
		if err := bounds.Validate(); err == nil {
			t.Fatalf("%s: Bounds.Validate() = nil, want a fail-closed error", name)
		}
	}
}
