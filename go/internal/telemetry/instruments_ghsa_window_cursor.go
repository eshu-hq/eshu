// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Closed GHSA window advance outcomes (eshu-hq/eshu#10037), the outcome
// label set of eshu_dp_ghsa_window_advance_total. The extension host's
// commit policy records exactly one per GHSA window claim decision.
const (
	// GHSAWindowOutcomeAdvanced: the window proved coverage to its end
	// and the cursor writer is armed to the window end.
	GHSAWindowOutcomeAdvanced = "advanced"
	// GHSAWindowOutcomePrefix: the window proved coverage only to a
	// prefix (a budget or refusal cut) and the cursor writer is armed to
	// that prefix; the next window starts at the cut.
	GHSAWindowOutcomePrefix = "prefix"
	// GHSAWindowOutcomeHeld: the host could not arm the cursor writer
	// (no covered_through, a cache replay, or an unarmed host), so the
	// claim failed retryable and nothing committed.
	GHSAWindowOutcomeHeld = "held"
	// GHSAWindowOutcomeNoProgress: covered_through did not pass the
	// window start, so the claim failed retryable with
	// ghsa_window_no_progress and nothing committed.
	GHSAWindowOutcomeNoProgress = "no_progress"
	// GHSAWindowOutcomeOther counts an outcome outside this set.
	GHSAWindowOutcomeOther = "other"
)

var ghsaWindowOutcomes = [...]string{
	GHSAWindowOutcomeAdvanced,
	GHSAWindowOutcomePrefix,
	GHSAWindowOutcomeHeld,
	GHSAWindowOutcomeNoProgress,
	GHSAWindowOutcomeOther,
}

// GHSAWindowOutcomes returns the closed, ordered GHSA window outcome set.
func GHSAWindowOutcomes() []string {
	return slices.Clone(ghsaWindowOutcomes[:])
}

// GHSAWindowOutcome maps outcome onto the closed set.
func GHSAWindowOutcome(outcome string) string {
	if slices.Contains(ghsaWindowOutcomes[:], outcome) {
		return outcome
	}
	return GHSAWindowOutcomeOther
}

// RecordGHSAWindowAdvance counts one GHSA window commit-policy outcome.
// Nil instruments skip the metric. Labels are the closed advisory type
// and outcome only: no scope, window, or cursor value ever becomes a
// label. The caller records an armed advance's cursor position through
// GHSACursorLags so the lag gauge moves with the same decision.
func (inst *Instruments) RecordGHSAWindowAdvance(ctx context.Context, advisoryType, outcome string) {
	if inst == nil || inst.GHSAWindowAdvances == nil {
		return
	}
	inst.GHSAWindowAdvances.Add(ctx, 1, metric.WithAttributes(
		attribute.String(MetricDimensionAdvisoryType, advisoryType),
		AttrOutcome(GHSAWindowOutcome(outcome)),
	))
}

// GHSACursorLagTracker records the latest committed cursor position per
// advisory type. It is process-local by design, following
// NVDCursorLagTracker: it feeds the cursor-lag gauge and adds no Postgres
// read or write to the commit path. Lag is the scrape clock minus the
// cursor, so a stream whose claims keep failing or holding shows a
// growing value while a healthy stream stays near its refresh interval.
// A series exists only while a position is recorded; an absent series
// means no GHSA window has committed in this process yet.
type GHSACursorLagTracker struct {
	mu        sync.Mutex
	positions map[string]int64
}

// NewGHSACursorLagTracker returns an empty tracker.
func NewGHSACursorLagTracker() *GHSACursorLagTracker {
	return &GHSACursorLagTracker{positions: make(map[string]int64)}
}

// RecordCursorPosition stores the latest cursor for one advisory type. A
// nil tracker or a blank type records nothing. Positions keep the max
// per type, mirroring the store's compare-and-advance: concurrent claims
// can record in either order, and a stale out-of-order record must not
// move a series backward.
func (t *GHSACursorLagTracker) RecordCursorPosition(advisoryType string, cursorUnixMicro int64) {
	if t == nil || advisoryType == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if cursorUnixMicro > t.positions[advisoryType] {
		t.positions[advisoryType] = cursorUnixMicro
	}
}

// LagSecondsByAdvisoryType returns, per advisory type, the lag in seconds
// at now (now minus cursor, floored at zero). A nil tracker reports
// nothing.
func (t *GHSACursorLagTracker) LagSecondsByAdvisoryType(now time.Time) map[string]float64 {
	lags := make(map[string]float64)
	if t == nil {
		return lags
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for advisoryType, cursorUnixMicro := range t.positions {
		lag := float64(now.UTC().UnixMicro()-cursorUnixMicro) / 1e6
		if lag < 0 {
			lag = 0
		}
		lags[advisoryType] = lag
	}
	return lags
}

// registerGHSACursorLag registers the GHSA cursor-lag gauge and the
// window-advance counter (#10037). The gauge callback reads
// process-local state only, so a scrape adds no query. Labels stay
// advisory-type and outcome only: no scope, window, advisory, or clock
// value ever becomes a label.
func registerGHSACursorLag(meter metric.Meter, inst *Instruments) error {
	inst.GHSACursorLags = NewGHSACursorLagTracker()
	var err error
	inst.GHSACursorLagSeconds, err = meter.Float64ObservableGauge(
		"eshu_dp_ghsa_cursor_lag_seconds",
		metric.WithDescription("GHSA rolling-window lag in seconds per advisory type (scrape clock minus the latest committed covered-through cursor; #10037). A value growing past the anchor refresh interval plus one hour means the stream is stuck: check eshu_dp_ghsa_window_advance_total for held or no_progress outcomes. Process-local: a series exists only while a position is recorded in this process"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			for advisoryType, lag := range inst.GHSACursorLags.LagSecondsByAdvisoryType(time.Now().UTC()) {
				o.Observe(lag, metric.WithAttributes(attribute.String(MetricDimensionAdvisoryType, advisoryType)))
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register GHSACursorLagSeconds gauge: %w", err)
	}
	inst.GHSAWindowAdvances, err = meter.Int64Counter(
		"eshu_dp_ghsa_window_advance_total",
		metric.WithDescription("Total GHSA window commit-policy outcomes per advisory type (#10037): advanced (cursor armed to the window end), prefix (armed to a covered prefix; the next window starts at the cut), held (host could not arm the writer or a cache replay arrived; claim failed retryable, nothing committed), no_progress (covered_through did not pass the window start; claim failed retryable, nothing committed)"),
	)
	if err != nil {
		return fmt.Errorf("register GHSAWindowAdvances counter: %w", err)
	}
	return nil
}
