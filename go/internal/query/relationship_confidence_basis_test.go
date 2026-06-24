// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestRelationshipConfidenceBasis(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		row  map[string]any
		want string
	}{
		{
			name: "assertion override",
			row: map[string]any{
				"confidence":        1.0,
				"resolution_source": "assertion",
				"evidence_count":    1,
			},
			want: "assertion_override",
		},
		{
			name: "aggregate evidence",
			row: map[string]any{
				"confidence":        0.92,
				"resolution_source": "inferred",
				"evidence_count":    2,
			},
			want: "evidence_aggregate",
		},
		{
			name: "single evidence",
			row: map[string]any{
				"confidence":     0.84,
				"evidence_count": 1,
			},
			want: "evidence_constant",
		},
		{
			name: "evidence type fallback",
			row: map[string]any{
				"confidence":    0.82,
				"evidence_type": "terraform_app_repo",
			},
			want: "evidence_constant",
		},
		{
			name: "missing evidence metadata",
			row:  map[string]any{},
		},
		{
			name: "legacy evidence type without confidence",
			row: map[string]any{
				"evidence_type": "terraform_app_repo",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := relationshipConfidenceBasis(test.row); got != test.want {
				t.Fatalf("relationshipConfidenceBasis() = %q, want %q", got, test.want)
			}
		})
	}
}
