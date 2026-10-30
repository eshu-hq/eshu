// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
)

// ssmHostInventoryBounds reads the fail-closed SSM host freshness
// configuration (eshu-hq/eshu#9558), mirroring loadSSMHostBounds in
// cmd/api. Every bound must parse as a positive duration; a missing or
// invalid value zeroes the bounds so the handler answers 503 rather than
// serving status under unbounded rules.
func ssmHostInventoryBounds(getenv func(string) string) ssmhost.Bounds {
	if getenv == nil {
		return ssmhost.Bounds{}
	}
	return ssmhost.Bounds{
		MaxCaptureAge:             ssmPositiveDuration(getenv("ESHU_SSM_HOST_MAX_CAPTURE_AGE")),
		MaxCrossTypeSkew:          ssmPositiveDuration(getenv("ESHU_SSM_HOST_MAX_CROSS_TYPE_SKEW")),
		MaxPlatformObservationAge: ssmPositiveDuration(getenv("ESHU_SSM_HOST_MAX_PLATFORM_OBSERVATION_AGE")),
	}
}

func ssmPositiveDuration(raw string) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}
