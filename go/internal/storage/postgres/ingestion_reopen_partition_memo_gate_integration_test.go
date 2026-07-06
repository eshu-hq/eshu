// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// reopenPartitionMemoProofSchemaSQL extends deferredPartitionMemoProofSchemaSQL
// (scopes, generations, facts, relationship_evidence_facts,
// graph_projection_phase_state, deferred_backfill_partition_memo) with
// fact_work_items — the table the reopen partition-memo gate (issue #4770)
// actually reads and writes — so these tests can seed a REAL succeeded
// deployment_mapping/code_import_repo_edge work item and drive the REAL
// ReopenDeploymentMappingWorkItems / ReopenCodeImportRepoEdgeWorkItems against
// it end to end, not just the fact-load half the sibling proof file covers.
const reopenPartitionMemoProofSchemaSQL = deferredPartitionMemoProofSchemaSQL + `
CREATE TABLE fact_work_items (
    work_item_id    TEXT PRIMARY KEY,
    scope_id        TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id   TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    stage           TEXT NOT NULL,
    domain          TEXT NOT NULL,
    conflict_domain TEXT NOT NULL DEFAULT 'scope',
    conflict_key    TEXT NULL,
    status          TEXT NOT NULL,
    attempt_count   INTEGER NOT NULL DEFAULT 0,
    lease_owner     TEXT NULL,
    claim_until     TIMESTAMPTZ NULL,
    visible_at      TIMESTAMPTZ NULL,
    last_attempt_at TIMESTAMPTZ NULL,
    next_attempt_at TIMESTAMPTZ NULL,
    failure_class   TEXT NULL,
    failure_message TEXT NULL,
    failure_details TEXT NULL,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);
`

// TestReopenDeploymentMappingWorkItemsNilSkipSetAlwaysReopensEvenAfterMemoHit
// is the MANDATORY regression proof for the issue #4770/#4816 hostile
// re-review finding: ReopenDeploymentMappingWorkItems (the public method, nil
// skip-set — bootstrap-index's only call shape) must reopen a succeeded work
// item UNCONDITIONALLY, even when its partition has a real memo-hit row
// (produced by running BackfillAllRelationshipEvidence twice over an
// unchanged catalog+fact corpus, exactly like
// TestDeferredBackfillPartitionMemoNoChangeRerunSkipsAndIsIdentical proves for
// the fact-load side). A prior version of this gate treated a nil skip-set as
// "fall back to a memo-table lookup" and skipped this exact case — safe only
// when no backfill in the same call had just written a fresh memo row, an
// assumption bootstrap-index's separate-phase call shape violates (see
// applyReopenPartitionMemoGate's doc comment). The fix removes that fallback
// entirely: nil now always means reopen-all.
func TestReopenDeploymentMappingWorkItemsNilSkipSetAlwaysReopensEvenAfterMemoHit(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionReopenPartitionMemoSchema(t, db)
	ensureResolverSchema(t, ctx, db)

	base := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []memoProofFixture{
		{scopeID: "git:scope-a", genID: "gen-a", repoID: "repo-a", repoName: "alpha-service"},
		{scopeID: "git:scope-b", genID: "gen-b", repoID: "repo-b", repoName: "beta-service"},
	}
	seedMemoProofScopesAndFacts(t, ctx, db, fixtures, map[string]string{
		"repo-a": "beta-service",
	}, base)

	adapter := SQLDB{DB: db}
	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return base }

	// Pass 1: discovers evidence and writes a memo row for scope-a's partition
	// (non-ArgoCD-bearing) under the current catalog fingerprint.
	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 1 BackfillAllRelationshipEvidence() error = %v", err)
	}
	pass1Edges := evidenceEdgeSet(t, ctx, db)
	if pass1Memos := countMemoRows(t, ctx, db); pass1Memos == 0 {
		t.Fatal("pass 1 wrote no partition memo rows; test precondition violated")
	}

	// Seed a succeeded deployment_mapping work item for scope-a's partition, as
	// if the reducer already resolved it against the evidence pass 1 committed.
	seedSucceededReopenWorkItem(t, ctx, db, "work-deployment-mapping-a", "git:scope-a", "gen-a", "deployment_mapping", base)

	// BASELINE: run the REAL cross-repo resolver once against pass 1's
	// evidence, representing the resolution the reducer already performed
	// before this work item was marked 'succeeded'.
	baselineCount := resolveCrossRepoIntents(t, ctx, db, "git:scope-a", "gen-a")
	baselineIntents := snapshotSharedProjectionIntents(t, ctx, db, "gen-a")
	if baselineCount == 0 || len(baselineIntents) == 0 {
		t.Fatal("baseline Resolve() emitted no intents; fixture is not exercising the cross-repo edge")
	}

	// Pass 2: identical catalog and facts. scope-a's partition is now a memo
	// HIT (backward evidence unchanged since pass 1) at the fact-load layer, so
	// BackfillAllRelationshipEvidence itself skips its fact load. But the
	// PUBLIC, nil-skip-set ReopenDeploymentMappingWorkItems below must still
	// reopen this work item unconditionally — a memo hit at the fact-load layer
	// is irrelevant to the nil-skip-set reopen contract.
	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 2 BackfillAllRelationshipEvidence() error = %v", err)
	}
	pass2Edges := evidenceEdgeSet(t, ctx, db)
	if len(pass2Edges) != len(pass1Edges) {
		t.Fatalf("pass 2 edge set size = %d, want %d (evidence must be unchanged on a memo-hit partition)", len(pass2Edges), len(pass1Edges))
	}
	for edge := range pass1Edges {
		if !pass2Edges[edge] {
			t.Fatalf("pass 2 missing edge %q present in pass 1; evidence set is not byte-identical", edge)
		}
	}

	if err := store.ReopenDeploymentMappingWorkItems(ctx, nil, nil); err != nil {
		t.Fatalf("ReopenDeploymentMappingWorkItems() error = %v", err)
	}

	// THE ASSERTION: the nil-skip-set reopen must transition the work item to
	// 'pending' unconditionally, never skipping on a fact-load-layer memo hit.
	if got, want := workItemStatus(t, ctx, db, "work-deployment-mapping-a"), "pending"; got != want {
		t.Fatalf("work item status after nil-skip-set reopen = %q, want %q (nil skip-set must reopen unconditionally, even on a memo-hit partition)", got, want)
	}
}

