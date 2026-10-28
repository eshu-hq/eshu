// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

func newExtensionStatusRecorder(t testing.TB) (*ComponentExtensionStatusRecorder, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	return NewComponentExtensionStatusRecorder(inst), reader
}

func collectExtension(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Aggregation {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	out := map[string]metricdata.Aggregation{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out[m.Name] = m.Data
		}
	}
	return out
}

// sumByAttrs sums an Int64 counter's points keyed by "k=v," pairs in the
// attribute set's sorted order.
func sumByAttrs(t *testing.T, data metricdata.Aggregation) map[string]int64 {
	t.Helper()
	sum, ok := data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("data = %T, want Sum[int64]", data)
	}
	out := map[string]int64{}
	for _, dp := range sum.DataPoints {
		key := ""
		for _, kv := range dp.Attributes.ToSlice() {
			key += string(kv.Key) + "=" + kv.Value.AsString() + ","
		}
		out[key] += dp.Value
	}
	return out
}

// TestComponentExtensionStatusRecorderRecordsEveryInstrument proves one status
// row and one result move every instrument of the family with the bounded
// label set and the right values.
func TestComponentExtensionStatusRecorderRecordsEveryInstrument(t *testing.T) {
	t.Parallel()

	recorder, reader := newExtensionStatusRecorder(t)
	ctx := context.Background()
	recorder.RecordStatus(ctx, "jira", "retryable", "failure", "rate_limited", 3, 2, 1500)
	recorder.RecordResult(ctx, "jira", "retryable", "rate_limited", 120*time.Second)

	data := collectExtension(t, reader)
	if got := sumByAttrs(t, data[contract.MetricComponentExtensionResults]); got["collector_kind=jira,failure_class=rate_limited,state=retryable,"] != 1 {
		t.Fatalf("results = %v, want one retryable rate_limited jira result", got)
	}
	if got := sumByAttrs(t, data[contract.MetricComponentExtensionStatusRecords]); got["collector_kind=jira,failure_class=rate_limited,state=retryable,status_class=failure,"] != 1 {
		t.Fatalf("status records = %v, want one failure row", got)
	}
	if got := sumByAttrs(t, data[contract.MetricComponentExtensionReportedFacts]); got["collector_kind=jira,state=retryable,"] != 3 {
		t.Fatalf("reported facts = %v, want 3", got)
	}
	if got := sumByAttrs(t, data[contract.MetricComponentExtensionReportedWarnings]); got["collector_kind=jira,state=retryable,"] != 2 {
		t.Fatalf("reported warnings = %v, want 2", got)
	}
	latency, ok := data[contract.MetricComponentExtensionSourceLatency].(metricdata.Histogram[float64])
	if !ok || len(latency.DataPoints) != 1 || latency.DataPoints[0].Count != 1 || latency.DataPoints[0].Sum != 1.5 {
		t.Fatalf("source latency = %#v, want one 1.5 s observation", data[contract.MetricComponentExtensionSourceLatency])
	}
	gauge, ok := data[contract.MetricComponentExtensionRetryAfter].(metricdata.Gauge[float64])
	if !ok || len(gauge.DataPoints) != 1 || gauge.DataPoints[0].Value != 120 {
		t.Fatalf("retry-after gauge = %#v, want 120", data[contract.MetricComponentExtensionRetryAfter])
	}
	if got, _ := gauge.DataPoints[0].Attributes.Value("collector_kind"); got.AsString() != "jira" || gauge.DataPoints[0].Attributes.Len() != 1 {
		t.Fatalf("retry-after attrs = %v, want only collector_kind=jira", gauge.DataPoints[0].Attributes.ToSlice())
	}

	// A later non-retryable result resets the requested delay to zero so the
	// gauge reports the latest request, not the last throttle forever.
	recorder.RecordResult(ctx, "jira", "complete", "", 0)
	gauge = collectExtension(t, reader)[contract.MetricComponentExtensionRetryAfter].(metricdata.Gauge[float64])
	if gauge.DataPoints[0].Value != 0 {
		t.Fatalf("retry-after gauge after complete = %v, want 0", gauge.DataPoints[0].Value)
	}
}

// TestComponentExtensionStatusRecorderSkipsUnreportedCounts proves zero
// fact, warning, and latency values do not create observations: an omitted
// source_latency_ms must not drag the histogram toward zero.
func TestComponentExtensionStatusRecorderSkipsUnreportedCounts(t *testing.T) {
	t.Parallel()

	recorder, reader := newExtensionStatusRecorder(t)
	recorder.RecordStatus(context.Background(), "jira", "complete", "complete", "", 0, 0, 0)
	data := collectExtension(t, reader)
	for _, name := range []string{
		contract.MetricComponentExtensionReportedFacts,
		contract.MetricComponentExtensionReportedWarnings,
		contract.MetricComponentExtensionSourceLatency,
	} {
		if _, ok := data[name]; ok {
			t.Fatalf("%s recorded for zero-valued status, want no observation", name)
		}
	}
	if got := sumByAttrs(t, data[contract.MetricComponentExtensionStatusRecords]); got["collector_kind=jira,failure_class=none,state=complete,status_class=complete,"] != 1 {
		t.Fatalf("status records = %v, want failure_class=none", got)
	}
}

