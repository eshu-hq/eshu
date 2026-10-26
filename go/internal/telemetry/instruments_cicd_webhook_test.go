// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestCICDWebhookSignalsAreRegistered(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := NewInstruments(provider.Meter("cicd-webhook-contract"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	instruments.CICDRunWebhookLag.Record(t.Context(), 2.5)
	instruments.CICDRunWebhookHandoffs.Add(t.Context(), 1)
	instruments.CICDRunPollFallback.Add(t.Context(), 1)

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	want := map[string]bool{
		"eshu_dp_ci_cd_run_webhook_lag_seconds":    false,
		"eshu_dp_ci_cd_run_webhook_handoffs_total": false,
		"eshu_dp_ci_cd_run_poll_fallback_total":    false,
	}
	for _, scope := range metrics.ScopeMetrics {
		for _, measured := range scope.Metrics {
			if _, ok := want[measured.Name]; ok {
				want[measured.Name] = true
			}
		}
	}
	for name, registered := range want {
		if !registered {
			t.Errorf("metric %q was not emitted", name)
		}
	}
}
