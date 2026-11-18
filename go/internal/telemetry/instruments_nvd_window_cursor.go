// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// NVDCursorLagTracker records the latest server-stamped cursor position per
// cursor key (the collector instance id in production). It is process-local
// by design, following SuccessorHoldTracker: it feeds the cursor-lag gauge
// and adds no Postgres read or write to the commit path. Both operands are
// server timestamps (the committed head and the cursor clamped to it), so a
// fast local clock fabricates no lag and a slow one hides none. A series
// exists only while a position is recorded; an absent series means no
// rolling window has committed in this process yet. The recording call site
// is the extension host's post-commit claim hook (#9877, #10716).
//
// Each position also carries the transport of the record that last moved its
// cursor (#10716): on the feed path the head is the feed horizon rather than
// the server clock, so the same gauge reads differently per transport and the
// label keeps the two apart.
type NVDCursorLagTracker struct {
	mu        sync.Mutex
	positions map[nvdCursorKey]nvdCursorPosition
}

type nvdCursorKey struct {
	source string
	key    string
}

type nvdCursorPosition struct {
	cursorUnixMicro int64
	headUnixMicro   int64
	transport       string
}

// NVDLagSeries identifies one cursor-lag series: the source and the closed
// transport that served the record that last moved the cursor.
type NVDLagSeries struct {
	Source    string
	Transport string
}

// NewNVDCursorLagTracker returns an empty tracker.
func NewNVDCursorLagTracker() *NVDCursorLagTracker {
	return &NVDCursorLagTracker{positions: make(map[nvdCursorKey]nvdCursorPosition)}
}

// RecordCursorPosition stores the latest server-stamped position for key. A
// nil tracker or a blank key records nothing. Positions keep the max per
// key, mirroring the planner's compare-and-advance: concurrent claims can
// record in either order, and a stale out-of-order record must not move a
// series backward. The transport follows the record that moved the cursor,
// so a stale record from the other transport cannot relabel the series. A
// negative lag still floors at zero in the gauge.
func (t *NVDCursorLagTracker) RecordCursorPosition(source, transport, key string, cursorUnixMicro, headUnixMicro int64) {
	if t == nil || key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	id := nvdCursorKey{source: source, key: key}
	current, seen := t.positions[id]
	if !seen || cursorUnixMicro > current.cursorUnixMicro {
		current.cursorUnixMicro = cursorUnixMicro
		current.transport = NVDTransport(transport)
	}
	if headUnixMicro > current.headUnixMicro {
		current.headUnixMicro = headUnixMicro
	}
	t.positions[id] = current
}