// TestRunDeferredRelationshipMaintenanceReopensPartitionProcessedThisPass is the
// P1 regression proof for the codex finding on issue #4770/PR #4816: a
// partition that is a memo MISS at the START of a maintenance pass — so
// BackfillAllRelationshipEvidence reprocesses it and, in the SAME pass, writes
// a FRESH memo row keyed to the CURRENT catalog fingerprint — must still have
// its already-succeeded work item reopened by the very same pass's reopen
// step, because that work item resolved BEFORE this pass's new evidence
// existed.
//
// The pre-fix bug: ReopenDeploymentMappingWorkItems independently recomputed
// the current fingerprint and RE-READ the memo table AFTER
// BackfillAllRelationshipEvidence had already written the fresh memo row for
// this pass, so a partition reprocessed THIS pass read back as a memo HIT to
// the reopen gate and was wrongly skipped — even though the work item is
// stale relative to the evidence that just landed. Driving
// RunDeferredRelationshipMaintenance (the real ingester call sequence: backfill
// then reopen, both against a partition with NO prior memo row) reproduces
// this exactly, unlike the sibling equivalence test above, which seeds a
// memo HIT from a separate first pass before seeding the work item.
func TestRunDeferredRelationshipMaintenanceReopensPartitionProcessedThisPass(t *testing.T) {
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
	// memo-MISS at the start of this pass, the cold-start/catalog-changed case
	// the reopen mechanism exists to handle.
	if got := countMemoRows(t, ctx, db); got != 0 {
		t.Fatalf("precondition: memo row count = %d, want 0 (partition must start as a memo miss)", got)
	}

	// Seed a succeeded deployment_mapping work item for scope-a's partition
	// BEFORE this pass runs, representing a resolve that happened using
	// evidence that predates the backward evidence this pass is about to
	// commit for the first time.
	seedSucceededReopenWorkItem(t, ctx, db, "work-deployment-mapping-a", "git:scope-a", "gen-a", "deployment_mapping", base)

	adapter := SQLDB{DB: db}
	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return base }

	// Drive the REAL ingester call sequence: BackfillAllRelationshipEvidence
	// (which discovers evidence for the memo-miss partition and writes a FRESH
	// memo row for it in the SAME pass) immediately followed by
	// ReopenDeploymentMappingWorkItems, exactly as
	// RunDeferredRelationshipMaintenance calls them.
	if err := store.RunDeferredRelationshipMaintenance(ctx, nil, nil); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenance() error = %v", err)
	}

	// Sanity: this pass really did write a fresh memo row for scope-a (proving
	// the partition was reprocessed this pass, not skipped).
	if got := countMemoRows(t, ctx, db); got == 0 {
		t.Fatal("RunDeferredRelationshipMaintenance wrote no partition memo rows; test precondition violated")
	}

	// THE ASSERTION: the work item must be reopened (transitioned to
	// 'pending') because its partition was processed — not skipped — by this
	// very pass's backfill step. Skipping the reopen here means the new
	// backward evidence this pass just committed is never consumed by the
	// reducer, defeating the entire reopen mechanism for the cold-start case.
	if got, want := workItemStatus(t, ctx, db, "work-deployment-mapping-a"), "pending"; got != want {
		t.Fatalf("work item status after same-pass reopen = %q, want %q (a partition reprocessed THIS pass must reopen its succeeded work items, not read back as a memo hit against the memo row this SAME pass just wrote)", got, want)
	}
}

