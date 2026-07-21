// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestRecordFluxCrossRepoURLResolutionStatsEmitsOnePointPerOutcome proves the
// eshu_dp_flux_cross_repo_url_resolution_total counter records one data
// point per non-zero outcome label (issue #5483 C2 telemetry contract:
// outcome=linked|unresolved|ambiguous|self).
func TestRecordFluxCrossRepoURLResolutionStatsEmitsOnePointPerOutcome(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	recordFluxCrossRepoURLResolutionStats(context.Background(), instruments, relationships.FluxCrossRepoURLResolutionStats{
		Linked:     2,
		Unresolved: 3,
		Ambiguous:  1,
		Self:       4,
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	got := map[string]int64{}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != "eshu_dp_flux_cross_repo_url_resolution_total" {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want metricdata.Sum[int64]", metricRecord.Data)
			}
			for _, dp := range sum.DataPoints {
				outcome, ok := dp.Attributes.Value(telemetry.MetricDimensionOutcome)
				if !ok {
					t.Fatalf("data point missing %q attribute: %#v", telemetry.MetricDimensionOutcome, dp.Attributes)
				}
				got[outcome.AsString()] = dp.Value
			}
		}
	}

	want := map[string]int64{"linked": 2, "unresolved": 3, "ambiguous": 1, "self": 4}
	for outcome, wantValue := range want {
		if got[outcome] != wantValue {
			t.Errorf("outcome %q = %d, want %d (all outcomes: %#v)", outcome, got[outcome], wantValue, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("recorded %d distinct outcomes, want %d: %#v", len(got), len(want), got)
	}
}

// TestRecordFluxCrossRepoURLResolutionStatsSkipsZeroOutcomes proves a
// zero-count outcome never emits a spurious zero-valued data point, matching
// the recordEvent nil-guard convention elsewhere in this package.
func TestRecordFluxCrossRepoURLResolutionStatsSkipsZeroOutcomes(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	recordFluxCrossRepoURLResolutionStats(context.Background(), instruments, relationships.FluxCrossRepoURLResolutionStats{
		Unresolved: 1,
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	count := 0
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != "eshu_dp_flux_cross_repo_url_resolution_total" {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want metricdata.Sum[int64]", metricRecord.Data)
			}
			count = len(sum.DataPoints)
		}
	}
	if count != 1 {
		t.Fatalf("data point count = %d, want 1 (only the non-zero unresolved outcome)", count)
	}
}

// TestRecordFluxCrossRepoURLResolutionStatsAllowsNilInstruments proves the
// helper never panics when Instruments is unset, mirroring
// TestAWSPaginationCheckpointStoreRecordEventAllowsPartialInstruments.
func TestRecordFluxCrossRepoURLResolutionStatsAllowsNilInstruments(t *testing.T) {
	t.Parallel()

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("recordFluxCrossRepoURLResolutionStats() panic = %v, want nil", recovered)
		}
	}()

	recordFluxCrossRepoURLResolutionStats(context.Background(), nil, relationships.FluxCrossRepoURLResolutionStats{Linked: 1})
	recordFluxCrossRepoURLResolutionStats(context.Background(), &telemetry.Instruments{}, relationships.FluxCrossRepoURLResolutionStats{Linked: 1})
}
