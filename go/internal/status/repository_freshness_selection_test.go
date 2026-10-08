// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
)

var (
	verdictNow    = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	verdictWindow = 48 * time.Hour
	// verdictSince is when every excluded fixture row entered its state.
	verdictSince = verdictNow.Add(-time.Hour)
)

// selectionFor runs the production aggregation over rows at verdictNow with
// the scope's newest generation observed at latest, so these cases exercise
// the same liveness, confirmation, and rule (d) checks the reader applies.
func selectionFor(t *testing.T, latest time.Time, rows ...selection.Observation) RepositoryFreshnessSelection {
	t.Helper()
	summary, err := selection.Summarize(rows, verdictNow, func() (time.Time, error) { return latest, nil })
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	return summary
}

func observation(selectorID string, state selection.State, cycles int) selection.Observation {
	return selection.Observation{
		SelectorID:      selectorID,
		State:           state,
		LastListedAt:    verdictSince,
		StateSince:      verdictSince,
		StateCycleCount: cycles,
		EvaluatedAt:     verdictNow,
		LivenessWindow:  verdictWindow,
	}
}

func expired(o selection.Observation) selection.Observation {
	o.EvaluatedAt = verdictNow.Add(-verdictWindow - time.Second)
	return o
}

// TestComputeRepositoryFreshnessVerdictSelection covers the #7625
// not_selected verdict at precedence 2: after unknown, before unobserved,
// behind, building, and current. Only selection state not_selected changes
// the verdict; unknown, selected, pending_confirmation, and
// excluded_still_ingested all fall through.
func TestComputeRepositoryFreshnessVerdictSelection(t *testing.T) {
	t.Parallel()

	drained := RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: true}
	built := func(sel RepositoryFreshnessSelection) RepositoryFreshnessSnapshot {
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
	before := verdictSince.Add(-time.Hour)
	unknown := selectionFor(t, before)
	confirmed := selectionFor(t, before, observation("sel-a", selection.StateNotListed, 2))
	push := &RepositoryFreshnessUnobservedPush{TargetSHA: "def456"}

	tests := []struct {
		name           string
		snapshot       RepositoryFreshnessSnapshot
		expectedCommit string
		want           RepositoryFreshnessVerdict
	}{
		{name: "no live rows keeps current", snapshot: built(unknown), want: RepositoryFreshnessCurrent},
		{name: "a read that stopped before selection keeps current", snapshot: built(RepositoryFreshnessSelection{}), want: RepositoryFreshnessCurrent},
		{
			name: "no live rows keeps unobserved",
			snapshot: func() RepositoryFreshnessSnapshot {
				s := built(unknown)
				s.UnobservedPush = push
				return s
			}(),
			want: RepositoryFreshnessUnobserved,
		},
		{name: "no live rows keeps behind", snapshot: built(unknown), expectedCommit: "def456", want: RepositoryFreshnessBehind},
		{
			name: "no live rows keeps building",
			snapshot: func() RepositoryFreshnessSnapshot {
				s := built(unknown)
				s.Stages.Projected = false
				return s
			}(),
			want: RepositoryFreshnessBuilding,
		},
		{
			name: "expired selector rows read unknown",
			snapshot: built(selectionFor(t, before,
				expired(observation("sel-a", selection.StateArchivedExcluded, 9)),
				expired(observation("sel-b", selection.StateNotListed, 9)))),
			want: RepositoryFreshnessCurrent,
		},
		{name: "live selected keeps current", snapshot: built(selectionFor(t, before, observation("sel-a", selection.StateSelected, 3))), want: RepositoryFreshnessCurrent},
		{name: "one unlisted cycle is pending", snapshot: built(selectionFor(t, before, observation("sel-a", selection.StateNotListed, 1))), want: RepositoryFreshnessCurrent},
		{name: "confirmed unlisted is not_selected", snapshot: built(confirmed), want: RepositoryFreshnessNotSelected},
		{
			name:     "a first archived exclusion is pending, not immediate",
			snapshot: built(selectionFor(t, before, observation("sel-a", selection.StateArchivedExcluded, 1))),
			want:     RepositoryFreshnessCurrent,
		},
		{
			name:     "confirmed archived exclusion is not_selected",
			snapshot: built(selectionFor(t, before, observation("sel-a", selection.StateArchivedExcluded, 2))),
			want:     RepositoryFreshnessNotSelected,
		},
		{
			name: "explicit selected beside a confirmed org rule exclusion keeps current",
			snapshot: built(selectionFor(t, before,
				observation("explicit@token:abc", selection.StateSelected, 5),
				observation("org@app:1:2", selection.StateRuleExcluded, 5))),
			want: RepositoryFreshnessCurrent,
		},
		{
			name:     "a generation observed after the exclusion keeps current",
			snapshot: built(selectionFor(t, verdictSince.Add(time.Microsecond), observation("sel-a", selection.StateRuleExcluded, 2))),
			want:     RepositoryFreshnessCurrent,
		},
		{
			name:     "a generation observed exactly at the exclusion start is not_selected",
			snapshot: built(selectionFor(t, verdictSince, observation("sel-a", selection.StateRuleExcluded, 2))),
			want:     RepositoryFreshnessNotSelected,
		},
		{
			name: "expired selected row does not mask a live exclusion",
			snapshot: built(selectionFor(t, before,
				expired(observation("sel-a", selection.StateSelected, 3)),
				observation("sel-b", selection.StateNotListed, 2))),
			want: RepositoryFreshnessNotSelected,
		},
		{
			name:     "unresolved repository stays unknown",
			snapshot: RepositoryFreshnessSnapshot{Resolved: false, Selection: confirmed},
			want:     RepositoryFreshnessUnknown,
		},
		{
			name: "non-git scope without a commit stays unknown",
			snapshot: func() RepositoryFreshnessSnapshot {
				s := built(confirmed)
				s.ScopeKind, s.ObservedCommit = "aws_account", ""
				return s
			}(),
			want: RepositoryFreshnessUnknown,
		},
		{
			name: "not_selected outranks unobserved",
			snapshot: func() RepositoryFreshnessSnapshot {
				s := built(confirmed)
				s.UnobservedPush = push
				return s
			}(),
			want: RepositoryFreshnessNotSelected,
		},
		{name: "not_selected outranks behind", snapshot: built(confirmed), expectedCommit: "def456", want: RepositoryFreshnessNotSelected},
		{
			name: "not_selected outranks building",
			snapshot: func() RepositoryFreshnessSnapshot {
				s := built(confirmed)
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
				t.Fatalf("ComputeRepositoryFreshnessVerdict() = %q, want %q (selection %+v)", got, tt.want, tt.snapshot.Selection)
			}
		})
	}
}
