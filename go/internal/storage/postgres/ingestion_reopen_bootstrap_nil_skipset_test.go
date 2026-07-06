// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

// TestReopenDeploymentMappingWorkItemsNilSkipSetReopensPartitionProcessedThisPass
// is the P0 regression proof for the hostile re-review finding on PR #4816
// (codex #4770 re-review): bootstrap-index's pipeline calls
// BackfillAllRelationshipEvidence (Phase 2) and the PUBLIC
// ReopenDeploymentMappingWorkItems (Phase 4, nil skip-set — the value-receiver
// bootstrapCommitter interface has no same-pass skip-set to thread) as SEPARATE
// phases, not through RunDeferredRelationshipMaintenance. The pre-fix nil
// skip-set path fell back to a FRESH legacy memo-table lookup
// (computeCurrentReopenCatalogFingerprint + LookupMany), which reads back the
// memo row BackfillAllRelationshipEvidence itself just committed THIS pass for
// a partition that was a genuine memo MISS at the start of the pass — the same
// same-pass-re-read bug #4770/#4816 fixed for the ingester's
// RunDeferredRelationshipMaintenance path, but reintroduced on the bootstrap
// path because bootstrap's phases are separate calls with a nil skip-set.
//
// The fix: a nil skip-set must mean "reopen unconditionally" (matching main's
// pre-#4770 behavior for this one-shot path), never "fall back to a memo
// re-read that cannot distinguish this pass's own fresh write from a prior
// pass's committed one."
func TestReopenDeploymentMappingWorkItemsNilSkipSetReopensPartitionProcessedThisPass(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionReopenPartitionMemoSchema(t, db)

	base := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []memoProofFixture{
		{scopeID: "git:scope-a", genID: "gen-a", repoID: "repo-a", repoName: "alpha-service"},
		{scopeID: "git:scope-b", genID: "gen-b", repoID: "repo-b", repoName: "beta-service"},
	}
	seedMemoProofScopesAndFacts(t, ctx, db, fixtures, map[string]string{
		"repo-a": "beta-service",
	}, base)

	// Precondition: scope-a's partition has NO memo row yet — a genuine
	// memo-MISS at the start of this pass, matching bootstrap's cold-start case.
	if got := countMemoRows(t, ctx, db); got != 0 {
		t.Fatalf("precondition: memo row count = %d, want 0 (partition must start as a memo miss)", got)
	}

	// Seed a succeeded deployment_mapping work item for scope-a's partition,
	// representing a resolve that ran BEFORE this pass's backward evidence
	// existed — exactly the item that must be reopened once that evidence
	// lands.
	seedSucceededReopenWorkItem(t, ctx, db, "work-deployment-mapping-a", "git:scope-a", "gen-a", "deployment_mapping", base)

	adapter := SQLDB{DB: db}
	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return base }

	// Drive bootstrap-index's ACTUAL call shape: BackfillAllRelationshipEvidence
	// (Phase 2, writes a FRESH memo row for scope-a's just-reprocessed
	// partition) then the PUBLIC ReopenDeploymentMappingWorkItems (Phase 4, nil
	// skip-set) as two SEPARATE calls — never RunDeferredRelationshipMaintenance,
	// which threads a same-pass skip-set and was already fixed.
	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v", err)
	}

	// Sanity: this pass really did write a fresh memo row for scope-a (proving
	// the partition was reprocessed this pass, not skipped).
	if got := countMemoRows(t, ctx, db); got == 0 {
		t.Fatal("BackfillAllRelationshipEvidence wrote no partition memo rows; test precondition violated")
	}

	if err := store.ReopenDeploymentMappingWorkItems(ctx, nil, nil); err != nil {
		t.Fatalf("ReopenDeploymentMappingWorkItems() error = %v", err)
	}

	// THE ASSERTION: bootstrap's Phase 4 nil-skip-set reopen must still reopen
	// this work item (transition to 'pending'), because its partition was
	// processed by THIS SAME bootstrap run's Phase 2 backfill. Reading it back
	// as a memo hit against the memo row Phase 2 itself just wrote means the
	// fresh cross-repo evidence Phase 2 committed is NEVER consumed by the
	// reducer — the exact missing-DEPLOYS_FROM/DEPENDS_ON-after-cold-bootstrap
	// bug the hostile re-review reproduced.
	if got, want := workItemStatus(t, ctx, db, "work-deployment-mapping-a"), "pending"; got != want {
		t.Fatalf("work item status after bootstrap-shape nil-skip-set reopen = %q, want %q (a nil skip-set MUST reopen unconditionally, never re-read this same pass's own fresh memo write)", got, want)
	}
}

// TestReopenCodeImportRepoEdgeWorkItemsNilSkipSetReopensPartitionProcessedThisPass
// is the code_import_repo_edge sibling of the deployment_mapping proof above:
// bootstrap-index's Phase 4 also calls the PUBLIC
// ReopenCodeImportRepoEdgeWorkItems with a nil skip-set after its own Phase 2
// BackfillAllRelationshipEvidence call.
func TestReopenCodeImportRepoEdgeWorkItemsNilSkipSetReopensPartitionProcessedThisPass(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionReopenPartitionMemoSchema(t, db)

	base := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []memoProofFixture{
		{scopeID: "git:scope-a", genID: "gen-a", repoID: "repo-a", repoName: "alpha-service"},
		{scopeID: "git:scope-b", genID: "gen-b", repoID: "repo-b", repoName: "beta-service"},
	}
	seedMemoProofScopesAndFacts(t, ctx, db, fixtures, map[string]string{
		"repo-a": "beta-service",
	}, base)

	if got := countMemoRows(t, ctx, db); got != 0 {
		t.Fatalf("precondition: memo row count = %d, want 0 (partition must start as a memo miss)", got)
	}

	seedSucceededReopenWorkItem(t, ctx, db, "work-code-import-a", "git:scope-a", "gen-a", "code_import_repo_edge", base)

	adapter := SQLDB{DB: db}
	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return base }

	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v", err)
	}
	if got := countMemoRows(t, ctx, db); got == 0 {
		t.Fatal("BackfillAllRelationshipEvidence wrote no partition memo rows; test precondition violated")
	}

	if err := store.ReopenCodeImportRepoEdgeWorkItems(ctx, nil, nil); err != nil {
		t.Fatalf("ReopenCodeImportRepoEdgeWorkItems() error = %v", err)
	}

	if got, want := workItemStatus(t, ctx, db, "work-code-import-a"), "pending"; got != want {
		t.Fatalf("work item status after bootstrap-shape nil-skip-set reopen = %q, want %q (a nil skip-set MUST reopen unconditionally)", got, want)
	}
}
