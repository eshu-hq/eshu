// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestDecideUnchangedGenerationCells(t *testing.T) {
	t.Parallel()
	active := UnchangedGenerationCandidate{GenerationID: "gen-a", FreshnessHint: "hint", SourceCommit: "sha", Status: scope.GenerationStatusActive}
	pending := active
	pending.Status = scope.GenerationStatusPending
	pending.WorkLive = true
	wedged := pending
	wedged.WorkLive = false
	markedActive := active
	markedActive.StreamDropped = true
	markedPending := pending
	markedPending.StreamDropped = true
	snapshot := func(hint, sha, reason string) scope.ScopeGeneration {
		return scope.ScopeGeneration{FreshnessHint: hint, SourceCommitSHA: sha, ReconcileReason: reason}
	}
	cases := []struct {
		name       string
		generation scope.ScopeGeneration
		candidate  UnchangedGenerationCandidate
		found      bool
		wantAction UnchangedGenerationAction
		wantReason string
	}{
		{"no current generation", snapshot("hint", "sha", "interval_elapsed"), UnchangedGenerationCandidate{}, false, UnchangedActionStore, ""},
		{"different hint", snapshot("other", "sha", "interval_elapsed"), active, true, UnchangedActionStore, ""},
		{"different commit", snapshot("hint", "sha-2", ""), active, true, UnchangedActionStore, ""},
		{"active match, ordinary sync", snapshot("hint", "sha", ""), active, true, UnchangedActionSkip, telemetry.RefreshSkipReasonUnchanged},
		{"active match, interval_elapsed", snapshot("hint", "sha", "interval_elapsed"), active, true, UnchangedActionReprojectActive, telemetry.RefreshSkipReasonReconcileReprojected},
		{"active match, graph_dirty", snapshot("hint", "sha", "graph_dirty"), active, true, UnchangedActionReprojectActive, telemetry.RefreshSkipReasonReconcileReprojected},
		{"active match, unspecified reconcile", snapshot("hint", "sha", "unspecified"), active, true, UnchangedActionReprojectActive, telemetry.RefreshSkipReasonReconcileReprojected},
		// Ruling a9-125: an explicit reindex is the operator remedy for a
		// parser change the content-version gate did not catch, so it stores
		// the re-parse even when the identity is unchanged.
		{"active match, fleet reindex", snapshot("hint", "sha", scope.ReconcileReasonReindexRequested), active, true, UnchangedActionStore, ""},
		{"active match, repository reindex", snapshot("hint", "sha", scope.ReconcileReasonRepositoryReindexRequested), active, true, UnchangedActionStore, ""},
		{"active match, stale request", snapshot("hint", "sha", scope.ReconcileReasonRequestStale), active, true, UnchangedActionStore, ""},
		{"pending match, ordinary sync", snapshot("hint", "sha", ""), pending, true, UnchangedActionSkip, telemetry.RefreshSkipReasonPendingSameIdentity},
		{"pending match, in_flight_expired", snapshot("hint", "sha", "in_flight_expired"), pending, true, UnchangedActionSkip, telemetry.RefreshSkipReasonPendingSameIdentity},
		{"pending match, stale request", snapshot("hint", "sha", scope.ReconcileReasonRequestStale), pending, true, UnchangedActionSkip, telemetry.RefreshSkipReasonPendingSameIdentity},
		// Ruling a9-126: a pending match whose projector work is gone cannot
		// activate, so the snapshot is stored and supersedes it.
		{"pending match, work not live", snapshot("hint", "sha", "in_flight_expired"), wedged, true, UnchangedActionStore, ""},
		// Ruling a9-143.20: a candidate whose own fact stream dropped a file is
		// incomplete, so an equal snapshot stores a fresh generation instead of
		// re-opening or skipping it, active or pending alike.
		{"marked active match, interval_elapsed", snapshot("hint", "sha", "interval_elapsed"), markedActive, true, UnchangedActionStore, storeReasonStreamDroppedCandidate},
		{"marked active match, ordinary sync", snapshot("hint", "sha", ""), markedActive, true, UnchangedActionStore, storeReasonStreamDroppedCandidate},
		{"marked pending match", snapshot("hint", "sha", ""), markedPending, true, UnchangedActionStore, storeReasonStreamDroppedCandidate},
		{"marked candidate, different hint", snapshot("other", "sha", ""), markedActive, true, UnchangedActionStore, ""},
		// Review F7-5: the mark decides on its own; a snapshot without a
		// pre-parse identity still stores over a marked candidate.
		{"marked active match, snapshot without identity", snapshot("hint", "sha", "interval_elapsed"), markedActive, true, UnchangedActionStore, storeReasonStreamDroppedCandidate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			action, reason := DecideUnchangedGeneration(tc.generation, tc.candidate, tc.found)
			if action != tc.wantAction || reason != tc.wantReason {
				t.Fatalf("DecideUnchangedGeneration() = (%d, %q), want (%d, %q)", action, reason, tc.wantAction, tc.wantReason)
			}
		})
	}
	if GenerationNeedsDuplicateCheck(scope.ScopeGeneration{FreshnessHint: "hint", DefaultBranchChanged: true}) {
		t.Fatal("a default-branch change must always be stored")
	}
	if GenerationNeedsDuplicateCheck(scope.ScopeGeneration{}) {
		t.Fatal("a snapshot with no hint can never be a duplicate")
	}
}