// TestComponentExtensionStatusRecorderBoundsLabels proves no caller value can
// mint an unbounded series: unknown state and class fold to unknown, an
// unpatterned failure class folds to other, and failure classes past the
// per-process cap fold to other while admitted ones keep their value.
func TestComponentExtensionStatusRecorderBoundsLabels(t *testing.T) {
	t.Parallel()

	recorder, reader := newExtensionStatusRecorder(t)
	ctx := context.Background()
	recorder.RecordResult(ctx, "jira", "exploded", "https://tenant.example/api?token=x", 0)
	recorder.RecordStatus(ctx, "jira", "complete", "shouting", "Has Space", 0, 0, 0)
	for i := 0; i < contract.ComponentExtensionFailureClassCap+10; i++ {
		recorder.RecordResult(ctx, "jira", "terminal", fmt.Sprintf("class_%03d", i), 0)
	}
	recorder.RecordResult(ctx, "jira", "terminal", "class_000", 0)

	results := sumByAttrs(t, collectExtension(t, reader)[contract.MetricComponentExtensionResults])
	if got := results["collector_kind=jira,failure_class=other,state=unknown,"]; got != 1 {
		t.Fatalf("results = %v, want the URL failure class and bad state folded", results)
	}
	if got := results["collector_kind=jira,failure_class=class_000,state=terminal,"]; got != 2 {
		t.Fatalf("admitted class_000 = %d, want 2 (admission is stable)", got)
	}
	distinct := map[string]bool{}
	for key := range results {
		distinct[key] = true
	}
	// Admitted classes plus the folded other bucket, plus the unknown-state
	// row: never one series per minted class.
	if limit := contract.ComponentExtensionFailureClassCap + 2; len(distinct) > limit {
		t.Fatalf("distinct result series = %d, want <= %d", len(distinct), limit)
	}
	if got := results["collector_kind=jira,failure_class=other,state=terminal,"]; got == 0 {
		t.Fatalf("results = %v, want classes past the cap folded to other", results)
	}
	statuses := sumByAttrs(t, collectExtension(t, reader)[contract.MetricComponentExtensionStatusRecords])
	if got := statuses["collector_kind=jira,failure_class=other,state=complete,status_class=unknown,"]; got != 1 {
		t.Fatalf("status records = %v, want folded class and failure class", statuses)
	}
}

// TestComponentExtensionStatusRecorderAdmitsRefinedFailureClass pins that a
// collector-refined "<class>.<reason>" token is a legal label: it is admitted
// as its own series on the result and status instruments, not folded to
// other (eshu-hq/eshu#9289). A malformed refinement still folds.
func TestComponentExtensionStatusRecorderAdmitsRefinedFailureClass(t *testing.T) {
	t.Parallel()

	recorder, reader := newExtensionStatusRecorder(t)
	ctx := context.Background()
	const class = "invalid_config.target_scope_mismatch"
	recorder.RecordResult(ctx, "security_alert", "terminal", class, 0)
	recorder.RecordStatus(ctx, "security_alert", "terminal", "failure", class, 0, 0, 0)
	recorder.RecordResult(ctx, "security_alert", "terminal", "invalid_config.Bad Reason", 0)

	metrics := collectExtension(t, reader)
	results := sumByAttrs(t, metrics[contract.MetricComponentExtensionResults])
	if got := results["collector_kind=security_alert,failure_class="+class+",state=terminal,"]; got != 1 {
		t.Fatalf("results = %v, want the refined class admitted as its own label", results)
	}
	if got := results["collector_kind=security_alert,failure_class=other,state=terminal,"]; got != 1 {
		t.Fatalf("results = %v, want the malformed refinement folded to other", results)
	}
	statuses := sumByAttrs(t, metrics[contract.MetricComponentExtensionStatusRecords])
	if got := statuses["collector_kind=security_alert,failure_class="+class+",state=terminal,status_class=failure,"]; got != 1 {
		t.Fatalf("status records = %v, want the refined class admitted", statuses)
	}
}

// TestComponentExtensionStatusRecorderFailureClassLengthBound pins the label
// pattern edge: a 96-character class is admitted as its own label and the first
// 97-character class folds to other, so widening the bound by one fails here
// (eshu-hq/eshu#9289).
func TestComponentExtensionStatusRecorderFailureClassLengthBound(t *testing.T) {
	t.Parallel()

	recorder, reader := newExtensionStatusRecorder(t)
	ctx := context.Background()
	atBound := "a." + strings.Repeat("b", 94)
	pastBound := "a." + strings.Repeat("b", 95)
	if len(atBound) != 96 || len(pastBound) != 97 {
		t.Fatalf("fixture lengths = %d, %d, want 96, 97", len(atBound), len(pastBound))
	}
	recorder.RecordResult(ctx, "security_alert", "terminal", atBound, 0)
	recorder.RecordResult(ctx, "security_alert", "terminal", pastBound, 0)

	results := sumByAttrs(t, collectExtension(t, reader)[contract.MetricComponentExtensionResults])
	if got := results["collector_kind=security_alert,failure_class="+atBound+",state=terminal,"]; got != 1 {
		t.Fatalf("results = %v, want the 96-character class admitted", results)
	}
	if got := results["collector_kind=security_alert,failure_class=other,state=terminal,"]; got != 1 {
		t.Fatalf("results = %v, want the 97-character class folded to other", results)
	}
}

