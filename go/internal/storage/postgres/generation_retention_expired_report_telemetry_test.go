// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The #7585 PR3 boundary proofs for the expired-generations live signals:
// the report must count expired-but-blocked history by closed reason,
// time its own read, fail through the shared retention failure signal,
// and keep scope/generation identifiers out of metric labels (they ride
// span events and structured logs only).

// newExpiredReportTestInstruments builds instruments over a manual reader
// so report tests can assert the exact counted reason.
func newExpiredReportTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

// collectExpiredReportMetrics gathers the manual reader's current samples.
func collectExpiredReportMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return rm
}

// expiredBlockedReasonValue reads one reason's count from
// eshu_dp_generation_retention_expired_blocked_total, 0 when the reason
// was never counted.
func expiredBlockedReasonValue(t *testing.T, rm metricdata.ResourceMetrics, reason string) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_generation_retention_expired_blocked_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("expired blocked counter data type = %T, want Int64 sum", m.Data)
			}
			for _, point := range sum.DataPoints {
				if value, ok := point.Attributes.Value("reason"); ok && value.AsString() == reason {
					return point.Value
				}
			}
		}
	}
	return 0
}

// expiredReportDurationSamples returns the (count, sum) of
// eshu_dp_generation_retention_expired_report_duration_seconds samples.
func expiredReportDurationSamples(t *testing.T, rm metricdata.ResourceMetrics) (uint64, float64) {
	t.Helper()
	var count uint64
	var sum float64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_generation_retention_expired_report_duration_seconds" {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("expired report duration data type = %T, want float64 histogram", m.Data)
			}
			for _, point := range histogram.DataPoints {
				count += point.Count
				sum += point.Sum
			}
		}
	}
	return count, sum
}

// expiredReportFailureValue reads the shared retention failure counter's
// expired_report_error series, 0 when no report failure was counted.
func expiredReportFailureValue(t *testing.T, rm metricdata.ResourceMetrics) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_generation_retention_failures_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("retention failures data type = %T, want Int64 sum", m.Data)
			}
			for _, point := range sum.DataPoints {
				if value, ok := point.Attributes.Value("reason"); ok && value.AsString() == "expired_report_error" {
					return point.Value
				}
			}
		}
	}
	return 0
}

// assertNoReportIdentityLabels fails when any report metric sample carries
// a scope or generation identifier: identifiers stay in span events and
// logs, never in metric labels.
func assertNoReportIdentityLabels(t *testing.T, rm metricdata.ResourceMetrics) {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_generation_retention_expired_blocked_total" &&
				m.Name != "eshu_dp_generation_retention_expired_report_duration_seconds" &&
				m.Name != "eshu_dp_generation_retention_failures_total" {
				continue
			}
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, point := range data.DataPoints {
					for _, key := range []attribute.Key{"scope_id", "generation_id"} {
						if value, ok := point.Attributes.Value(key); ok {
							t.Fatalf("%s carries %s=%q, want no identity labels", m.Name, key, value.AsString())
						}
					}
				}
			case metricdata.Histogram[float64]:
				for _, point := range data.DataPoints {
					for _, key := range []attribute.Key{"scope_id", "generation_id"} {
						if value, ok := point.Attributes.Value(key); ok {
							t.Fatalf("%s carries %s=%q, want no identity labels", m.Name, key, value.AsString())
						}
					}
				}
			}
		}
	}
}

// spanEventAttr reads one span event attribute, "" when absent. Span event
// attributes are a slice, not a set, so this walks the pairs.
func spanEventAttr(attrs []attribute.KeyValue, key attribute.Key) string {
	for _, attr := range attrs {
		if attr.Key == key {
			return attr.Value.AsString()
		}
	}
	return ""
}

// expiredReportFixtureRows builds one report fixture per blocked class plus
// one eligible and one scoped-out row, all past the ceiling.
func expiredReportFixtureRows(now time.Time) [][]any {
	century := 100 * 24 * time.Hour
	return [][]any{
		expiredBlockedFixtureRow(now, "gen-live-1", century, false, true, false, false, false),
		expiredBlockedFixtureRow(now, "gen-live-2", century, false, true, false, false, false),
		expiredBlockedFixtureRow(now, "gen-retrying", century, false, false, true, false, false),
		expiredBlockedFixtureRow(now, "gen-active", century, true, false, false, false, false),
		expiredBlockedFixtureRow(now, "gen-eligible", century, false, false, false, false, false),
		expiredBlockedFixtureRow(now, "gen-scoped-out", century, false, true, false, true, false),
	}
}

