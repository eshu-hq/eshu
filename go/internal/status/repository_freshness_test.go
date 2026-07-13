// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import "testing"

// TestComputeRepositoryFreshnessVerdict exercises every branch of the
// verdict precedence documented on ComputeRepositoryFreshnessVerdict,
// including the empty-observed-commit honesty cases (#5143) and the
// shared-enrichment-only building case that must never be attributed to the
// repo's own stages.
func TestComputeRepositoryFreshnessVerdict(t *testing.T) {
	t.Parallel()

	baseGeneration := RepositoryFreshnessGeneration{ID: "gen-1", Status: "active", TriggerKind: "push"}

	tests := []struct {
		name           string
		snapshot       RepositoryFreshnessSnapshot
		expectedCommit string
		want           RepositoryFreshnessVerdict
	}{
		{
			name:     "unresolved repository has no scope at all",
			snapshot: RepositoryFreshnessSnapshot{Resolved: false},
			want:     RepositoryFreshnessUnknown,
		},
		{
			name: "resolved scope with no generation",
			snapshot: RepositoryFreshnessSnapshot{
				Resolved:      true,
				ScopeKind:     "repository",
				HasGeneration: false,
			},
			want: RepositoryFreshnessUnknown,
		},
		{
			name: "non-git scope with empty commit is unknown even though stages are drained",
			snapshot: RepositoryFreshnessSnapshot{
				Resolved:       true,
				ScopeKind:      "aws_account",
				HasGeneration:  true,
				Generation:     baseGeneration,
				ObservedCommit: "",
				Stages:         RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: true},
			},
			want: RepositoryFreshnessUnknown,
		},
		{
			name: "git scope with empty commit (pre-delta-baseline) still computes current honestly",
			snapshot: RepositoryFreshnessSnapshot{
				Resolved:       true,
				ScopeKind:      "repository",
				HasGeneration:  true,
				Generation:     baseGeneration,
				ObservedCommit: "",
				Stages:         RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: true},
			},
			want: RepositoryFreshnessCurrent,
		},
		{
			name: "queued webhook push not yet observed takes precedence over drained stages",
			snapshot: RepositoryFreshnessSnapshot{
				Resolved:       true,
				ScopeKind:      "repository",
				HasGeneration:  true,
				Generation:     baseGeneration,
				ObservedCommit: "abc123",
				Stages:         RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: true},
				UnobservedPush: &RepositoryFreshnessUnobservedPush{TargetSHA: "def456"},
			},
			want: RepositoryFreshnessUnobserved,
		},
		{
			name: "expected commit mismatch while generation is still progressing is behind",
			snapshot: RepositoryFreshnessSnapshot{
				Resolved:       true,
				ScopeKind:      "repository",
				HasGeneration:  true,
				Generation:     baseGeneration,
				ObservedCommit: "abc123",
				Stages:         RepositoryFreshnessStages{Collected: true, Reduced: false, Projected: false},
			},
			expectedCommit: "def456",
			want:           RepositoryFreshnessBehind,
		},
		{
			name: "expected commit mismatch while everything else is idle is still behind, never fabricated current",
			snapshot: RepositoryFreshnessSnapshot{
				Resolved:       true,
				ScopeKind:      "repository",
				HasGeneration:  true,
				Generation:     baseGeneration,
				ObservedCommit: "abc123",
				Stages:         RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: true},
			},
			expectedCommit: "def456",
			want:           RepositoryFreshnessBehind,
		},
		{
			name: "own reduced stage outstanding is building",
			snapshot: RepositoryFreshnessSnapshot{
				Resolved:       true,
				ScopeKind:      "repository",
				HasGeneration:  true,
				Generation:     baseGeneration,
				ObservedCommit: "abc123",
				Stages:         RepositoryFreshnessStages{Collected: true, Reduced: false, Projected: true, Materialized: true},
			},
			want: RepositoryFreshnessBuilding,
		},
		{
			name: "own projected stage outstanding is building",
			snapshot: RepositoryFreshnessSnapshot{
				Resolved:       true,
				ScopeKind:      "repository",
				HasGeneration:  true,
				Generation:     baseGeneration,
				ObservedCommit: "abc123",
				Stages:         RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: false, Materialized: true},
			},
			want: RepositoryFreshnessBuilding,
		},
		{
			name: "shared enrichment pending only (own stages fully drained) is still building",
			snapshot: RepositoryFreshnessSnapshot{
				Resolved:       true,
				ScopeKind:      "repository",
				HasGeneration:  true,
				Generation:     baseGeneration,
				ObservedCommit: "abc123",
				Stages:         RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: false},
				SharedEnrichment: RepositoryFreshnessSharedEnrichment{
					Pending:        true,
					PendingDomains: []RepositoryFreshnessPendingDomain{{Domain: "deployment_mapping", Count: 3}},
				},
			},
			want: RepositoryFreshnessBuilding,
		},
		{
			name: "fully drained with no expected commit is current",
			snapshot: RepositoryFreshnessSnapshot{
				Resolved:       true,
				ScopeKind:      "repository",
				HasGeneration:  true,
				Generation:     baseGeneration,
				ObservedCommit: "abc123",
				Stages:         RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: true},
			},
			want: RepositoryFreshnessCurrent,
		},
		{
			name: "fully drained with matching expected commit is current",
			snapshot: RepositoryFreshnessSnapshot{
				Resolved:       true,
				ScopeKind:      "repository",
				HasGeneration:  true,
				Generation:     baseGeneration,
				ObservedCommit: "abc123",
				Stages:         RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: true},
			},
			expectedCommit: "abc123",
			want:           RepositoryFreshnessCurrent,
		},
		{
			name: "expected commit is trimmed before comparison",
			snapshot: RepositoryFreshnessSnapshot{
				Resolved:       true,
				ScopeKind:      "repository",
				HasGeneration:  true,
				Generation:     baseGeneration,
				ObservedCommit: "abc123",
				Stages:         RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: true},
			},
			expectedCommit: "  abc123  ",
			want:           RepositoryFreshnessCurrent,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ComputeRepositoryFreshnessVerdict(tt.snapshot, tt.expectedCommit)
			if got != tt.want {
				t.Fatalf("ComputeRepositoryFreshnessVerdict() = %q, want %q", got, tt.want)
			}
		})
	}
}
