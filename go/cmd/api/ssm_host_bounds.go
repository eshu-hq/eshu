// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
)

const (
	// ssmHostMaxCaptureAgeEnv bounds the age of an inventory capture the
	// readback may report as successful (eshu-hq/eshu#9558). Required:
	// unset or invalid answers the route 503 rather than deriving status
	// under unbounded rules.
	ssmHostMaxCaptureAgeEnv = "ESHU_SSM_HOST_MAX_CAPTURE_AGE"
	// ssmHostMaxCrossTypeSkewEnv bounds the allowed capture-time difference
	// between the platform and application evidence admitted as one host
	// view. Required alongside the capture age.
	ssmHostMaxCrossTypeSkewEnv = "ESHU_SSM_HOST_MAX_CROSS_TYPE_SKEW"
	// ssmHostMaxPlatformObservationAgeEnv bounds the age of a
	// DescribeInstanceInformation platform observation, which carries no
	// inventory capture time. Required alongside the capture age.
	ssmHostMaxPlatformObservationAgeEnv = "ESHU_SSM_HOST_MAX_PLATFORM_OBSERVATION_AGE"
)

// loadSSMHostBounds reads the fail-closed SSM host freshness configuration
// for the readback. Every bound must parse as a positive duration; a missing
// or invalid value zeroes the bounds so Bounds.Validate fails and the
// handler answers 503 rather than serving status under unbounded rules.
func loadSSMHostBounds(getenv func(string) string) ssmhost.Bounds {
	if getenv == nil {
		return ssmhost.Bounds{}
	}
	return ssmhost.Bounds{
		MaxCaptureAge:             strictSSMPositiveDuration(getenv(ssmHostMaxCaptureAgeEnv)),
		MaxCrossTypeSkew:          strictSSMPositiveDuration(getenv(ssmHostMaxCrossTypeSkewEnv)),
		MaxPlatformObservationAge: strictSSMPositiveDuration(getenv(ssmHostMaxPlatformObservationAgeEnv)),
	}
}

func strictSSMPositiveDuration(raw string) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}
