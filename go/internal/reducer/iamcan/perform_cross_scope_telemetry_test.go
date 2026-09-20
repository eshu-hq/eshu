// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestIAMCanPerformUnregisteredTargetScopeTelemetry proves an operator can see
// both halves of the unregistered-scope wait: a deferral counts
// scope_unregistered plus a readiness wait outcome=deferred, and an expired
// bound counts abandoned plus outcome=abandoned.
func TestIAMCanPerformUnregisteredTargetScopeTelemetry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		cycle      time.Time
		wantTarget string
		wantWait   string
	}{
		{name: "inside bound", cycle: time.Now(), wantTarget: crossScopeTargetScopeUnregistered, wantWait: crossscope.ReadinessWaitDeferred},
		{name: "bound expired", cycle: time.Now().Add(-crossscope.ProducerReadinessMaxWait - time.Minute), wantTarget: crossScopeTargetAbandoned, wantWait: crossscope.ReadinessWaitAbandoned},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			instruments, err := telemetry.NewInstruments(provider.Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments: %v", err)
			}
			handler := crossScopeHandler(&fakeCrossScopeTargets{}, &recordingIAMCanPerformWriter{})
			handler.Instruments = instruments
			intent := crossScopeIntent()
			intent.CycleStartedAt = tc.cycle
			_, _ = handler.Handle(context.Background(), intent)

			var rm metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &rm); err != nil {
				t.Fatalf("Collect: %v", err)
			}
			if !metricHasAttrs(rm, "eshu_dp_iam_can_perform_cross_scope_targets_total", map[string]string{"outcome": tc.wantTarget}) {
				t.Fatalf("cross-scope targets outcome=%s not recorded", tc.wantTarget)
			}
			if !metricHasAttrs(rm, "eshu_dp_reducer_readiness_waits_total", map[string]string{
				"domain": "iam_can_perform_materialization", "outcome": tc.wantWait,
			}) {
				t.Fatalf("readiness wait outcome=%s not recorded", tc.wantWait)
			}
		})
	}
}