// TestReopenDeploymentMappingWorkItemsReopensNonMemoHitPartition is a control
// proof: a work item whose partition has NO memo row at all (the common
// real-world bootstrap case) still reopens via the nil-skip-set public
// method, confirming the reopen-all-on-nil contract holds independent of
// whatever the memo table happens to contain.
func TestReopenDeploymentMappingWorkItemsReopensNonMemoHitPartition(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionReopenPartitionMemoSchema(t, db)

	base := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []memoProofFixture{
		{scopeID: "git:scope-a", genID: "gen-a", repoID: "repo-a", repoName: "alpha-service"},
	}
	seedMemoProofScopesAndFacts(t, ctx, db, fixtures, nil, base)

	// Seed a memo row directly under a fingerprint unrelated to the current
	// catalog — proving the reopen result no longer depends on the memo table
	// at all for the nil-skip-set path.
	if _, err := db.ExecContext(ctx, `
INSERT INTO deferred_backfill_partition_memo (scope_id, generation_id, catalog_fingerprint, committed_at)
VALUES ($1, $2, $3, $4)`,
		"git:scope-a", "gen-a", "sha256:irrelevant-to-nil-skip-set-path", base); err != nil {
		t.Fatalf("seed memo row: %v", err)
	}

	seedSucceededReopenWorkItem(t, ctx, db, "work-deployment-mapping-a", "git:scope-a", "gen-a", "deployment_mapping", base)

	adapter := SQLDB{DB: db}
	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return base }

	if err := store.ReopenDeploymentMappingWorkItems(ctx, nil, nil); err != nil {
		t.Fatalf("ReopenDeploymentMappingWorkItems() error = %v", err)
	}

	if got, want := workItemStatus(t, ctx, db, "work-deployment-mapping-a"), "pending"; got != want {
		t.Fatalf("work item status after nil-skip-set reopen = %q, want %q (must reopen regardless of memo table contents)", got, want)
	}
}