// TestSelectExpiredGenerationsRecordsBlockedCountsByReason proves the
// live-signal report counts expired-but-blocked history on the
// expired-blocked counter with the closed blocked-reason vocabulary.
func TestSelectExpiredGenerationsRecordsBlockedCountsByReason(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	inst, reader := newExpiredReportTestInstruments(t)
	database := &generationRetentionFakeDB{expiredBlockedRows: expiredReportFixtureRows(now)}
	store := NewGenerationRetentionStore(database).WithInstruments(inst)
	store.Now = func() time.Time { return now }

	if _, err := store.SelectExpiredGenerations(context.Background(), expiredBlockedTestPolicy()); err != nil {
		t.Fatalf("SelectExpiredGenerations() error = %v", err)
	}
	rm := collectExpiredReportMetrics(t, reader)
	for _, tc := range []struct {
		reason string
		want   int64
	}{
		{string(GenerationRetentionBlockedLiveWork), 2},
		{string(GenerationRetentionBlockedRetrying), 1},
		{string(GenerationRetentionBlockedActiveCurrent), 1},
		{string(GenerationRetentionBlockedDependencyPin), 0},
	} {
		if got := expiredBlockedReasonValue(t, rm, tc.reason); got != tc.want {
			t.Errorf("reason %q = %d, want %d", tc.reason, got, tc.want)
		}
	}
}

// TestSelectExpiredGenerationsEligibleAndScopedOutEmitNoBlockedSample
// proves only expired-but-blocked rows move the counter: eligible history
// and scoped-out (uncovered/delta-protected) rows, even with coincidental
// live-work signals, emit no reason sample.
func TestSelectExpiredGenerationsEligibleAndScopedOutEmitNoBlockedSample(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	century := 100 * 24 * time.Hour
	inst, reader := newExpiredReportTestInstruments(t)
	database := &generationRetentionFakeDB{expiredBlockedRows: [][]any{
		expiredBlockedFixtureRow(now, "gen-eligible", century, false, false, false, false, false),
		expiredBlockedFixtureRow(now, "gen-uncovered", century, false, false, false, true, false),
		expiredBlockedFixtureRow(now, "gen-delta", century, false, false, false, false, true),
		// A scoped-out row whose live-work flags evaluate to a coincidental
		// blocked reason must still emit nothing: readers gate on
		// ScopedOut(), never on the evaluated reason alone.
		expiredBlockedFixtureRow(now, "gen-coincidence", century, false, true, false, true, false),
	}}
	store := NewGenerationRetentionStore(database).WithInstruments(inst)
	store.Now = func() time.Time { return now }

	report, err := store.SelectExpiredGenerations(context.Background(), expiredBlockedTestPolicy())
	if err != nil {
		t.Fatalf("SelectExpiredGenerations() error = %v", err)
	}
	if len(report) != 4 {
		t.Fatalf("len(report) = %d, want 4 fixture rows surfaced", len(report))
	}
	rm := collectExpiredReportMetrics(t, reader)
	for _, reason := range []string{"active_current", "live_work", "retrying", "dependency_pin", ""} {
		if got := expiredBlockedReasonValue(t, rm, reason); got != 0 {
			t.Errorf("reason %q = %d, want 0 (no blocked row in this report)", reason, got)
		}
	}
	assertNoReportIdentityLabels(t, rm)
}

