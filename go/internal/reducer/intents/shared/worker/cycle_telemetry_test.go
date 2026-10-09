// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func backoffTestInstruments(t *testing.T) (*sdkmetric.ManualReader, *telemetry.Instruments) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return reader, inst
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return rm
}

// TestRecordPartitionVisit_EmitsOutcomeAndBackoff proves every visit
// records the visits counter (domain + closed outcome) and the current
// per-partition backoff delay gauge.
func TestRecordPartitionVisit_EmitsOutcomeAndBackoff(t *testing.T) {
	t.Parallel()

	reader, inst := backoffTestInstruments(t)
	runner := Runner{Instruments: inst}

	const domain = reducercontract.DomainSQLRelationships
	runner.recordPartitionVisit(context.Background(), domain, 3, telemetry.SharedProjectionVisitOutcomeBackoffSkipped, 2500*time.Millisecond)
	runner.recordPartitionVisit(context.Background(), domain, 4, telemetry.SharedProjectionVisitOutcomeVisited, 0)

	rm := collectMetrics(t, reader)

	if !sumHasAttrsAndValue(rm, "eshu_dp_shared_projection_partition_visits_total", map[string]any{
		telemetry.MetricDimensionDomain:  domain,
		telemetry.MetricDimensionOutcome: telemetry.SharedProjectionVisitOutcomeBackoffSkipped,
	}, 1) {
		t.Errorf("visits_total: no data point with domain=%q outcome=backoff_skipped value=1", domain)
	}
	if !sumHasAttrsAndValue(rm, "eshu_dp_shared_projection_partition_visits_total", map[string]any{
		telemetry.MetricDimensionDomain:  domain,
		telemetry.MetricDimensionOutcome: telemetry.SharedProjectionVisitOutcomeVisited,
	}, 1) {
		t.Errorf("visits_total: no data point with domain=%q outcome=visited value=1", domain)
	}
	if !gaugeFloatHasAttrsAndValue(rm, "eshu_dp_shared_projection_partition_backoff_seconds", map[string]any{
		telemetry.MetricDimensionDomain:      domain,
		telemetry.MetricDimensionPartitionID: int64(3),
	}, 2.5) {
		t.Errorf("partition_backoff_seconds: no data point with domain=%q partition_id=3 value=2.5", domain)
	}
	assertOnlyAllowedLabels(t, rm, []string{
		"eshu_dp_shared_projection_partition_visits_total",
		"eshu_dp_shared_projection_partition_backoff_seconds",
	}, map[string]bool{
		telemetry.MetricDimensionDomain:      true,
		telemetry.MetricDimensionOutcome:     true,
		telemetry.MetricDimensionPartitionID: true,
	})
}

// TestRecordSelectionPrefetch_EmitsKindStats proves one visit's prefetch
// keys/queries/rows/cache-hits/durations and widen rounds land on the
// per-kind instruments.
func TestRecordSelectionPrefetch_EmitsKindStats(t *testing.T) {
	t.Parallel()

	reader, inst := backoffTestInstruments(t)
	runner := Runner{Instruments: inst}

	const domain = reducercontract.DomainRunsIn
	runner.recordSelectionPrefetch(context.Background(), domain, PartitionProcessResult{
		SelectionRounds: 2,
		PrefetchStats: sharedintent.PrefetchStats{
			Acceptance: sharedintent.PrefetchKindStats{Keys: 10, Queries: 2, Rows: 8, Duration: 3 * time.Millisecond},
			Readiness:  sharedintent.PrefetchKindStats{Keys: 5, Queries: 1, CacheHits: 7, Duration: time.Millisecond},
		},
	})

	rm := collectMetrics(t, reader)

	acceptance := map[string]any{telemetry.MetricDimensionDomain: domain, telemetry.MetricDimensionKind: "acceptance"}
	readiness := map[string]any{telemetry.MetricDimensionDomain: domain, telemetry.MetricDimensionKind: "readiness"}
	if !sumHasAttrsAndValue(rm, "eshu_dp_shared_projection_prefetch_keys_total", acceptance, 10) {
		t.Error("prefetch_keys_total: no acceptance data point with value=10")
	}
	if !sumHasAttrsAndValue(rm, "eshu_dp_shared_projection_prefetch_queries_total", acceptance, 2) {
		t.Error("prefetch_queries_total: no acceptance data point with value=2")
	}
	if !sumHasAttrsAndValue(rm, "eshu_dp_shared_projection_prefetch_rows_total", acceptance, 8) {
		t.Error("prefetch_rows_total: no acceptance data point with value=8")
	}
	if !sumHasAttrsAndValue(rm, "eshu_dp_shared_projection_prefetch_keys_total", readiness, 5) {
		t.Error("prefetch_keys_total: no readiness data point with value=5")
	}
	if !sumHasAttrsAndValue(rm, "eshu_dp_shared_projection_prefetch_cache_hits_total", readiness, 7) {
		t.Error("prefetch_cache_hits_total: no readiness data point with value=7")
	}
	if !histogramHasAttrsAndValue(rm, "eshu_dp_shared_projection_prefetch_seconds", acceptance, 0.003) {
		t.Error("prefetch_seconds: no acceptance data point with sum~=0.003")
	}
	if !sumHasAttrsAndValue(rm, "eshu_dp_shared_projection_selection_rounds_total", map[string]any{telemetry.MetricDimensionDomain: domain}, 2) {
		t.Error("selection_rounds_total: no data point with value=2")
	}
	assertOnlyAllowedLabels(t, rm, []string{
		"eshu_dp_shared_projection_prefetch_keys_total",
		"eshu_dp_shared_projection_prefetch_queries_total",
		"eshu_dp_shared_projection_prefetch_rows_total",
		"eshu_dp_shared_projection_prefetch_cache_hits_total",
		"eshu_dp_shared_projection_prefetch_seconds",
		"eshu_dp_shared_projection_selection_rounds_total",
	}, map[string]bool{
		telemetry.MetricDimensionDomain: true,
		telemetry.MetricDimensionKind:   true,
	})
}

