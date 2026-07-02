// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
)

func TestDeployableUnitCorrelationReportsSubDurationsAndSignals(t *testing.T) {
	t.Parallel()

	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: deployableUnitCorrelationEnvelopes(
				"repo-api",
				"api",
				[]map[string]any{
					{
						"repo_id":       "repo-api",
						"language":      "dockerfile",
						"relative_path": "Dockerfile",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{
								map[string]any{"name": "runtime"},
							},
						},
					},
				},
			),
		},
		PhasePublisher: &recordingGraphProjectionPhasePublisher{},
	}

	result, err := handler.Handle(context.Background(), deployableUnitIntent("api"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	for _, key := range []string{
		"load_facts",
		"extract_candidates",
		"load_resolved",
		"apply_resolved",
		"filter_candidates",
		"evaluate_candidates",
		"edge_materialize",
		"edge_retract",
		"edge_write",
		"admission_decisions",
		"phase_publish",
		"total",
	} {
		if _, ok := result.SubDurations[key]; !ok {
			t.Fatalf("SubDurations missing %q: %#v", key, result.SubDurations)
		}
	}
	for key, want := range map[string]float64{
		"fact_count":           2,
		"raw_candidate_count":  1,
		"candidate_count":      1,
		"evaluated_candidates": 1,
		"edge_rows":            1,
		"retract_rows":         0,
		"write_rows":           0,
		"canonical_writes":     0,
	} {
		if got := result.SubSignals[key]; got != want {
			t.Fatalf("SubSignals[%q] = %v, want %v; signals=%#v", key, got, want, result.SubSignals)
		}
	}
}