// TestSelectExpiredGenerationsRecordsReportDuration proves every successful
// report records exactly one duration sample, so the read's own cost stays
// visible even when the backlog is empty.
func TestSelectExpiredGenerationsRecordsReportDuration(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	inst, reader := newExpiredReportTestInstruments(t)
	database := &generationRetentionFakeDB{expiredBlockedRows: expiredReportFixtureRows(now)}
	store := NewGenerationRetentionStore(database).WithInstruments(inst)
	store.Now = func() time.Time { return now }

	if _, err := store.SelectExpiredGenerations(context.Background(), expiredBlockedTestPolicy()); err != nil {
		t.Fatalf("SelectExpiredGenerations() error = %v", err)
	}
	rm := collectExpiredReportMetrics(t, reader)
	count, sum := expiredReportDurationSamples(t, rm)
	if count != 1 {
		t.Fatalf("report duration samples = %d, want exactly 1 per report", count)
	}
	if sum < 0 {
		t.Fatalf("report duration sum = %v, want a non-negative wall time", sum)
	}
	assertNoReportIdentityLabels(t, rm)
}

// TestSelectExpiredGenerationsFailureRecordsFailureClass proves a failed
// report read counts once on the shared retention failure counter with the
// expired-report reason and records no duration sample.
func TestSelectExpiredGenerationsFailureRecordsFailureClass(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	inst, reader := newExpiredReportTestInstruments(t)
	database := &generationRetentionFakeDB{expiredBlockedErr: errors.New("connection refused")}
	store := NewGenerationRetentionStore(database).WithInstruments(inst)
	store.Now = func() time.Time { return now }

	if _, err := store.SelectExpiredGenerations(context.Background(), expiredBlockedTestPolicy()); err == nil {
		t.Fatal("SelectExpiredGenerations() error = nil, want the store read failure")
	}
	rm := collectExpiredReportMetrics(t, reader)
	if got := expiredReportFailureValue(t, rm); got != 1 {
		t.Fatalf("expired_report_error failures = %d, want 1", got)
	}
	if count, _ := expiredReportDurationSamples(t, rm); count != 0 {
		t.Fatalf("report duration samples = %d, want 0 on a failed read", count)
	}
	assertNoReportIdentityLabels(t, rm)
}

// TestSelectExpiredGenerationsTelemetryNilSafe proves the report keeps its
// PR2 behavior when no telemetry is wired: nil instruments, tracer, and
// logger record nothing and panic nothing.
func TestSelectExpiredGenerationsTelemetryNilSafe(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	database := &generationRetentionFakeDB{expiredBlockedRows: expiredReportFixtureRows(now)}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	report, err := store.SelectExpiredGenerations(context.Background(), expiredBlockedTestPolicy())
	if err != nil {
		t.Fatalf("SelectExpiredGenerations() error = %v", err)
	}
	blocked := 0
	for _, generation := range report {
		if generation.ExpiredBlocked() {
			blocked++
		}
	}
	if blocked != 4 {
		t.Fatalf("blocked rows = %d, want 4 without telemetry wired", blocked)
	}
}

// TestSelectExpiredGenerationsSpanCarriesScopeAndGenerationIDs proves the
// report span names the read and carries per-blocked-generation scope and
// generation identifiers as span event attributes, never as metric labels.
func TestSelectExpiredGenerationsSpanCarriesScopeAndGenerationIDs(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer("test")
	database := &generationRetentionFakeDB{expiredBlockedRows: expiredReportFixtureRows(now)}
	store := NewGenerationRetentionStore(database).WithTracer(tracer)
	store.Now = func() time.Time { return now }

	if _, err := store.SelectExpiredGenerations(context.Background(), expiredBlockedTestPolicy()); err != nil {
		t.Fatalf("SelectExpiredGenerations() error = %v", err)
	}
	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want exactly 1 report span", len(spans))
	}
	span := spans[0]
	if span.Name() != "generation_retention.expired_report" {
		t.Fatalf("span name = %q, want %q", span.Name(), "generation_retention.expired_report")
	}
	seen := map[string]string{}
	for _, event := range span.Events() {
		if event.Name != "generation_retention.expired_blocked" {
			continue
		}
		seen[spanEventAttr(event.Attributes, "generation_id")] = spanEventAttr(event.Attributes, "scope_id") + "/" + spanEventAttr(event.Attributes, "reason")
	}
	for _, tc := range []struct {
		generationID string
		want         string
	}{
		{"gen-live-1", "scope-1/live_work"},
		{"gen-live-2", "scope-1/live_work"},
		{"gen-retrying", "scope-1/retrying"},
		{"gen-active", "scope-1/active_current"},
	} {
		if got := seen[tc.generationID]; got != tc.want {
			t.Errorf("span event for %s = %q, want %q", tc.generationID, got, tc.want)
		}
	}
	if _, ok := seen["gen-eligible"]; ok {
		t.Error("span carries an event for gen-eligible, want events for blocked rows only")
	}
	if _, ok := seen["gen-scoped-out"]; ok {
		t.Error("span carries an event for gen-scoped-out, want events for blocked rows only")
	}
}