// TestRecordSelectionPrefetch_SkipsWhenNoSelection proves a visit that ran
// no selection (lease held, backoff skipped) emits no prefetch points.
func TestRecordSelectionPrefetch_SkipsWhenNoSelection(t *testing.T) {
	t.Parallel()

	reader, inst := backoffTestInstruments(t)
	runner := Runner{Instruments: inst}

	runner.recordSelectionPrefetch(context.Background(), reducercontract.DomainSQLRelationships, PartitionProcessResult{})

	rm := collectMetrics(t, reader)
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "eshu_dp_shared_projection_prefetch_keys_total",
				"eshu_dp_shared_projection_prefetch_queries_total",
				"eshu_dp_shared_projection_prefetch_rows_total",
				"eshu_dp_shared_projection_prefetch_cache_hits_total",
				"eshu_dp_shared_projection_prefetch_seconds",
				"eshu_dp_shared_projection_selection_rounds_total":
				t.Errorf("unexpected metric %q emitted for a visit with no selection", m.Name)
			}
		}
	}
}

// TestRecordCycleBackoff_EmitsGauges proves the per-cycle backoff summary
// samples the global interval and at-max gauges.
func TestRecordCycleBackoff_EmitsGauges(t *testing.T) {
	t.Parallel()

	reader, inst := backoffTestInstruments(t)
	runner := Runner{Instruments: inst}
	// Pin one cell at T_max through the runner's own tracker so the gauge
	// reflects real tracker state.
	tracker := runner.tracker()
	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		tracker.reportUnproductive(reducercontract.DomainSQLRelationships, 3, now)
	}

	runner.recordCycleBackoff(context.Background(), 2*time.Second, PartitionProcessResult{
		PartitionsVisited:        80,
		PartitionsBackoffSkipped: 8,
	})

	rm := collectMetrics(t, reader)

	if !gaugeFloatHasAttrsAndValue(rm, "eshu_dp_shared_projection_cycle_backoff_seconds", map[string]any{}, 2.0) {
		t.Error("cycle_backoff_seconds: no data point with value=2.0")
	}
	if !gaugeIntHasAttrsAndValue(rm, "eshu_dp_shared_projection_partitions_at_max_backoff", map[string]any{}, 1) {
		t.Error("partitions_at_max_backoff: no data point with value=1")
	}
}