// TestReopenDeploymentMappingWorkItemsAlwaysReopensArgoCDBearingPartition is
// the ArgoCD carve-out proof at the reopen layer via the nil-skip-set public
// method: an ArgoCD-bearing partition (repo-control, holding an
// ApplicationSet with a git generator pointing at repo-config, the same
// fixture shape TestDeferredBackfillPartitionMemoArgoCDCarveOutAlwaysReloads
// uses for the fact-load side) NEVER gets a memo row on the write side, and
// under the nil-skip-set reopen-all contract every succeeded work item
// reopens regardless — so this partition's work item reopens for the same
// reason every other nil-skip-set candidate does.
func TestReopenDeploymentMappingWorkItemsAlwaysReopensArgoCDBearingPartition(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionReopenPartitionMemoSchema(t, db)

	base := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	// repo-control: holds the ArgoCD ApplicationSet (never memoized, per
	// listArgoCDBearingPartitionsQuery / writeDeferredBackfillPartitionMemos).
	seedArgoCDControlFixture(t, ctx, db, base)

	adapter := SQLDB{DB: db}
	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return base }

	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 1 BackfillAllRelationshipEvidence() error = %v", err)
	}
	var controlMemoCount int
	if err := db.QueryRowContext(
		ctx,
		"SELECT count(*) FROM deferred_backfill_partition_memo WHERE scope_id = $1", "git:scope-control",
	).Scan(&controlMemoCount); err != nil {
		t.Fatalf("count control memo rows: %v", err)
	}
	if controlMemoCount != 0 {
		t.Fatalf("repo-control (ArgoCD-bearing) got a memo row; precondition violated, got %d rows", controlMemoCount)
	}

	seedSucceededReopenWorkItem(t, ctx, db, "work-deployment-mapping-control", "git:scope-control", "gen-control", "deployment_mapping", base)

	// Pass 2: repo-control's OWN partition is completely unchanged, but it must
	// still reopen because it is ArgoCD-bearing (never memoized).
	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 2 BackfillAllRelationshipEvidence() error = %v", err)
	}
	if err := store.ReopenDeploymentMappingWorkItems(ctx, nil, nil); err != nil {
		t.Fatalf("ReopenDeploymentMappingWorkItems() error = %v", err)
	}

	if got, want := workItemStatus(t, ctx, db, "work-deployment-mapping-control"), "pending"; got != want {
		t.Fatalf("ArgoCD-bearing partition work item status after reopen = %q, want %q (must ALWAYS reopen, never memo-skipped)", got, want)
	}
}

// TestReopenCodeImportRepoEdgeWorkItemsNilSkipSetAlwaysReopensEvenAfterMemoHit
// applies the same reopen-all-on-nil proof to the code_import_repo_edge
// reopen path, proving the fix is not deployment_mapping-only.
func TestReopenCodeImportRepoEdgeWorkItemsNilSkipSetAlwaysReopensEvenAfterMemoHit(t *testing.T) {
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

	adapter := SQLDB{DB: db}
	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return base }

	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 1 BackfillAllRelationshipEvidence() error = %v", err)
	}
	seedSucceededReopenWorkItem(t, ctx, db, "work-code-import-a", "git:scope-a", "gen-a", "code_import_repo_edge", base)

	// Pass 2 makes scope-a's partition a memo hit at the fact-load layer, but
	// the PUBLIC, nil-skip-set ReopenCodeImportRepoEdgeWorkItems below must
	// still reopen unconditionally.
	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 2 BackfillAllRelationshipEvidence() error = %v", err)
	}
	if err := store.ReopenCodeImportRepoEdgeWorkItems(ctx, nil, nil); err != nil {
		t.Fatalf("ReopenCodeImportRepoEdgeWorkItems() error = %v", err)
	}

	if got, want := workItemStatus(t, ctx, db, "work-code-import-a"), "pending"; got != want {
		t.Fatalf("code_import_repo_edge work item status after nil-skip-set reopen = %q, want %q (nil skip-set must reopen unconditionally)", got, want)
	}
}

// provisionReopenPartitionMemoSchema is provisionDeferredPartitionMemoSchema
// plus fact_work_items, so these tests can drive the real reopen paths.
func provisionReopenPartitionMemoSchema(t *testing.T, db *sql.DB) string {
	t.Helper()
	ctx := context.Background()
	schemaName := fmt.Sprintf("reopen_partition_memo_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, reopenPartitionMemoProofSchemaSQL); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}
	return schemaName
}
