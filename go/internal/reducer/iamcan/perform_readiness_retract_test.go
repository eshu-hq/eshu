// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestIAMCanPerformFirstGenerationReCommitRetractsShrunkTargets is review
// P3-2. On a scope's first generation the first commit skips the retract, but
// a later re-commit in the same generation must not: here the ready bucket's
// s3 scope activates a newer generation without that bucket while the other
// target becomes ready, so the resolved set shrinks from {A} to {B}. Without
// the retract the edge to the removed bucket would survive until the next iam
// generation.
func TestIAMCanPerformFirstGenerationReCommitRetractsShrunkTargets(t *testing.T) {
	t.Parallel()
	clock := waitTestNow()
	ledger := newMemoryWaitLedger()
	loader := &fakeCrossScopeTargets{snapshot: waitSnapshot(false)}
	handler, _ := waitHandler(ledger, loader, &clock)
	handler.PriorGenerationCheck = func(context.Context, string, string) (bool, error) { return false, nil }
	intent := waitIntent("iam-gen-1", clock)
	intent.AttemptCount = 1

	_, err := handler.Handle(context.Background(), intent)
	requireTargetNotReady(t, err)
	first := handler.Writer.(*recordingIAMCanPerformWriter)
	if first.retractCalls != 0 || len(first.edgeRows) != 1 {
		t.Fatalf("first commit of the first generation: retracts %d rows %d, want the retract skipped and 1 edge",
			first.retractCalls, len(first.edgeRows))
	}

	clock = clock.Add(time.Minute)
	shrunk := waitSnapshot(true)
	shrunk.Scopes[0].ActiveGenerationID = "s3-gen-2"
	shrunk.Resources[crossScopeS3Scope] = []facts.Envelope{}
	loader.snapshot = shrunk
	recommit := &recordingIAMCanPerformWriter{}
	handler.Writer = recommit
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() after the target resolved error = %v, want success", err)
	}
	if recommit.retractCalls != 1 {
		t.Fatalf("same-generation re-commit retracts = %d, want 1 so the removed bucket's edge goes away",
			recommit.retractCalls)
	}
	if len(recommit.edgeRows) != 1 {
		t.Fatalf("re-commit rows = %d, want only the newly ready bucket's edge", len(recommit.edgeRows))
	}
}
