// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Defaults and bounds of the bounded command retry (#8091).
const (
	// DefaultRetryAttempts is how many times one network git command runs
	// before its failure is final: the first attempt and one retry.
	DefaultRetryAttempts = 2
	// DefaultRetryBackoff is the base wait before a retry, before jitter.
	DefaultRetryBackoff = 3 * time.Second
	// DefaultRetrySweepBudget is how many retries one sweep may start across
	// all repositories. It is the global cap that keeps a provider outage from
	// multiplying the sweep by the attempt count.
	DefaultRetrySweepBudget = 20

	retryAttemptsEnv    = "ESHU_GIT_RETRY_ATTEMPTS"
	retrySweepBudgetEnv = "ESHU_GIT_RETRY_SWEEP_BUDGET"
	// maxRetryAttempts bounds attempts times the deadline: 5 x 600 s for a clone
	// is the worst case one repository can add to a sweep.
	maxRetryAttempts    = 5
	maxRetrySweepBudget = 1000
)

func (l Limits) retryAttempts() int {
	if l.RetryAttempts > 0 {
		return l.RetryAttempts
	}
	return DefaultRetryAttempts
}

func (l Limits) retryBackoff() time.Duration {
	if l.RetryBackoff > 0 {
		return l.RetryBackoff
	}
	return DefaultRetryBackoff
}

func (l Limits) retrySweepBudget() int {
	if l.RetrySweepBudget > 0 {
		return l.RetrySweepBudget
	}
	return DefaultRetrySweepBudget
}

// retryFromEnv fills the retry fields of limits from ESHU_GIT_RETRY_ATTEMPTS
// and ESHU_GIT_RETRY_SWEEP_BUDGET. Unset or blank keeps the default. A set
// value must be a whole number from 1 to the variable's maximum; anything else
// is a startup error that names the variable and the value. 1 is the way to
// turn the retry off: attempts of one is today's single-attempt behaviour.
func retryFromEnv(getenv func(string) string, limits *Limits) error {
	attempts, err := boundedIntFromEnv(getenv, retryAttemptsEnv, DefaultRetryAttempts, maxRetryAttempts)
	if err != nil {
		return err
	}
	budget, err := boundedIntFromEnv(getenv, retrySweepBudgetEnv, DefaultRetrySweepBudget, maxRetrySweepBudget)
	if err != nil {
		return err
	}
	limits.RetryAttempts, limits.RetrySweepBudget = attempts, budget
	return nil
}

func boundedIntFromEnv(getenv func(string) string, name string, def, maxValue int) (int, error) {
	trimmed := strings.TrimSpace(getenv(name))
	if trimmed == "" {
		return def, nil
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil || value < 1 || value > maxValue {
		return 0, fmt.Errorf("%s=%q: a set value must be a whole number from 1 to %d", name, trimmed, maxValue)
	}
	return value, nil
}