// TestComponentExtensionStatusRecorderNilSafe proves a recorder built without
// instruments, and a nil recorder, record nothing and do not panic.
func TestComponentExtensionStatusRecorderNilSafe(t *testing.T) {
	t.Parallel()

	var nilRecorder *ComponentExtensionStatusRecorder
	nilRecorder.RecordStatus(context.Background(), "jira", "complete", "complete", "", 1, 1, 1)
	nilRecorder.RecordResult(context.Background(), "jira", "complete", "", 0)
	empty := NewComponentExtensionStatusRecorder(nil)
	empty.RecordStatus(context.Background(), "jira", "complete", "complete", "", 1, 1, 1)
	empty.RecordResult(context.Background(), "jira", "complete", "", 0)
}

// TestComponentExtensionStatusRecorderConcurrent records from many goroutines
// against the shared caches; run under -race it proves NextClaimed callers on
// one worker can share a recorder.
func TestComponentExtensionStatusRecorderConcurrent(t *testing.T) {
	t.Parallel()

	recorder, reader := newExtensionStatusRecorder(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	const workers, perWorker = 8, 200
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				class := fmt.Sprintf("class_%d", (w*perWorker+i)%100)
				recorder.RecordStatus(ctx, "jira", "retryable", "failure", class, 1, 0, 10)
				recorder.RecordResult(ctx, "jira", "retryable", class, time.Second)
			}
		}(w)
	}
	wg.Wait()
	var total int64
	for _, v := range sumByAttrs(t, collectExtension(t, reader)[contract.MetricComponentExtensionResults]) {
		total += v
	}
	if total != workers*perWorker {
		t.Fatalf("results total = %d, want %d", total, workers*perWorker)
	}
}

// TestComponentExtensionRetryAfterGaugeDescribesTheRequest pins the operator
// contract (#8908 review F1): the gauge reports the delay the extension
// requested after the one-hour cap, not the delay ClaimedService applied. The
// host records it before ClaimedService applies max(poll interval, request)
// or spends the attempt budget, so a description that says "honored" would
// mislead an operator reading a throttled collector at 3 AM.
func TestComponentExtensionRetryAfterGaugeDescribesTheRequest(t *testing.T) {
	t.Parallel()

	recorder, reader := newExtensionStatusRecorder(t)
	recorder.RecordResult(context.Background(), "jira", "retryable", "rate_limited", time.Second)
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var description string
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == contract.MetricComponentExtensionRetryAfter {
				description = m.Description
			}
		}
	}
	if description == "" {
		t.Fatalf("%s was not collected", contract.MetricComponentExtensionRetryAfter)
	}
	if !strings.Contains(description, "requested") {
		t.Fatalf("description = %q, want it to say the delay is what the extension requested", description)
	}
	if strings.Contains(description, "honored") {
		t.Fatalf("description = %q, must not claim the delay was honored: ClaimedService decides afterwards", description)
	}
}

// TestComponentExtensionFailureClassCapIsDocumented pins the number the
// operator docs state ("past 64"), so changing the cap forces a docs edit.
func TestComponentExtensionFailureClassCapIsDocumented(t *testing.T) {
	t.Parallel()

	if got := contract.ComponentExtensionFailureClassCap; got != 64 {
		t.Fatalf("ComponentExtensionFailureClassCap = %d, want 64 as documented in metrics.md and the extensionhost README", got)
	}
}

// TestComponentExtensionStatusRecorderFoldsUnpatternedKind proves a
// collector_kind outside the bounded pattern folds to unknown even though the
// host passes only manifest kinds: the recorder must stay safe on its own.
func TestComponentExtensionStatusRecorderFoldsUnpatternedKind(t *testing.T) {
	t.Parallel()

	recorder, reader := newExtensionStatusRecorder(t)
	recorder.RecordResult(context.Background(), "https://tenant.example/x", "complete", "", 0)
	results := sumByAttrs(t, collectExtension(t, reader)[contract.MetricComponentExtensionResults])
	if got := results["collector_kind=unknown,failure_class=none,state=complete,"]; got != 1 {
		t.Fatalf("results = %v, want the unpatterned kind folded to unknown", results)
	}
}

// BenchmarkComponentExtensionStatusRecorder measures the warmed recorder: one
// status row plus one result, the per-claim cost the extension host adds.
func BenchmarkComponentExtensionStatusRecorder(b *testing.B) {
	recorder, _ := newExtensionStatusRecorder(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		recorder.RecordStatus(ctx, "jira", "retryable", "failure", "rate_limited", 3, 1, 250)
		recorder.RecordResult(ctx, "jira", "retryable", "rate_limited", 120*time.Second)
	}
}
