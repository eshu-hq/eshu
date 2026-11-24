// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"context"
	"reflect"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestSortedCICDArtifactPatchRunKeysIsDeterministic: the artifact patch walks
// run keys in provider, run id, then attempt order, whatever the map order.
func TestSortedCICDArtifactPatchRunKeysIsDeterministic(t *testing.T) {
	t.Parallel()

	unique := map[cicdRunCorrelationPatchKey]struct{}{
		{provider: "jenkins", runID: "1", runAttempt: "1"}:        {},
		{provider: "github_actions", runID: "2", runAttempt: "1"}: {},
		{provider: "github_actions", runID: "1", runAttempt: "2"}: {},
		{provider: "github_actions", runID: "1", runAttempt: "1"}: {},
	}
	want := []cicdRunCorrelationPatchKey{
		{provider: "github_actions", runID: "1", runAttempt: "1"},
		{provider: "github_actions", runID: "1", runAttempt: "2"},
		{provider: "github_actions", runID: "2", runAttempt: "1"},
		{provider: "jenkins", runID: "1", runAttempt: "1"},
	}
	for range 5 {
		if got := sortedCICDArtifactPatchRunKeys(unique); !reflect.DeepEqual(got, want) {
			t.Fatalf("run keys = %v, want %v", got, want)
		}
	}
}

// TestEmitCountersCountsEachOutcomeAndSkipsZeros: one point per outcome with
// decisions, none for an outcome with zero, and no panic without instruments.
func TestEmitCountersCountsEachOutcomeAndSkipsZeros(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	ctx := context.Background()
	CICDRunCorrelationHandler{Instruments: instruments}.emitCounters(ctx, map[CICDRunCorrelationOutcome]int{
		CICDRunCorrelationExact: 3, CICDRunCorrelationRejected: 1, CICDRunCorrelationAmbiguous: 0,
	})
	CICDRunCorrelationHandler{}.emitCounters(ctx, map[CICDRunCorrelationOutcome]int{CICDRunCorrelationExact: 1})

	var resources metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &resources); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := map[string]int64{}
	for _, scope := range resources.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if m.Name != "eshu_dp_ci_cd_run_correlations_total" || !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				outcome, _ := point.Attributes.Value("outcome")
				got[outcome.AsString()] += point.Value
			}
		}
	}
	want := map[string]int64{string(CICDRunCorrelationExact): 3, string(CICDRunCorrelationRejected): 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outcome counts = %v, want %v", got, want)
	}
}