// TestRunSamplesCycleGaugesOnProductiveCycles proves the cycle gauges
// sample every cycle: the run below performs exactly one productive
// cycle and cancels before any empty cycle can run, so the poll-interval
// data point can only come from the productive path.
func TestRunSamplesCycleGaugesOnProductiveCycles(t *testing.T) {
	t.Parallel()

	reader, inst := backoffTestInstruments(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &Runner{
		IntentReader: &cancelOnMarkReader{
			fakeSharedIntentReader: &fakeSharedIntentReader{
				intents: []sharedintent.Row{selectionTestRow(reducercontract.DomainWorkloadDependency, "scope-a", "unit-a", "gen-1", 0)},
			},
			cancel: cancel,
		},
		LeaseManager: &fakeLeaseManager{granted: true},
		EdgeWriter:   &fakeEdgeWriter{},
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Instruments:  inst,
		Config: RunnerConfig{
			PartitionCount: 1,
			PollInterval:   50 * time.Millisecond,
		},
		Wait: func(context.Context, time.Duration) error {
			t.Error("Wait called: the run should end after its single productive cycle")
			return nil
		},
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	rm := collectMetrics(t, reader)
	if !gaugeFloatHasAttrsAndValue(rm, "eshu_dp_shared_projection_cycle_backoff_seconds", map[string]any{}, 0.05) {
		t.Error("cycle_backoff_seconds: no data point with the poll interval 0.05 after the productive cycle")
	}
}

// cancelOnMarkReader cancels the run's context from the first completion,
// so the runner performs exactly one productive cycle and returns.
type cancelOnMarkReader struct {
	*fakeSharedIntentReader
	cancel context.CancelFunc
}

func (r *cancelOnMarkReader) MarkIntentsCompleted(ctx context.Context, intentIDs []string, completedAt time.Time) error {
	err := r.fakeSharedIntentReader.MarkIntentsCompleted(ctx, intentIDs, completedAt)
	r.cancel()
	return err
}

// TestPrefetchKindLabel_MapsClosedSet pins the sharedintent kind values to
// the telemetry kind labels so a rename on either side fails loudly.
func TestPrefetchKindLabel_MapsClosedSet(t *testing.T) {
	t.Parallel()

	if got := prefetchKindLabel(sharedintent.PrefetchKindAcceptance); got != "acceptance" {
		t.Fatalf("prefetchKindLabel(acceptance) = %q, want %q", got, "acceptance")
	}
	if got := prefetchKindLabel(sharedintent.PrefetchKindReadiness); got != "readiness" {
		t.Fatalf("prefetchKindLabel(readiness) = %q, want %q", got, "readiness")
	}
	if telemetry.SharedProjectionPrefetchKindAcceptance != "acceptance" || telemetry.SharedProjectionPrefetchKindReadiness != "readiness" {
		t.Fatal("telemetry kind constants drifted from the closed set")
	}
}

// sumHasAttrsAndValue checks that a Sum[int64] metric carries a data point
// matching every attribute with the given value.
func sumHasAttrsAndValue(rm metricdata.ResourceMetrics, name string, attrs map[string]any, wantValue int64) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				if !attrsMatch(dp.Attributes, attrs) {
					continue
				}
				if dp.Value == wantValue {
					return true
				}
			}
		}
	}
	return false
}

// gaugeFloatHasAttrsAndValue checks that a Gauge[float64] metric carries a
// matching data point (within 1% + epsilon).
func gaugeFloatHasAttrsAndValue(rm metricdata.ResourceMetrics, name string, attrs map[string]any, wantValue float64) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[float64])
			if !ok {
				continue
			}
			for _, dp := range gauge.DataPoints {
				if !attrsMatch(dp.Attributes, attrs) {
					continue
				}
				diff := dp.Value - wantValue
				if diff < 0 {
					diff = -diff
				}
				if diff <= wantValue*0.01+0.0001 {
					return true
				}
			}
		}
	}
	return false
}

// gaugeIntHasAttrsAndValue checks that a Gauge[int64] metric carries a
// matching data point.
func gaugeIntHasAttrsAndValue(rm metricdata.ResourceMetrics, name string, attrs map[string]any, wantValue int64) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				continue
			}
			for _, dp := range gauge.DataPoints {
				if !attrsMatch(dp.Attributes, attrs) {
					continue
				}
				if dp.Value == wantValue {
					return true
				}
			}
		}
	}
	return false
}

func attrsMatch(set attribute.Set, attrs map[string]any) bool {
	for key, want := range attrs {
		got, ok := set.Value(attribute.Key(key))
		if !ok {
			return false
		}
		var gotVal any
		switch got.Type() {
		case attribute.STRING:
			gotVal = got.AsString()
		case attribute.INT64:
			gotVal = got.AsInt64()
		default:
			return false
		}
		if gotVal != want {
			return false
		}
	}
	return true
}

// assertOnlyAllowedLabels fails when any data point of the named metrics
// carries a label outside the allowed set: no intent, scope, generation,
// key, or owner value may ride these instruments.
func assertOnlyAllowedLabels(t *testing.T, rm metricdata.ResourceMetrics, names []string, allowed map[string]bool) {
	t.Helper()
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if !wanted[m.Name] {
				continue
			}
			check := func(set attribute.Set) {
				t.Helper()
				iter := set.Iter()
				for iter.Next() {
					if !allowed[string(iter.Attribute().Key)] {
						t.Errorf("%s data point carries forbidden label %q", m.Name, string(iter.Attribute().Key))
					}
				}
			}
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					check(dp.Attributes)
				}
			case metricdata.Gauge[float64]:
				for _, dp := range data.DataPoints {
					check(dp.Attributes)
				}
			case metricdata.Gauge[int64]:
				for _, dp := range data.DataPoints {
					check(dp.Attributes)
				}
			case metricdata.Histogram[float64]:
				for _, dp := range data.DataPoints {
					check(dp.Attributes)
				}
			}
		}
	}
}
