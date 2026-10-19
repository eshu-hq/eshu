// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import "testing"

// TestComputeRepositoryFreshnessVerdictRetirementRuleZero proves the #7766
// rule-0 precedence: an open non-complete tombstone answers retiring and a
// complete tombstone answers retired, ahead of every commit, build,
// selection, and unknown rule below. The tombstone is the operator's explicit
// retirement act, so it outranks even a fully drained current read.
func TestComputeRepositoryFreshnessVerdictRetirementRuleZero(t *testing.T) {
	t.Parallel()

	drained := RepositoryFreshnessSnapshot{
		Resolved:       true,
		ScopeKind:      "repository",
		HasGeneration:  true,
		Generation:     RepositoryFreshnessGeneration{ID: "gen-1", Status: "active", TriggerKind: "push"},
		ObservedCommit: "abc123",
		Stages:         RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: true},
	}

	tests := []struct {
		name     string
		snapshot RepositoryFreshnessSnapshot
		want     RepositoryFreshnessVerdict
	}{
		{
			name: "open pending tombstone on a drained repo is retiring, not current",
			snapshot: func() RepositoryFreshnessSnapshot {
				s := drained
				s.Retirement = &RepositoryFreshnessRetirement{State: "pending", RetirementID: "rr_1"}
				return s
			}(),
			want: RepositoryFreshnessRetiring,
		},
		{
			name: "open running tombstone with outstanding work is retiring, not building",
			snapshot: func() RepositoryFreshnessSnapshot {
				s := drained
				s.Stages.Reduced = false
				s.Retirement = &RepositoryFreshnessRetirement{State: "running", RetirementID: "rr_1"}
				return s
			}(),
			want: RepositoryFreshnessRetiring,
		},
		{
			name: "complete tombstone on a drained repo is retired, not current",
			snapshot: func() RepositoryFreshnessSnapshot {
				s := drained
				s.Retirement = &RepositoryFreshnessRetirement{State: "complete", RetirementID: "rr_1"}
				return s
			}(),
			want: RepositoryFreshnessRetired,
		},
		{
			name: "complete tombstone with no resolved scope is retired, not unknown",
			snapshot: RepositoryFreshnessSnapshot{
				RepositoryID: "repo-gone",
				Retirement:   &RepositoryFreshnessRetirement{State: "complete", RetirementID: "rr_1"},
			},
			want: RepositoryFreshnessRetired,
		},
		{
			name:     "no tombstone keeps the drained repo current",
			snapshot: drained,
			want:     RepositoryFreshnessCurrent,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ComputeRepositoryFreshnessVerdict(tt.snapshot, ""); got != tt.want {
				t.Fatalf("ComputeRepositoryFreshnessVerdict() = %q, want %q", got, tt.want)
			}
		})
	}
}
