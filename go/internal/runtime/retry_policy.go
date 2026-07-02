// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	defaultRetryMaxAttempts       = 3
	defaultRetryDelay             = 30 * time.Second
	defaultRetryMaxDelay          = time.Hour
	defaultRetryJitterFraction    = 0.1
	maxRetryJitterFractionAllowed = 1.0
)

// RetryPolicyConfig captures bounded retry settings for one runtime stage.
//
// RetryDelay is the base delay; the actual per-attempt delay grows
// exponentially with the durable attempt count (baseDelay*(1<<attempt)),
// capped at MaxRetryDelay, plus a uniform random jitter term drawn from
// [0, RetryDelay*JitterFraction). Fixed, jitter-free delays let many
// work items that fail at the same instant reconverge on the identical
// visible_at and self-reinforce into a retry storm that starves new work
// (#4450); the exponential term and jitter both exist to break that
// synchronization.
type RetryPolicyConfig struct {
	MaxAttempts int
	RetryDelay  time.Duration
	// MaxRetryDelay caps the exponential backoff term so a high attempt
	// count cannot grow the delay unboundedly. Zero/unset falls back to
	// defaultRetryMaxDelay (1 hour).
	MaxRetryDelay time.Duration
	// JitterFraction scales the random component added on top of the
	// exponential term, relative to RetryDelay. A value of 0 disables
	// jitter entirely (deterministic legacy behavior); the default 0.1
	// matches the formula in issue #4450: rand(0, baseDelay*0.1).
	JitterFraction float64
}

// LoadRetryPolicyConfig reads a bounded retry policy using the supplied stage
// prefix, for example PROJECTOR or REDUCER.
func LoadRetryPolicyConfig(getenv func(string) string, stagePrefix string) (RetryPolicyConfig, error) {
	stagePrefix = strings.TrimSpace(stagePrefix)
	if stagePrefix == "" {
		return RetryPolicyConfig{}, fmt.Errorf("retry policy stage prefix is required")
	}

	maxAttempts, err := intEnvOrDefault(
		getenv,
		fmt.Sprintf("ESHU_%s_MAX_ATTEMPTS", stagePrefix),
		defaultRetryMaxAttempts,
	)
	if err != nil {
		return RetryPolicyConfig{}, err
	}
	retryDelay, err := durationEnvOrDefault(
		getenv,
		fmt.Sprintf("ESHU_%s_RETRY_DELAY", stagePrefix),
		defaultRetryDelay,
	)
	if err != nil {
		return RetryPolicyConfig{}, err
	}
	maxRetryDelay, err := durationEnvOrDefault(
		getenv,
		fmt.Sprintf("ESHU_%s_MAX_RETRY_DELAY", stagePrefix),
		defaultRetryMaxDelay,
	)
	if err != nil {
		return RetryPolicyConfig{}, err
	}
	jitterFraction, err := floatEnvOrDefault(
		getenv,
		fmt.Sprintf("ESHU_%s_RETRY_JITTER_FRACTION", stagePrefix),
		defaultRetryJitterFraction,
	)
	if err != nil {
		return RetryPolicyConfig{}, err
	}
	if maxAttempts <= 0 {
		return RetryPolicyConfig{}, fmt.Errorf("ESHU_%s_MAX_ATTEMPTS must be positive", stagePrefix)
	}
	if retryDelay <= 0 {
		return RetryPolicyConfig{}, fmt.Errorf("ESHU_%s_RETRY_DELAY must be positive", stagePrefix)
	}
	if maxRetryDelay <= 0 {
		return RetryPolicyConfig{}, fmt.Errorf("ESHU_%s_MAX_RETRY_DELAY must be positive", stagePrefix)
	}
	if math.IsNaN(jitterFraction) {
		return RetryPolicyConfig{}, fmt.Errorf(
			"ESHU_%s_RETRY_JITTER_FRACTION must not be NaN",
			stagePrefix,
		)
	}
	if jitterFraction < 0 || jitterFraction > maxRetryJitterFractionAllowed {
		return RetryPolicyConfig{}, fmt.Errorf(
			"ESHU_%s_RETRY_JITTER_FRACTION must be within [0, %v]",
			stagePrefix,
			maxRetryJitterFractionAllowed,
		)
	}

	return RetryPolicyConfig{
		MaxAttempts:    maxAttempts,
		RetryDelay:     retryDelay,
		MaxRetryDelay:  maxRetryDelay,
		JitterFraction: jitterFraction,
	}, nil
}
