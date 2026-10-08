// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
)

var (
	verdictNow      = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	verdictInterval = 5 * time.Minute
)

// selectionFor runs the production aggregation over rows at verdictNow, so
// these cases exercise the same liveness and confirmation rules the reader
// applies. It returns nil when no row is live, as the reader does.
func selectionFor(rows ...selection.Observation) *RepositoryFreshnessSelection {
	summary, ok := selection.Summarize(rows, verdictNow)
	if !ok {
		return nil
	}
	return &summary
}

func liveObservation(selectorID string, state selection.State) selection.Observation {
	return selection.Observation{
		SelectorID:         selectorID,
		State:              state,
		LastListedAt:       verdictNow,
		EvaluatedAt:        verdictNow,
		EvaluationInterval: verdictInterval,
	}
}

func unlistedObservation(selectorID string, cycles int, firstUnlisted time.Time) selection.Observation {
	return selection.Observation{
		SelectorID:         selectorID,
		State:              selection.StateNotListed,
		LastListedAt:       firstUnlisted.Add(-verdictInterval),
		FirstUnlistedAt:    firstUnlisted,
		UnlistedCycleCount: cycles,
		EvaluatedAt:        verdictNow,
		EvaluationInterval: verdictInterval,
	}
}

func staleObservation(selectorID string, state selection.State) selection.Observation {
	row := liveObservation(selectorID, state)
	row.EvaluatedAt = verdictNow.Add(-4 * verdictInterval)
	return row
}

// TestComputeRepositoryFreshnessVerdictSelection covers the #7625
// not_selected verdict at precedence 2: after unknown, before unobserved,
// behind, building, and current. Selection evidence only changes the verdict
// when every live selector row is settled exclusion evidence.
func TestComputeRepositoryFreshnessVerdictSelection(t *testing.T) {
	t.Parallel()

	drained := RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: true}
	built := func(sel *RepositoryFreshnessSelection) RepositoryFreshnessSnapshot {
		return RepositoryFreshnessSnapshot{
			Resolved:       true,
			ScopeKind:      "repository",
			HasGeneration:  true,
			Generation:     RepositoryFreshnessGeneration{ID: "gen-1", Status: "active", TriggerKind: "push"},
			ObservedCommit: "abc123",
			Stages:         drained,
			Selection:      sel,
		}
	}
	confirmed := unlistedObservation("sel-a", 2, verdictNow.Add(-verdictInterval))
	pending := unlistedObservation("sel-a", 1, verdictNow)
	push := &RepositoryFreshnessUnobservedPush{TargetSHA: "def456"}

	tests := []struct {
		name           string
		snapshot       RepositoryFreshnessSnapshot
		expectedCommit string
		want           RepositoryFreshnessVerdict
	}{
		{name: "no live rows keeps current", snapshot: built(selectionFor()), want: RepositoryFreshnessCurrent},
		{
			name: "no live rows keeps unobserved",
			snapshot: func() RepositoryFreshnessSnapshot {
				s := built(nil)
				s.UnobservedPush = push
				return s
			}(),
			want: RepositoryFreshnessUnobserved,
		},
		{name: "no live rows keeps behind", snapshot: built(nil), expectedCommit: "def456", want: RepositoryFreshnessBehind},
		{
			name: "no live rows keeps building",
			snapshot: func() RepositoryFreshnessSnapshot {
				s := built(nil)
				s.Stages.Projected = false
				return s
			}(),
			want: RepositoryFreshnessBuilding,
		},
		{
			name:     "stale selector rows only are ignored",
			snapshot: built(selectionFor(staleObservation("sel-a", selection.StateArchivedExcluded), staleObservation("sel-b", selection.StateNotListed))),
			want:     RepositoryFreshnessCurrent,
		},
		{name: "live selected keeps current", snapshot: built(selectionFor(liveObservation("sel-a", selection.StateSelected))), want: RepositoryFreshnessCurrent},
		{name: "one pending unlisted cycle is not not_selected", snapshot: built(selectionFor(pending)), want: RepositoryFreshnessCurrent},
		{name: "confirmed unlisted is not_selected", snapshot: built(selectionFor(confirmed)), want: RepositoryFreshnessNotSelected},
		{
			name:     "archived exclusion is not_selected immediately",
			snapshot: built(selectionFor(liveObservation("sel-a", selection.StateArchivedExcluded))),
			want:     RepositoryFreshnessNotSelected,
		},
		{
			name:     "rule exclusion is not_selected immediately",
			snapshot: built(selectionFor(liveObservation("sel-a", selection.StateRuleExcluded))),
			want:     RepositoryFreshnessNotSelected,
		},
		{
			name:     "one live selected selector beside a live excluded selector is unaffected",
			snapshot: built(selectionFor(liveObservation("sel-a", selection.StateSelected), liveObservation("sel-b", selection.StateRuleExcluded))),
			want:     RepositoryFreshnessCurrent,
		},
		{
			name:     "stale selected row does not mask a live exclusion",
			snapshot: built(selectionFor(staleObservation("sel-a", selection.StateSelected), confirmed)),
			want:     RepositoryFreshnessNotSelected,
		},
		{
			name: "unresolved repository stays unknown",
			snapshot: RepositoryFreshnessSnapshot{
				Resolved:  false,
				Selection: selectionFor(confirmed),
			},
			want: RepositoryFreshnessUnknown,
		},
		{
			name: "non-git scope without a commit stays unknown",
			snapshot: func() RepositoryFreshnessSnapshot {
				s := built(selectionFor(confirmed))
				s.ScopeKind, s.ObservedCommit = "aws_account", ""
				return s
			}(),
			want: RepositoryFreshnessUnknown,
		},
		{
			name: "not_selected outranks unobserved",
			snapshot: func() RepositoryFreshnessSnapshot {
				s := built(selectionFor(confirmed))
				s.UnobservedPush = push
				return s
			}(),
			want: RepositoryFreshnessNotSelected,
		},
		{name: "not_selected outranks behind", snapshot: built(selectionFor(confirmed)), expectedCommit: "def456", want: RepositoryFreshnessNotSelected},
		{
			name: "not_selected outranks building",
			snapshot: func() RepositoryFreshnessSnapshot {
				s := built(selectionFor(confirmed))
				s.SharedEnrichment.Pending = true
				return s
			}(),
			want: RepositoryFreshnessNotSelected,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ComputeRepositoryFreshnessVerdict(tt.snapshot, tt.expectedCommit); got != tt.want {
				t.Fatalf("ComputeRepositoryFreshnessVerdict() = %q, want %q", got, tt.want)
			}
		})
	}
}
