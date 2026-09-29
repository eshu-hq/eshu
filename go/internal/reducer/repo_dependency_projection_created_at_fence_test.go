// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

// TestRepoDependencyProjectionRunnerReplaysFencedWhenCompletedRunsOnCreatedAtMoves
// pins the #7323 ruling: the shared-projection intent upsert is last-writer-wins
// on created_at while completed_at only advances, so re-upserting an already
// completed RUNS_ON intent moves its created_at (the acceptance epoch of the
// readiness fence) without reopening it. When a pending sibling later selects
// the acceptance unit, the fence token must be new (readiness published for the
// older epoch does not satisfy it), exactly one fenced replay must be requested,
// and once the new token is ready the active rows are written and completed.
func TestRepoDependencyProjectionRunnerReplaysFencedWhenCompletedRunsOnCreatedAtMoves(t *testing.T) {
	t.Parallel()

	oldEpoch := time.Date(2026, time.September, 20, 9, 0, 0, 0, time.UTC)
	newEpoch := oldEpoch.Add(time.Hour)
	completedAt := oldEpoch.Add(time.Minute)
	repoID := "repo-source"
	runsOnPayload := map[string]any{
		"repo_id":           repoID,
		"platform_id":       "platform:kubernetes:none:cluster/prod:none:none",
		"relationship_type": "RUNS_ON",
		"evidence_source":   CrossRepoEvidenceSource,
	}
	// Same intent_id, so the upsert conflicts on the row; only created_at moves.
	runsOnAtOldEpoch := repoDependencyIntentRow(
		"runs-on-1", "scope-a", repoID, repoID, "run-1", "gen-1", oldEpoch, runsOnPayload,
	)
	runsOnAtOldEpoch.CompletedAt = &completedAt
	runsOnAfterUpsert := runsOnAtOldEpoch
	runsOnAfterUpsert.CreatedAt = newEpoch
	pendingSibling := repoDependencyIntentRow(
		"depends-on-1", "scope-a", repoID, repoID, "run-1", "gen-1", newEpoch.Add(time.Millisecond),
		map[string]any{
			"repo_id":           repoID,
			"target_repo_id":    "repo-target",
			"relationship_type": "DEPENDS_ON",
			"evidence_source":   CrossRepoEvidenceSource,
		},
	)

	oldFences, err := repoDependencyRunsOnFenceRequests([]SharedProjectionIntentRow{runsOnAtOldEpoch})
	if err != nil || len(oldFences) != 1 {
		t.Fatalf("old-epoch fence requests = (%d, %v), want (1, nil)", len(oldFences), err)
	}
	newFences, err := repoDependencyRunsOnFenceRequests([]SharedProjectionIntentRow{runsOnAfterUpsert})
	if err != nil || len(newFences) != 1 {
		t.Fatalf("new-epoch fence requests = (%d, %v), want (1, nil)", len(newFences), err)
	}
	oldFence, newFence := oldFences[0], newFences[0]
	if oldFence.fence == newFence.fence {
		t.Fatalf("fence token unchanged after created_at moved: %q", newFence.fence)
	}

	// Unit selection is pending-only: the completed RUNS_ON row is not pending,
	// so only the sibling selects the unit; the unit read still returns both.
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain:         []SharedProjectionIntentRow{runsOnAfterUpsert, pendingSibling},
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: {runsOnAfterUpsert, pendingSibling}},
		leaseGranted:            true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	replayer := &recordingWorkloadMaterializationReplayer{}
	newTokenReady := false
	runner := RepoDependencyProjectionRunner{
		IntentReader:                    reader,
		LeaseManager:                    reader,
		AcceptanceUnitGate:              reader,
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: replayer,
		WorkloadReadinessPrefetch: func(
			_ context.Context,
			keys []GraphProjectionPhaseKey,
			_ GraphProjectionPhase,
		) (GraphProjectionReadinessLookup, error) {
			if len(keys) != 1 || keys[0] != newFence.readinessKey {
				t.Fatalf("readiness keys = %#v, want only the new-epoch key %#v", keys, newFence.readinessKey)
			}
			return func(key GraphProjectionPhaseKey, _ GraphProjectionPhase) (bool, bool) {
				// Readiness published for the older epoch stays true and must not
				// satisfy the new token.
				if key == oldFence.readinessKey {
					return true, true
				}
				return newTokenReady && key == newFence.readinessKey, key == newFence.readinessKey
			}, nil
		},
		AcceptedGen: acceptedGenerationFixed("gen-1", true),
		Config:      RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	now := newEpoch.Add(time.Minute)
	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v, want nil", err)
	}
	if got, want := result.BlockedReadiness, 1; got != want {
		t.Fatalf("BlockedReadiness = %d, want %d", got, want)
	}
	if got, want := len(replayer.calls), 1; got != want {
		t.Fatalf("fenced replays = %d, want exactly %d", got, want)
	}
	call := replayer.calls[0]
	if !call.fenced || call.fence != newFence.fence || call.repoID != repoID {
		t.Fatalf("replay call = %#v, want fenced request with new token %q", call, newFence.fence)
	}
	if call.fence == oldFence.fence {
		t.Fatalf("replay reused the old-epoch token %q", call.fence)
	}
	if got := len(writer.writeCalls) + len(writer.retractCalls) + len(reader.marked); got != 0 {
		t.Fatalf("graph writes/retracts/marks = %d, want 0 before the new token is ready", got)
	}

	newTokenReady = true
	result, err = runner.processOnce(context.Background(), now.Add(time.Second))
	if err != nil {
		t.Fatalf("processOnce() after readiness error = %v, want nil", err)
	}
	if got := result.BlockedReadiness; got != 0 {
		t.Fatalf("BlockedReadiness after readiness = %d, want 0", got)
	}
	// The post-write replay is an unfenced projection replay; only the fenced
	// readiness request must stay at one.
	fenced := 0
	for _, replay := range replayer.calls {
		if replay.fenced {
			fenced++
		}
	}
	if got, want := fenced, 1; got != want {
		t.Fatalf("fenced replays after readiness = %d, want still %d", got, want)
	}
	if got, want := len(writer.writeCalls), 1; got != want {
		t.Fatalf("write calls after readiness = %d, want %d", got, want)
	}
	if got, want := len(writer.writeCalls[0].rows), 2; got != want {
		t.Fatalf("written rows after readiness = %d, want %d (RUNS_ON + DEPENDS_ON)", got, want)
	}
	if got, want := len(reader.marked), 2; got != want {
		t.Fatalf("marked intents after readiness = %d, want %d", got, want)
	}
	for _, row := range reader.pendingByAcceptanceUnit[repoID] {
		if row.CompletedAt == nil {
			t.Fatalf("intent %q not completed after readiness", row.IntentID)
		}
	}
}
