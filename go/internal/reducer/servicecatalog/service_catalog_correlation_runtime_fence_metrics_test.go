// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// TestServiceCatalogRuntimeReadinessCounterRecordsEveryOutcome pins the closed
// outcome set of eshu_dp_service_catalog_runtime_readiness_total: one count
// per fenced pass, labeled by the decision the fence took.
func TestServiceCatalogRuntimeReadinessCounterRecordsEveryOutcome(t *testing.T) {
	t.Parallel()

	cases := []struct {
		outcome    string
		states     map[string]RuntimeProducerState
		startedAgo time.Duration
	}{
		{outcome: RuntimeReadinessReady, states: map[string]RuntimeProducerState{"repo-checkout": {Total: 1}}, startedAgo: time.Minute},
		{outcome: RuntimeReadinessNoProducer, states: nil, startedAgo: time.Minute},
		{outcome: RuntimeReadinessDeferred, states: map[string]RuntimeProducerState{"repo-checkout": {Running: 1, Total: 1}}, startedAgo: time.Minute},
		{outcome: RuntimeReadinessBoundExceeded, states: map[string]RuntimeProducerState{"repo-checkout": {Pending: 1, Total: 1}}, startedAgo: crossscope.ProducerReadinessMaxWait + time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.outcome, func(t *testing.T) {
			t.Parallel()
			inst, reader := newServiceCatalogCorrelationInstruments(t)
			h := newRuntimeFenceHarness(tc.states, []ServiceRuntimeInstance{runtimeFenceInstance})
			h.handler.Instruments = inst
			_, _ = h.handler.Handle(context.Background(), runtimeFenceIntent(tc.startedAgo))

			var rm metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &rm); err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			got := reducerCounterValue(t, rm, "eshu_dp_service_catalog_runtime_readiness_total", map[string]string{
				"domain":  "service_catalog_correlation",
				"outcome": tc.outcome,
			})
			if got != 1 {
				t.Fatalf("eshu_dp_service_catalog_runtime_readiness_total{outcome=%q} = %d, want 1", tc.outcome, got)
			}
		})
	}
}
