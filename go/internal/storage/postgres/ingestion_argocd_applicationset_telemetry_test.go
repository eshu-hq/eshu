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

const argocdApplicationSetTemplateSourceMetric = "eshu_dp_argocd_applicationset_template_source_total"

// collectApplicationSetTemplateSourcePoints returns outcome -> value for the
// ApplicationSet template-source counter.
func collectApplicationSetTemplateSourcePoints(t *testing.T, reader sdkmetric.Reader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	got := map[string]int64{}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != argocdApplicationSetTemplateSourceMetric {
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
	return got
}

// TestRecordArgoCDApplicationSetTemplateSourceStatsEmitsOnePointPerOutcome
// proves each of the four closed outcome labels increments its own data point
// by the tallied count (issue #7767).
func TestRecordArgoCDApplicationSetTemplateSourceStatsEmitsOnePointPerOutcome(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	recordArgoCDApplicationSetTemplateSourceStats(context.Background(), instruments, relationships.ApplicationSetTemplateSourceStats{
		DeploySource:                2,
		SelfReference:               3,
		SkippedControlRepo:          1,
		SkippedTemplatedDestination: 4,
	})

	got := collectApplicationSetTemplateSourcePoints(t, reader)
	want := map[string]int64{
		relationships.ApplicationSetTemplateSourceOutcomeDeploySource:                2,
		relationships.ApplicationSetTemplateSourceOutcomeSelfReference:               3,
		relationships.ApplicationSetTemplateSourceOutcomeSkippedControlRepo:          1,
		relationships.ApplicationSetTemplateSourceOutcomeSkippedTemplatedDestination: 4,
	}
	for outcome, wantValue := range want {
		if got[outcome] != wantValue {
			t.Errorf("outcome %q = %d, want %d (all outcomes: %#v)", outcome, got[outcome], wantValue, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("recorded %d distinct outcomes, want %d: %#v", len(got), len(want), got)
	}
	for _, outcome := range []string{"deploy_source", "template_source_self_reference", "skipped_control_repo", "skipped_templated_destination"} {
		if _, ok := got[outcome]; !ok {
			t.Errorf("literal outcome label %q was not emitted: %#v", outcome, got)
		}
	}
}

// TestRecordArgoCDApplicationSetTemplateSourceStatsSkipsZeroOutcomes proves a
// zero-count outcome never emits a zero-valued data point.
func TestRecordArgoCDApplicationSetTemplateSourceStatsSkipsZeroOutcomes(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	recordArgoCDApplicationSetTemplateSourceStats(context.Background(), instruments, relationships.ApplicationSetTemplateSourceStats{
		SkippedControlRepo: 1,
	})

	got := collectApplicationSetTemplateSourcePoints(t, reader)
	if len(got) != 1 || got["skipped_control_repo"] != 1 {
		t.Fatalf("data points = %#v, want only skipped_control_repo=1", got)
	}
}

// TestRecordArgoCDApplicationSetTemplateSourceStatsAllowsNilInstruments proves
// the helper never panics when Instruments or the counter is unset.
func TestRecordArgoCDApplicationSetTemplateSourceStatsAllowsNilInstruments(t *testing.T) {
	t.Parallel()

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("recordArgoCDApplicationSetTemplateSourceStats() panic = %v, want nil", recovered)
		}
	}()

	stats := relationships.ApplicationSetTemplateSourceStats{DeploySource: 1}
	recordArgoCDApplicationSetTemplateSourceStats(context.Background(), nil, stats)
	recordArgoCDApplicationSetTemplateSourceStats(context.Background(), &telemetry.Instruments{}, stats)
}
