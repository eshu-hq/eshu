// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/value/affected"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func collectRefreshGatePoints(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return rm
}

func refreshGateValue(rm metricdata.ResourceMetrics, attrs map[string]string) (int64, bool) {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_value_flow_refresh_gate_evaluations_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				match := true
				for k, want := range attrs {
					found := false
					for _, attr := range dp.Attributes.ToSlice() {
						if string(attr.Key) == k && attr.Value.AsString() == want {
							found = true
							break
						}
					}
					if !found {
						match = false
						break
					}
				}
				if match {
					return dp.Value, true
				}
			}
		}
	}
	return 0, false
}

// TestRefreshGateEvaluationEmitsCounter pins P1-2: every gate evaluation
// records one eshu_dp_value_flow_refresh_gate_evaluations_total point labeled
// by producer domain and outcome (affected/suppressed/fail_open).
func TestRefreshGateEvaluationEmitsCounter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	intent := Intent{ScopeID: "telemetry-proof", GenerationID: "genesis"}

	for _, tc := range []struct {
		name    string
		rows    []map[string]any
		wired   bool
		outcome string
	}{
		{"suppressed on empty graph read", nil, true, "suppressed"},
		{"affected on positive graph read", []map[string]any{{"repo_id": "repo-telemetry"}}, true, "affected"},
		{"fail open unwired", nil, false, "fail_open"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			inst, err := telemetry.NewInstruments(provider.Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			handler := WorkloadMaterializationHandler{Instruments: inst}
			if tc.wired {
				handler.AffectedGraph = &wiringProbeRunner{rows: tc.rows}
			}
			got := handler.refreshResultSignals(ctx, intent, nil, 2, []string{"repo-telemetry"})
			_ = got
			rm := collectRefreshGatePoints(t, reader)
			value, ok := refreshGateValue(rm, map[string]string{
				"domain":  string(DomainWorkloadMaterialization),
				"outcome": tc.outcome,
			})
			if !ok || value != 1 {
				t.Errorf("counter point {domain, outcome=%s} = (%d, %v), want (1, true)", tc.outcome, value, ok)
			}
		})
	}
}

// TestRefreshGateEvaluationToleratesNilInstruments pins the nil-guard: a
// handler without instruments still gates (fail-open or graph answer) without
// panicking.
func TestRefreshGateEvaluationToleratesNilInstruments(t *testing.T) {
	t.Parallel()
	handler := WorkloadMaterializationHandler{AffectedGraph: &wiringProbeRunner{}}
	signals := handler.refreshResultSignals(context.Background(), Intent{}, nil, 2, []string{"repo-nil"})
	if signals[affected.RefreshAffectedReposSignal] != 0 {
		t.Errorf("nil-instruments signal = %v, want 0 on empty graph read", signals[affected.RefreshAffectedReposSignal])
	}
}