// LagSeconds returns, per series, the maximum lag in seconds across its
// recorded positions (head minus cursor, floored at zero). A nil tracker
// reports nothing.
func (t *NVDCursorLagTracker) LagSeconds() map[NVDLagSeries]float64 {
	lags := make(map[NVDLagSeries]float64)
	if t == nil {
		return lags
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, position := range t.positions {
		lag := float64(position.headUnixMicro-position.cursorUnixMicro) / 1e6
		if lag < 0 {
			lag = 0
		}
		series := NVDLagSeries{Source: id.source, Transport: position.transport}
		if current, ok := lags[series]; !ok || lag > current {
			lags[series] = lag
		}
	}
	return lags
}

// NVDFeedAgeTracker records the newest NVD 2.0 feed generation time the host
// has seen per source and reports its age at scrape time (#10716). It is
// process-local and follows NVDCursorLagTracker: no query, and a series
// exists only after a feed time was recorded in this process. The age is
// computed when the gauge is read, not when the feed was, so a feed NVD
// stopped regenerating reads older at every scrape instead of freezing at the
// last claim's value.
type NVDFeedAgeTracker struct {
	mu        sync.Mutex
	now       func() time.Time
	feedTimes map[string]int64
}

// NewNVDFeedAgeTracker returns an empty tracker reading now for ages. A nil
// now reads the wall clock; tests inject a fixed clock.
func NewNVDFeedAgeTracker(now func() time.Time) *NVDFeedAgeTracker {
	if now == nil {
		now = time.Now
	}
	return &NVDFeedAgeTracker{now: now, feedTimes: make(map[string]int64)}
}

// RecordFeedTime stores the newest feed generation time for source. A nil
// tracker, a blank source, or a non-positive time records nothing. The newest
// time wins, so a replayed or out-of-order claim cannot make the feed look
// fresher or older than the newest read. A time ahead of the tracker's clock
// is recorded as now: stored verbatim, one bogus future time would win the
// slot and the age would read zero until the clock caught up with it, hiding
// a feed that stopped regenerating. A clamped time ages from the moment it
// was read.
func (t *NVDFeedAgeTracker) RecordFeedTime(source string, feedUnixMicro int64) {
	if t == nil || source == "" || feedUnixMicro <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if now := t.now().UnixMicro(); feedUnixMicro > now {
		feedUnixMicro = now
	}
	if feedUnixMicro > t.feedTimes[source] {
		t.feedTimes[source] = feedUnixMicro
	}
}

// AgeSecondsBySource returns the age in seconds of each source's newest feed
// time (now minus the feed time). Recording already clamps a future time to
// the clock, so the zero floor only matters if the clock itself steps back. A
// nil tracker reports nothing.
func (t *NVDFeedAgeTracker) AgeSecondsBySource() map[string]float64 {
	ages := make(map[string]float64)
	if t == nil {
		return ages
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for source, feedUnixMicro := range t.feedTimes {
		age := now.Sub(time.UnixMicro(feedUnixMicro)).Seconds()
		if age < 0 {
			age = 0
		}
		ages[source] = age
	}
	return ages
}

// registerNVDCursorLag registers the NVD cursor-lag gauge and the time
// anomaly counter (#9877). The gauge callback reads process-local state
// only, so a scrape adds no query. Labels are the closed source and
// transport sets: no scope, window, CVE, or clock value ever becomes a label.
func registerNVDCursorLag(meter metric.Meter, inst *Instruments) error {
	inst.NVDCursorLags = NewNVDCursorLagTracker()
	var err error
	inst.NVDCursorLagSeconds, err = meter.Float64ObservableGauge(
		"eshu_dp_vulnerability_nvd_cursor_lag_seconds",
		metric.WithDescription("NVD rolling-window lag in seconds per source and transport (stored head minus cursor, both server-stamped; #9877). On the feed transport the head is the feed horizon, not the server clock (#10716). Rising lag with flat clock skew means the planner or claims are behind. Process-local: max across this process's recorded positions; a series exists only while a position is recorded"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			for series, lag := range inst.NVDCursorLags.LagSeconds() {
				o.Observe(lag, metric.WithAttributes(
					attribute.String(MetricDimensionSource, series.Source),
					attribute.String(MetricDimensionTransport, series.Transport),
				))
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register NVDCursorLagSeconds gauge: %w", err)
	}
	inst.NVDTimeAnomalies, err = meter.Int64Counter(
		"eshu_dp_vulnerability_nvd_time_anomalies_total",
		metric.WithDescription("Total NVD unreliable-time observations per source (#9877): unknown head, clock skew past the 5-minute budget, or record timestamps past head plus budget. The reason rides the span, never a label"),
	)
	if err != nil {
		return fmt.Errorf("register NVDTimeAnomalies counter: %w", err)
	}
	return nil
}

// registerNVDWindowTransport registers the NVD window claim instruments and
// the NVD 2.0 feed signals (eshu-hq/eshu#10716). The extension host emits all
// of them after a claim's commit or failure, because the extracted satellite
// has no telemetry handle: it stamps what happened into the snapshot payload
// or a failure class and the host counts it. Labels are the closed transport,
// outcome, and guard-class sets; never a scope, window, CVE, URL, or feed
// file name.
func registerNVDWindowTransport(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.NVDWindowClaims, err = meter.Int64Counter(
		"eshu_dp_vulnerability_nvd_window_claims_total",
		metric.WithDescription("Total NVD window claim outcomes by transport (api, feed, unknown) and outcome (committed, refused, failed_retryable, failed_terminal; #10716). Emitted by the extension host once per claim at its final outcome: a commit, a refusal, a failed attempt, a failed commit, or a failed attempt-row write. A claim that fails before the satellite runs, or is rejected for an identity mismatch, is not counted"),
	); err != nil {
		return fmt.Errorf("register NVDWindowClaims counter: %w", err)
	}
	if inst.NVDWindowCursorAdvances, err = meter.Int64Counter(
		"eshu_dp_vulnerability_nvd_window_cursor_advance_total",
		metric.WithDescription("Total committed NVD windows by transport and cursor verdict (#10716): advanced means the commit transaction armed the cursor compare-and-advance, held means the facts committed and the satellite's verdict held the cursor"),
	); err != nil {
		return fmt.Errorf("register NVDWindowCursorAdvances counter: %w", err)
	}
	if inst.NVDWindowClaimDuration, err = meter.Float64Histogram(
		"eshu_dp_vulnerability_nvd_window_claim_duration_seconds",
		metric.WithDescription("Wall time of one NVD window claim on the extension host, claim start to commit or failure, by transport and outcome (#10716)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(1, 2.5, 5, 10, 30, 60, 120, 300, 600, 900),
	); err != nil {
		return fmt.Errorf("register NVDWindowClaimDuration histogram: %w", err)
	}
	inst.NVDFeedAges = NewNVDFeedAgeTracker(nil)
	if inst.NVDFeedAgeSeconds, err = meter.Float64ObservableGauge(
		"eshu_dp_vulnerability_nvd_feed_age_seconds",
		metric.WithDescription("Age in seconds of the newest NVD 2.0 feed meta time the satellite stamped on a completed window claim, now minus that time, per source (#10716). The host records the stamp from any served window, so a feed NVD stopped regenerating reads older at every scrape instead of freezing at the last claim. A time ahead of the host clock is clamped to now. Computed at scrape time. Process-local: a series exists only after a feed time was recorded in this process"),
		metric.WithUnit("s"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			for source, age := range inst.NVDFeedAges.AgeSecondsBySource() {
				o.Observe(age, metric.WithAttributes(attribute.String(MetricDimensionSource, source)))
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("register NVDFeedAgeSeconds gauge: %w", err)
	}
	if inst.NVDFeedGuardTrips, err = meter.Int64Counter(
		"eshu_dp_vulnerability_nvd_feed_guard_trips_total",
		metric.WithDescription("Total NVD feed guard trips by closed failure class (nvd_feed_year_stale, nvd_feed_group_oversize, nvd_feed_probe_failed, other; #10716). Each trip routed its window to the API walk, so any value above zero is worth a look"),
	); err != nil {
		return fmt.Errorf("register NVDFeedGuardTrips counter: %w", err)
	}
	if inst.NVDFeedDeadlineShort, err = meter.Int64Counter(
		"eshu_dp_vulnerability_nvd_feed_deadline_short_total",
		metric.WithDescription("Total NVD window claims routed to the API walk because under 150 seconds of claim deadline remained for the feed path (#10716)"),
	); err != nil {
		return fmt.Errorf("register NVDFeedDeadlineShort counter: %w", err)
	}
	if inst.NVDFeedSHA256Mismatch, err = meter.Int64Counter(
		"eshu_dp_vulnerability_nvd_feed_sha256_mismatch_total",
		metric.WithDescription("Total NVD claims that failed retryable because the feed body served for a selected window did not match the digest in its feed meta file (#10716). A mismatch while only probing the feed counts as a probe-failed guard trip instead"),
	); err != nil {
		return fmt.Errorf("register NVDFeedSHA256Mismatch counter: %w", err)
	}
	if inst.NVDFeedMetaUnavailable, err = meter.Int64Counter(
		"eshu_dp_vulnerability_nvd_feed_meta_unavailable_total",
		metric.WithDescription("Total NVD window claims that found the feed meta file missing or failing and ran the API walk instead (#10716). A sustained rate means the feed is gone"),
	); err != nil {
		return fmt.Errorf("register NVDFeedMetaUnavailable counter: %w", err)
	}
	return nil
}