// TestSelectExpiredGenerationsLogsReportSummary proves the report logs one
// summary line with its counts and duration, and one line per blocked
// generation carrying its identifiers, so operators can drill from counts
// to rows without a trace backend.
func TestSelectExpiredGenerationsLogsReportSummary(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	database := &generationRetentionFakeDB{expiredBlockedRows: expiredReportFixtureRows(now)}
	store := NewGenerationRetentionStore(database).WithLogger(logger)
	store.Now = func() time.Time { return now }

	if _, err := store.SelectExpiredGenerations(context.Background(), expiredBlockedTestPolicy()); err != nil {
		t.Fatalf("SelectExpiredGenerations() error = %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, "generation retention expired report completed") {
		t.Fatalf("logs omit the report summary line, got:\n%s", out)
	}
	for _, want := range []string{"expired_total=6", "blocked_total=4", "eligible_total=1", "scoped_out_total=1"} {
		if !strings.Contains(out, want) {
			t.Errorf("logs omit %q, got:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "gen-live-1") || !strings.Contains(out, "scope-1") {
		t.Errorf("logs omit the blocked generation identifiers, got:\n%s", out)
	}
}

// TestExpiredBlockedReasonLabelClosedVocabulary pins the counter's closed
// reason set at the mapping layer, including dependency_pin, which has no
// live-store signal yet and so never fires from the mapper's false flag.
func TestExpiredBlockedReasonLabelClosedVocabulary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		generation GenerationRetentionExpiredGeneration
		wantReason string
		wantCount  bool
	}{
		{
			name:       "active current",
			generation: GenerationRetentionExpiredGeneration{Evaluation: GenerationRetentionEvaluation{Expired: true, BlockedReason: GenerationRetentionBlockedActiveCurrent}},
			wantReason: "active_current",
			wantCount:  true,
		},
		{
			name:       "live work",
			generation: GenerationRetentionExpiredGeneration{Evaluation: GenerationRetentionEvaluation{Expired: true, BlockedReason: GenerationRetentionBlockedLiveWork}},
			wantReason: "live_work",
			wantCount:  true,
		},
		{
			name:       "retrying",
			generation: GenerationRetentionExpiredGeneration{Evaluation: GenerationRetentionEvaluation{Expired: true, BlockedReason: GenerationRetentionBlockedRetrying}},
			wantReason: "retrying",
			wantCount:  true,
		},
		{
			name:       "dependency pin",
			generation: GenerationRetentionExpiredGeneration{Evaluation: GenerationRetentionEvaluation{Expired: true, BlockedReason: GenerationRetentionBlockedDependencyPin}},
			wantReason: "dependency_pin",
			wantCount:  true,
		},
		{
			name:       "eligible emits nothing",
			generation: GenerationRetentionExpiredGeneration{Evaluation: GenerationRetentionEvaluation{Expired: true}},
			wantCount:  false,
		},
		{
			name: "scoped out emits nothing",
			generation: GenerationRetentionExpiredGeneration{
				Evaluation: GenerationRetentionEvaluation{Expired: true, BlockedReason: GenerationRetentionBlockedLiveWork},
				Uncovered:  true,
			},
			wantCount: false,
		},
		{
			name:      "compliant emits nothing",
			wantCount: false,
		},
	} {
		reason, ok := generationRetentionExpiredBlockedLabel(tc.generation)
		if ok != tc.wantCount {
			t.Errorf("%s: counted = %v, want %v", tc.name, ok, tc.wantCount)
		}
		if tc.wantCount && reason != tc.wantReason {
			t.Errorf("%s: reason = %q, want %q", tc.name, reason, tc.wantReason)
		}
	}
}
