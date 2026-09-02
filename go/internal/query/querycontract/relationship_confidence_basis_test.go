// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "testing"

// TestRelationshipConfidenceBasisEvidenceKinds covers the branch package query's
// own test does not reach: a row carrying evidence_kinds but neither an
// evidence_count nor an evidence_type. Kinds arrive from graph reads that
// project the kind list without the count, and a row that lost that branch
// would report no basis at all rather than a constant one.
func TestRelationshipConfidenceBasisEvidenceKinds(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"confidence":     0.6,
		"evidence_kinds": []string{"terraform_app_repo"},
	}
	if got := RelationshipConfidenceBasis(row); got != "evidence_constant" {
		t.Fatalf("RelationshipConfidenceBasis() = %q, want %q", got, "evidence_constant")
	}
}

// TestRelationshipConfidenceBasisAssertionOutranksEvidence pins the precedence
// the exported godoc states: a row a human or policy asserted reports
// assertion_override even when it also carries enough evidence to aggregate.
// Reversing the two branches would relabel every asserted correlation as
// inferred, which callers compare across responses.
func TestRelationshipConfidenceBasisAssertionOutranksEvidence(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"confidence":        0.99,
		"resolution_source": "ASSERTION",
		"evidence_count":    9,
		"evidence_type":     "terraform_app_repo",
	}
	if got := RelationshipConfidenceBasis(row); got != "assertion_override" {
		t.Fatalf("RelationshipConfidenceBasis() = %q, want %q", got, "assertion_override")
	}
}

// TestAddRelationshipConfidenceBasis covers the writer rather than the rule.
// It must never overwrite a basis a caller already set, and must leave a row
// alone when the rule yields nothing, because writing an empty confidence_basis
// key would tell a caller the field was computed and found absent.
func TestAddRelationshipConfidenceBasis(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		row    map[string]any
		want   string
		absent bool
	}{
		{
			name: "computes a missing basis",
			row:  map[string]any{"confidence": 0.9, "evidence_count": 3},
			want: "evidence_aggregate",
		},
		{
			name: "keeps a basis the caller already set",
			row: map[string]any{
				"confidence":       0.9,
				"evidence_count":   3,
				"confidence_basis": "caller_supplied",
			},
			want: "caller_supplied",
		},
		{
			name: "treats a blank basis as unset",
			row: map[string]any{
				"confidence":       0.9,
				"evidence_count":   3,
				"confidence_basis": "   ",
			},
			want: "evidence_aggregate",
		},
		{
			name:   "writes nothing when the rule yields nothing",
			row:    map[string]any{"confidence": 0.0},
			absent: true,
		},
		{
			name:   "no-ops on an empty row",
			row:    map[string]any{},
			absent: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			AddRelationshipConfidenceBasis(test.row)
			got, ok := test.row["confidence_basis"]
			if test.absent {
				if ok {
					t.Fatalf("confidence_basis = %v, want the key to stay absent", got)
				}
				return
			}
			if !ok {
				t.Fatalf("confidence_basis is absent, want %q", test.want)
			}
			if got != test.want {
				t.Fatalf("confidence_basis = %v, want %q", got, test.want)
			}
		})
	}
}

// TestAddRelationshipConfidenceBasisToleratesNilRow pins that a nil row does
// not panic. Writing to a nil map panics in Go, and this is the only caller
// shape where that is reachable.
//
// What keeps it safe today is not the len(row) == 0 short circuit -- deleting
// that line leaves this test green, because a nil row yields no confidence and
// so the rule returns "" and the write never runs. The short circuit is a fast
// path. This test guards the write itself: make it unconditional and the nil
// case panics here.
func TestAddRelationshipConfidenceBasisToleratesNilRow(t *testing.T) {
	t.Parallel()

	AddRelationshipConfidenceBasis(nil)
}
