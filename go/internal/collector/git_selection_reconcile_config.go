// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strconv"
	"strings"
	"time"
)

// Default reconciliation knobs (epic #2340). Reconciliation is on by default so
// drift the delta path missed is bounded: every scope is re-observed fully at
// least once per interval, capped per cycle so a fleet does not stampede.
const (
	defaultReconcileIntervalHours = 24
	defaultReconcileMaxPerCycle   = 10
)

// reconcileIntervalFromEnv reads ESHU_REPO_RECONCILE_INTERVAL_HOURS. An explicit
// 0 disables reconciliation; an unset or invalid value uses the default. The
// value is clamped to whole hours, which is ample granularity for a safety sweep.
func reconcileIntervalFromEnv(getenv func(string) string) time.Duration {
	hours := nonNegativeIntFromEnv(getenv, "ESHU_REPO_RECONCILE_INTERVAL_HOURS", defaultReconcileIntervalHours)
	return time.Duration(hours) * time.Hour
}

// reconcileMaxPerCycleFromEnv reads ESHU_REPO_RECONCILE_MAX_PER_CYCLE. An
// explicit 0 means no per-cycle cap (still interval-gated); unset or invalid
// uses the default.
func reconcileMaxPerCycleFromEnv(getenv func(string) string) int {
	return nonNegativeIntFromEnv(getenv, "ESHU_REPO_RECONCILE_MAX_PER_CYCLE", defaultReconcileMaxPerCycle)
}

// nonNegativeIntFromEnv parses a non-negative integer env var, allowing an
// explicit 0 (unlike intFromEnv, which treats 0 as unset). Negative or
// unparseable values fall back to defaultValue.
func nonNegativeIntFromEnv(getenv func(string) string, key string, defaultValue int) int {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return defaultValue
	}
	return value
}
