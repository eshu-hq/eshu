// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func selectionTestRow(state string, evaluatedAt, stateSince time.Time, cycleCount int) SelectionObservationRow {
	return SelectionObservationRow{
		SelectorID:            "sel_test",
		State:                 state,
		EvaluatedAt:           evaluatedAt,
		LivenessWindowSeconds: 48 * 3600,
		StateSince:            stateSince,
		StateCycleCount:       cycleCount,
	}
}

func TestComputeRepositorySelectionStateUnknownWithoutRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	selection := ComputeRepositorySelectionState(nil, time.Time{}, now)
	if selection.State != RepositorySelectionUnknown {
		t.Fatalf("State = %q, want %q", selection.State, RepositorySelectionUnknown)
	}
	if selection.Reason != RepositorySelectionReasonNoLiveObservations {
		t.Fatalf("Reason = %q, want %q", selection.Reason, RepositorySelectionReasonNoLiveObservations)
	}
	if selection.LiveSelectorCount != 0 {
		t.Fatalf("LiveSelectorCount = %d, want 0", selection.LiveSelectorCount)
	}
}

func TestComputeRepositorySelectionStateUnknownWhenLivenessLapses(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	// A confirmed exclusion whose row lapsed past its window reads unknown:
	// a stalled collector fails open instead of condemning repositories it
	// stopped looking at.
	stale := selectionTestRow(scope.SelectionStateNotListed, now.Add(-49*time.Hour), now.Add(-72*time.Hour), 12)
	selection := ComputeRepositorySelectionState([]SelectionObservationRow{stale}, time.Time{}, now)
	if selection.State != RepositorySelectionUnknown {
		t.Fatalf("State = %q, want %q", selection.State, RepositorySelectionUnknown)
	}
}

func TestComputeRepositorySelectionStateSelectedBeatsExclusion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	selected := selectionTestRow(scope.SelectionStateSelected, now, now.Add(-time.Hour), 3)
	selected.SelectorID = "sel_a"
	excluded := selectionTestRow(scope.SelectionStateNotListed, now, now.Add(-time.Hour), 3)
	excluded.SelectorID = "sel_b"
	selection := ComputeRepositorySelectionState([]SelectionObservationRow{excluded, selected}, time.Time{}, now)
	if selection.State != RepositorySelectionSelected {
		t.Fatalf("State = %q, want %q", selection.State, RepositorySelectionSelected)
	}
	if selection.LiveSelectorCount != 2 {
		t.Fatalf("LiveSelectorCount = %d, want 2", selection.LiveSelectorCount)
	}
}

func TestComputeRepositorySelectionStatePendingWithoutConfirmation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	// Seen twice but only a minute apart: the five-minute gap is not met.
	unconfirmed := selectionTestRow(scope.SelectionStateNotListed, now, now.Add(-time.Minute), 2)
	selection := ComputeRepositorySelectionState([]SelectionObservationRow{unconfirmed}, time.Time{}, now)
	if selection.State != RepositorySelectionPendingConfirmation {
		t.Fatalf("State = %q, want %q", selection.State, RepositorySelectionPendingConfirmation)
	}
	if selection.Reason != RepositorySelectionReasonUnconfirmedExclusion {
		t.Fatalf("Reason = %q, want %q", selection.Reason, RepositorySelectionReasonUnconfirmedExclusion)
	}
}

func TestComputeRepositorySelectionStatePendingOnFirstSight(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	firstSight := selectionTestRow(scope.SelectionStateArchivedExcluded, now, now, 1)
	selection := ComputeRepositorySelectionState([]SelectionObservationRow{firstSight}, time.Time{}, now)
	if selection.State != RepositorySelectionPendingConfirmation {
		t.Fatalf("State = %q, want %q", selection.State, RepositorySelectionPendingConfirmation)
	}
}

