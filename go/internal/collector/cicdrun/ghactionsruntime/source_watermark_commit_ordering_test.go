// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/runwatermark"
)

// This file holds the #5429 commit-ordering regression test: NextClaimed
// must never durably advance the run watermark itself. Before this fix,
// NextClaimed called saveWatermark directly on its success path, so a claim
// cycle that fetched a gapped window advanced the watermark even when the
// generation's facts were never committed (a retryable commit failure).
// On the retried claim, NextClaimed re-fetched the SAME window but compared
// it against the ALREADY-ADVANCED watermark, so detectRunBackfillGap no
// longer saw a gap -- the runs_backfill_gap warning silently vanished on
// retry, even though the runs between the true prior watermark and the
// window floor were still never fetched by any successfully committed
// cycle. See run_watermark.go and source_commit_observer.go for the fix:
// the watermark now only advances from
// ClaimedSource.ObserveClaimedGenerationCommitted, which
// collector.ClaimedService invokes exactly once, after the commit that
// generation describes has durably succeeded.

// TestClaimedSourceRetryAfterCommitFailureStillDetectsBackfillGap is THE
// #5429 regression proof. It seeds a watermark simulating a prior
// successfully-committed cycle, then drives a claim cycle whose window is
// gapped relative to that watermark twice in a row for the SAME work item
// -- modeling NextClaimed running once, its generation's commit FAILING
// (so no observer call happens), and the claim retrying: the SAME work
// item's NextClaimed runs again. Both calls must independently detect the
// backfill gap, because the watermark that seeds detection must not have
// moved between them (no commit ever succeeded). Only after the test
// itself invokes ObserveClaimedGenerationCommitted -- modeling the retry's
// commit finally succeeding -- does the watermark advance.
func TestClaimedSourceRetryAfterCommitFailureStillDetectsBackfillGap(t *testing.T) {
	t.Parallel()

	target := watermarkTestTarget()
	store := runwatermark.NewInMemoryStore()
	// Seed the watermark as if an EARLIER, successfully committed cycle
	// left off at run 100.
	if err := store.Save(context.Background(), runwatermark.Watermark{
		Key:          watermarkKey(target),
		LastRunID:    "100",
		GenerationID: "generation-0",
		FencingToken: 1,
	}); err != nil {
		t.Fatalf("seed watermark Save() error = %v, want nil", err)
	}

	gappedWindow := func() RunPage {
		return RunPage{
			// 21 new runs (110-130) landed since the seeded watermark;
			// MaxRuns=10 only fetches the newest 10 (121-130). Runs
			// 101-120 are the gap.
			Snapshots: []RunSnapshot{
				minimalRunSnapshot("130"), minimalRunSnapshot("129"), minimalRunSnapshot("128"),
				minimalRunSnapshot("127"), minimalRunSnapshot("126"), minimalRunSnapshot("125"),
				minimalRunSnapshot("124"), minimalRunSnapshot("123"), minimalRunSnapshot("122"),
				minimalRunSnapshot("121"),
			},
			Truncated: true,
		}
	}
	client := &sequencedClient{pages: []RunPage{gappedWindow(), gappedWindow()}}

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Watermarks:          store,
		Targets:             []TargetConfig{target},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	// The retried claim keeps the SAME GenerationID: a retry re-dispatches
	// the same durable work item row, it does not mint a new generation.
	item := watermarkTestClaim("generation-retry-1", 2)

	firstAttempt, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() [attempt 1] = %v, %v, %v, want nil error and ok=true", firstAttempt, ok, err)
	}
	requireWarningReason(t, drainFacts(t, firstAttempt.Facts), "runs_backfill_gap")

	// Model attempt 1's commit FAILING: collector.ClaimedService would
	// route this to FailClaimRetryable and never call
	// ObserveClaimedGenerationCommitted. Deliberately do NOT call it here.

	// Model collector.ClaimedService retrying the SAME work item: it
	// re-claims the item (unchanged GenerationID) and calls NextClaimed
	// again.
	secondAttempt, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() [attempt 2 / retry] = %v, %v, %v, want nil error and ok=true", secondAttempt, ok, err)
	}
	// THE BUG: before the fix, attempt 1 already durably saved the
	// watermark at 130 (inside NextClaimed), so this retry's
	// detectRunBackfillGap compares window floor 121 against watermark 130
	// and finds no gap -- the runs_backfill_gap warning silently vanishes
	// even though runs 101-120 were still never fetched by any
	// successfully committed cycle. After the fix, NextClaimed never saves
	// the watermark itself, so the retry still sees the seeded watermark
	// (100) and re-detects the same gap.
	requireWarningReason(t, drainFacts(t, secondAttempt.Facts), "runs_backfill_gap")

	// The watermark must still read the SEEDED value: no cycle has
	// committed yet, so nothing should have durably advanced it.
	preCommit, ok, loadErr := store.Load(context.Background(), watermarkKey(target))
	if loadErr != nil || !ok {
		t.Fatalf("Load() [pre-commit] = %+v, %v, %v, want a stored watermark", preCommit, ok, loadErr)
	}
	if preCommit.LastRunID != "100" {
		t.Fatalf("watermark LastRunID [pre-commit] = %q, want %q (must not advance before any commit succeeds)",
			preCommit.LastRunID, "100")
	}

	// Model the retry's commit finally succeeding: collector.ClaimedService
	// calls the optional post-commit hook exactly once.
	observer, ok := any(source).(collector.ClaimedGenerationCommitObserver)
	if !ok {
		t.Fatal("ClaimedSource must implement collector.ClaimedGenerationCommitObserver")
	}
	if err := observer.ObserveClaimedGenerationCommitted(context.Background(), item); err != nil {
		t.Fatalf("ObserveClaimedGenerationCommitted() error = %v, want nil", err)
	}

	postCommit, ok, loadErr := store.Load(context.Background(), watermarkKey(target))
	if loadErr != nil || !ok {
		t.Fatalf("Load() [post-commit] = %+v, %v, %v, want a stored watermark", postCommit, ok, loadErr)
	}
	if postCommit.LastRunID != "130" {
		t.Fatalf("watermark LastRunID [post-commit] = %q, want %q (must advance once the commit is observed)",
			postCommit.LastRunID, "130")
	}
}

// TestClaimedSourceObserveWithoutPendingEntryIsNoOp proves the nil-safety /
// no-stash-found contract: ObserveClaimedGenerationCommitted for a work item
// NextClaimed never produced (or whose fetched window was empty, so nothing
// was stashed) must not error and must not touch the watermark store.
func TestClaimedSourceObserveWithoutPendingEntryIsNoOp(t *testing.T) {
	t.Parallel()

	target := watermarkTestTarget()
	store := runwatermark.NewInMemoryStore()
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              &sequencedClient{},
		Watermarks:          store,
		Targets:             []TargetConfig{target},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	observer, ok := any(source).(collector.ClaimedGenerationCommitObserver)
	if !ok {
		t.Fatal("ClaimedSource must implement collector.ClaimedGenerationCommitObserver")
	}
	item := watermarkTestClaim("generation-never-claimed", 1)
	if err := observer.ObserveClaimedGenerationCommitted(context.Background(), item); err != nil {
		t.Fatalf("ObserveClaimedGenerationCommitted() error = %v, want nil for an unstashed item", err)
	}
	if _, ok, loadErr := store.Load(context.Background(), watermarkKey(target)); loadErr != nil || ok {
		t.Fatalf("Load() = _, %v, %v, want ok=false (no watermark ever written)", ok, loadErr)
	}
}
