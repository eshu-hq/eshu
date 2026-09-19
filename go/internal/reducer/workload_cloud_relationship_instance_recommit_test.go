// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/workloadinstance"
)

// TestWorkloadCloudRelationshipFirstGenerationReCommitRetracts is review P3-2
// for USES: the first commit of a scope's first generation skips the retract,
// but the re-commit after the missing instance appears retracts first, so the
// rewrite never stacks on edges an earlier evaluation of the same generation
// wrote.
func TestWorkloadCloudRelationshipFirstGenerationReCommitRetracts(t *testing.T) {
	t.Parallel()
	clock := time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC)
	ledger := &usesWaitLedger{rows: map[string]crossscope.ReadinessWait{}}
	lookup := &fakeWorkloadInstanceExistence{existing: map[workloadinstance.Anchor]struct{}{prodAnchor: {}}}
	first := &recordingWorkloadCloudRelationshipWriter{}
	handler := instanceReadinessHandler(lookup, first)
	handler.ReadinessWaits = ledger
	handler.Now = func() time.Time { return clock }
	handler.PriorGenerationCheck = func(context.Context, string, string) (bool, error) { return false, nil }
	intent := instanceReadinessIntent(clock)
	intent.AttemptCount = 1

	_, err := handler.Handle(context.Background(), intent)
	requireInstancesNotReady(t, err)
	if first.retractCalls != 0 || first.writeCalls != 1 {
		t.Fatalf("first commit of the first generation: retracts %d writes %d, want 0/1", first.retractCalls, first.writeCalls)
	}

	clock = clock.Add(time.Minute)
	lookup.existing = map[workloadinstance.Anchor]struct{}{prodAnchor: {}, stageAnchor: {}}
	recommit := &recordingWorkloadCloudRelationshipWriter{}
	handler.EdgeWriter = recommit
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() after the instance appeared error = %v", err)
	}
	if recommit.retractCalls != 1 || recommit.writeCalls != 1 {
		t.Fatalf("same-generation re-commit: retracts %d writes %d, want 1/1", recommit.retractCalls, recommit.writeCalls)
	}
}