func TestComputeRepositorySelectionStateNotSelectedWhenConfirmed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	since := now.Add(-26 * time.Hour)
	confirmed := selectionTestRow(scope.SelectionStateNotListed, now, since, 2)
	confirmed.LastListedAt = since.Add(-time.Hour)
	// The last generation predates the exclusion: nothing was ingested after
	// the repository left the listing.
	selection := ComputeRepositorySelectionState([]SelectionObservationRow{confirmed}, since.Add(-time.Hour), now)
	if selection.State != RepositorySelectionNotSelected {
		t.Fatalf("State = %q, want %q", selection.State, RepositorySelectionNotSelected)
	}
	if selection.Reason != RepositorySelectionReasonConfirmedExclusion {
		t.Fatalf("Reason = %q, want %q", selection.Reason, RepositorySelectionReasonConfirmedExclusion)
	}
	if !selection.StateSince.Equal(since) {
		t.Fatalf("StateSince = %v, want %v", selection.StateSince, since)
	}
	if !selection.EvaluatedAt.Equal(now) {
		t.Fatalf("EvaluatedAt = %v, want %v", selection.EvaluatedAt, now)
	}
	if !selection.LastListedAt.Equal(since.Add(-time.Hour)) {
		t.Fatalf("LastListedAt = %v, want %v", selection.LastListedAt, since.Add(-time.Hour))
	}
}

func TestComputeRepositorySelectionStateStillIngestedAfterStateSince(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	since := now.Add(-26 * time.Hour)
	confirmed := selectionTestRow(scope.SelectionStateRuleExcluded, now, since, 4)
	// A generation observed after the exclusion began: something is still
	// ingesting the repository despite every live selector excluding it.
	selection := ComputeRepositorySelectionState([]SelectionObservationRow{confirmed}, since.Add(time.Hour), now)
	if selection.State != RepositorySelectionExcludedStillIngested {
		t.Fatalf("State = %q, want %q", selection.State, RepositorySelectionExcludedStillIngested)
	}
	if selection.Reason != RepositorySelectionReasonGenerationObservedAfterStateSince {
		t.Fatalf("Reason = %q, want %q", selection.Reason, RepositorySelectionReasonGenerationObservedAfterStateSince)
	}
}

func TestComputeRepositoryFreshnessVerdictNotSelectedPrecedence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	resolved := RepositoryFreshnessSnapshot{
		RepositoryID:   "repo-1",
		ScopeID:        "scope-1",
		Resolved:       true,
		ScopeKind:      "repository",
		HasGeneration:  true,
		Generation:     RepositoryFreshnessGeneration{ID: "gen-1", Status: "active"},
		ObservedCommit: "abc123",
		ObservedAt:     now,
		Stages:         RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: true},
		Selection:      RepositorySelection{State: RepositorySelectionNotSelected, Reason: RepositorySelectionReasonConfirmedExclusion},
	}

	if verdict := ComputeRepositoryFreshnessVerdict(resolved, ""); verdict != RepositoryFreshnessNotSelected {
		t.Fatalf("verdict = %q, want %q", verdict, RepositoryFreshnessNotSelected)
	}

	// not_selected yields to unknown: without a resolved scope there is no
	// selection evidence to report.
	unresolved := resolved
	unresolved.Resolved = false
	if verdict := ComputeRepositoryFreshnessVerdict(unresolved, ""); verdict != RepositoryFreshnessUnknown {
		t.Fatalf("unresolved verdict = %q, want %q", verdict, RepositoryFreshnessUnknown)
	}

	// not_selected beats unobserved: a queued push for a repository no
	// selector lists will never be built, so the verdict names the blocker
	// instead of promising imminent progress.
	queued := resolved
	queued.UnobservedPush = &RepositoryFreshnessUnobservedPush{TargetSHA: "def456", Ref: "main", ReceivedAt: now}
	if verdict := ComputeRepositoryFreshnessVerdict(queued, ""); verdict != RepositoryFreshnessNotSelected {
		t.Fatalf("queued-push verdict = %q, want %q", verdict, RepositoryFreshnessNotSelected)
	}

	// Any other selection state leaves the verdict alone.
	selected := resolved
	selected.Selection = RepositorySelection{State: RepositorySelectionSelected, Reason: RepositorySelectionReasonLiveSelectedRow}
	if verdict := ComputeRepositoryFreshnessVerdict(selected, ""); verdict != RepositoryFreshnessCurrent {
		t.Fatalf("selected verdict = %q, want %q", verdict, RepositoryFreshnessCurrent)
	}
}
