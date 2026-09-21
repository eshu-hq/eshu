// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// refreshGateProbeRunner is a fake affected.Runner: canned rows drive
// suppression/emission for the IAM emit-gate telemetry proof.
type refreshGateProbeRunner struct {
	rows []map[string]any
}

func (r *refreshGateProbeRunner) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	return r.rows, nil
}

func refreshGatePoint(t *testing.T, reader *sdkmetric.ManualReader, attrs map[string]string) (int64, bool) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
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

// TestIAMRefreshGateEvaluationEmitsCounter pins the F2 close: the IAM
// producer's gate evaluation records one
// eshu_dp_value_flow_refresh_gate_evaluations_total point labeled by the IAM
// domain and outcome, exactly like the three reducer-root producers.
func TestIAMRefreshGateEvaluationEmitsCounter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	intent := reducercontract.Intent{ScopeID: "telemetry-proof", GenerationID: "genesis"}
	edges := []map[string]any{{"principal_uid": "telemetry-principal"}}

	for _, tc := range []struct {
		name    string
		rows    []map[string]any
		outcome string
	}{
		{"suppressed on empty graph read", nil, "suppressed"},
		{"affected on positive graph read", []map[string]any{{"repo_id": "repo-telemetry"}}, "affected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			inst, err := telemetry.NewInstruments(provider.Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			handler := IAMCanPerformMaterializationHandler{
				Instruments:   inst,
				AffectedGraph: &refreshGateProbeRunner{rows: tc.rows},
			}
			_ = handler.refreshResultSignals(ctx, intent, nil, 1, edges)
			value, ok := refreshGatePoint(t, reader, map[string]string{
				"domain":  string(reducercontract.DomainIAMCanPerformMaterialization),
				"outcome": tc.outcome,
			})
			if !ok || value != 1 {
				t.Errorf("counter point {domain, outcome=%s} = (%d, %v), want (1, true)", tc.outcome, value, ok)
			}
		})
	}
}
