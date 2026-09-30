// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
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

// reconcilePolicy bounds the periodic full-snapshot reconciliation sweep. A
// scope is due for reconciliation when it has gone Interval without a projected
// full observation and no recent full attempt is still in flight or backing off
// (see decide); MaxPerCycle caps how many scopes a single selection cycle may
// force to full so a fleet does not stampede into simultaneous full snapshots.
// A zero Interval disables reconciliation entirely.
type reconcilePolicy struct {
	Interval    time.Duration
	MaxPerCycle int
}

// Reconcile decision reasons. They are a closed set used as the bounded reason
// label on the reconciliation counters and in the git_reconcile_forced log.
const (
	reconcileReasonFresh                 = "fresh"
	reconcileReasonInFlight              = "reconcile_in_flight"
	reconcileReasonInFlightExpired       = "in_flight_expired"
	reconcileReasonRetryBackoff          = "reconcile_retry_backoff"
	reconcileReasonRetryAfterUnprojected = "retry_after_unprojected"
	reconcileReasonNeverReconciled       = "never_reconciled"
	reconcileReasonIntervalElapsed       = "interval_elapsed"
	// reconcileReasonGraphDirty (#7389): a generation wrote the graph, never
	// activated, and no later activated full covers it.
	reconcileReasonGraphDirty = "graph_dirty"
)

// decide reports whether a scope in state s is due for a forced full
// reconciliation snapshot at now, and the bounded reason. It is pure: no clock
// read and no I/O.
//
// The obligation is measured on the last projected (activated) full
// generation; the throttle on the latest full attempt of any status:
//   - a projected full younger than Interval keeps the scope fresh;
//   - a pending full younger than Interval is still in flight, so forcing
//     another full of the same commit would only supersede it; at Interval it
//     stops suppressing and one new full is forced;
//   - a failed, or superseded-before-activation, full holds the sweep off for
//     Interval/4 (retry backoff), then one new full is forced.
//
// A negative age (clock skew) compares as less than every bound and
// suppresses. No single generation suppresses the sweep for longer than
// Interval: the worst case per scope is one forced full per Interval while
// fulls stay pending, and four per Interval while they keep failing.
func (p reconcilePolicy) decide(now time.Time, s scope.FullReconcileState) (bool, string) {
	if s.HasProjectedFull && now.Sub(s.LastProjectedFullAt) < p.Interval {
		return false, reconcileReasonFresh
	}
	if suppressed, reason := p.throttle(now, s); suppressed {
		return false, reason
	}
	if s.HasLatestFull && !s.LatestFullProjected {
		if s.LatestFullStatus == scope.GenerationStatusPending {
			return true, reconcileReasonInFlightExpired
		}
		return true, reconcileReasonRetryAfterUnprojected
	}
	if !s.HasProjectedFull {
		return true, reconcileReasonNeverReconciled
	}
	return true, reconcileReasonIntervalElapsed
}

// throttle is the one rule both reconcile paths share: a pending full younger
// than Interval is still in flight (reconcile_in_flight), and a failed or
// superseded-before-activation full younger than Interval/4 backs off
// (reconcile_retry_backoff). It is pure.
func (p reconcilePolicy) throttle(now time.Time, s scope.FullReconcileState) (bool, string) {
	if !s.HasLatestFull || s.LatestFullProjected {
		return false, ""
	}
	age := now.Sub(s.LatestFullAt)
	if s.LatestFullStatus == scope.GenerationStatusPending {
		if age < p.Interval {
			return true, reconcileReasonInFlight
		}
		return false, ""
	}
	if age < p.Interval/4 {
		return true, reconcileReasonRetryBackoff
	}
	return false, ""
}

// decideGraphDirty is decide for a scope with uncovered projection writers
// (#7389). Only the throttle can hold it off; a fresh full never does, because
// a full that activated before the writer wrote does not clean the graph.
func (p reconcilePolicy) decideGraphDirty(now time.Time, s scope.FullReconcileState) (bool, string) {
	if suppressed, reason := p.throttle(now, s); suppressed {
		return false, reason
	}
	return true, reconcileReasonGraphDirty
}

func (p reconcilePolicy) enabled() bool {
	return p.Interval > 0
}
