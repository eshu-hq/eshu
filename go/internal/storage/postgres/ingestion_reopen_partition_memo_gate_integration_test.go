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

// TestReopenDeploymentMappingWorkItemsSkipsMemoHitPartitionEquivalence is the
// MANDATORY equivalence regression proof for issue #4770. For a partition
// whose backward evidence already committed under the CURRENT catalog
// fingerprint (a real memo-hit, produced by running BackfillAllRelationshipEvidence
// twice over an unchanged catalog+fact corpus, exactly like
// TestDeferredBackfillPartitionMemoNoChangeRerunSkipsAndIsIdentical proves for
// the fact-load side), this test proves TWO things against the REAL
// production code, not a stand-in:
//
//  1. The unconditional-reopen counterfactual is itself a no-op: driving the
//     REAL reducer.CrossRepoRelationshipHandler.Resolve (backed by the real
//     Postgres RelationshipStore/SharedIntentStore) TWICE over the SAME
//     unchanged evidence — exactly what an unconditional reopen followed by a
//     reducer re-drive to convergence would do — produces a byte-identical
//     shared_projection_intents row set both times (0/0 on intent_id AND
//     payload, not merely a row count). This is the direct evidence for the
//     purity claim (DiscoverEvidence/Resolve/UpsertIntents are pure functions
//     of (facts, catalog, assertions) with no read-back of their own prior
//     output, and evidence_id/intent_id are content-addressed), proven by
//     actually running the resolver rather than asserting it analytically.
//  2. Given (1), the GATED ReopenDeploymentMappingWorkItems call leaving the
//     work item 'succeeded' (0 rows reopened) is provably equivalent to
//     letting the unconditional reopen run and re-resolve: both paths would
//     converge to the identical intent rows proven in (1), so skipping the
//     replay changes nothing about the eventual graph truth, only the
//     scheduling cost of getting there.
func TestReopenDeploymentMappingWorkItemsSkipsMemoHitPartitionEquivalence(t *testing.T) {
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

	// Pass 2: identical catalog and facts. scope-a's partition must be a memo
	// HIT (backward evidence unchanged since pass 1), so BackfillAllRelationshipEvidence
	// skips its fact load AND ReopenDeploymentMappingWorkItems must skip
	// reopening its succeeded work item as provably redundant.
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

	// UNCONDITIONAL-REOPEN COUNTERFACTUAL: run the REAL resolver a SECOND time
	// over the same (unchanged) evidence pass 2 left in place — exactly what
	// reopening this work item unconditionally and letting the reducer
	// re-claim and re-drive it to convergence would do. Compare the resulting
	// intent rows against the baseline for byte-identity (0/0), proving the
	// replay this gate skips would have been a no-op, not merely asserting it.
	counterfactualCount := resolveCrossRepoIntents(t, ctx, db, "git:scope-a", "gen-a")
	counterfactualIntents := snapshotSharedProjectionIntents(t, ctx, db, "gen-a")
	if counterfactualCount != baselineCount {
		t.Fatalf("unconditional-reopen counterfactual Resolve() emitted %d intents, want %d (baseline)", counterfactualCount, baselineCount)
	}
	assertIntentSnapshotsIdentical(t, baselineIntents, counterfactualIntents,
		"unconditional-reopen counterfactual vs baseline resolve over unchanged evidence")

	if err := store.ReopenDeploymentMappingWorkItems(ctx, nil, nil); err != nil {
		t.Fatalf("ReopenDeploymentMappingWorkItems() error = %v", err)
	}

	// EQUIVALENCE ASSERTION: the gated reopen must leave the work item
	// 'succeeded' — identical to the resulting status set an unconditional
	// reopen followed by re-resolving to convergence over UNCHANGED evidence
	// would produce (proven above to be a byte-identical no-op replay),
	// because the replay was skipped, not merely because nothing happened to
	// run.
	if got, want := workItemStatus(t, ctx, db, "work-deployment-mapping-a"), "succeeded"; got != want {
		t.Fatalf("work item status after gated reopen = %q, want %q (memo-hit partition must be skipped, 0 rows reopened)", got, want)
	}

	// The gated reopen must not itself have altered the intent rows: it only
	// skips the queue-status transition, never touches shared_projection_intents.
	postGateIntents := snapshotSharedProjectionIntents(t, ctx, db, "gen-a")
	assertIntentSnapshotsIdentical(t, counterfactualIntents, postGateIntents,
		"post-gate intents vs unconditional-reopen counterfactual")
}

// TestReopenDeploymentMappingWorkItemsReopensNonMemoHitPartition is the
// non-memo-hit control for the equivalence proof above: a work item whose
// partition has NO memo row at all (the common real-world case: a bootstrap
// pass, or — as reproduced here directly — a stale memo row recorded under a
// DIFFERENT, no-longer-current catalog fingerprint) must still reopen
// (transition to 'pending'), proving the gate only skips the
// provably-redundant memo-HIT case and does not over-skip a genuine miss.
//
// This seeds the stale memo row directly rather than relying on two
// BackfillAllRelationshipEvidence passes: running backfill again after
// onboarding a new repository would ALSO rewrite scope-a's memo row to the
// new (now-current) fingerprint in the same pass — since backfill always
// runs immediately before reopen in RunDeferredRelationshipMaintenance, that
// self-heals the memo before reopen ever sees it. The genuine
// production-observable miss this test targets is a memo row whose
// fingerprint predates the CURRENT reopen call's freshly computed one.
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

	// Seed a STALE memo row directly: scope-a's partition claims it committed
	// under a fingerprint that will never match the real catalog's current
	// fingerprint (computeCurrentReopenCatalogFingerprint always derives a
	// "sha256:..." digest), so this is a genuine, deterministic memo miss.
	if _, err := db.ExecContext(ctx, `
INSERT INTO deferred_backfill_partition_memo (scope_id, generation_id, catalog_fingerprint, committed_at)
VALUES ($1, $2, $3, $4)`,
		"git:scope-a", "gen-a", "sha256:stale-does-not-match-current-catalog", base); err != nil {
		t.Fatalf("seed stale memo row: %v", err)
	}

	seedSucceededReopenWorkItem(t, ctx, db, "work-deployment-mapping-a", "git:scope-a", "gen-a", "deployment_mapping", base)

	adapter := SQLDB{DB: db}
	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return base }

	if err := store.ReopenDeploymentMappingWorkItems(ctx, nil, nil); err != nil {
		t.Fatalf("ReopenDeploymentMappingWorkItems() error = %v", err)
	}

	if got, want := workItemStatus(t, ctx, db, "work-deployment-mapping-a"), "pending"; got != want {
		t.Fatalf("work item status after reopen with a stale (non-matching) catalog fingerprint = %q, want %q (must still reopen)", got, want)
	}
}

// TestReopenDeploymentMappingWorkItemsAlwaysReopensArgoCDBearingPartition is
// the MANDATORY ArgoCD carve-out proof at the reopen layer: an ArgoCD-bearing
// partition (repo-control, holding an ApplicationSet with a git generator
// pointing at repo-config, the same fixture shape
// TestDeferredBackfillPartitionMemoArgoCDCarveOutAlwaysReloads uses for the
// fact-load side) NEVER gets a memo row on the write side, so its succeeded
// deployment_mapping work item must ALWAYS reopen on every maintenance cycle,
// never skipped, even though its own (scope_id, generation_id) is completely
// unchanged between passes.
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

// TestReopenCodeImportRepoEdgeWorkItemsSkipsMemoHitPartition applies the same
// memo-hit-skip proof to the code_import_repo_edge reopen path (issue #4770
// scope item 4: "Apply the SAME gate to the code_import_repo_edge reopen
// path"), proving the gate is not deployment_mapping-only.
func TestReopenCodeImportRepoEdgeWorkItemsSkipsMemoHitPartition(t *testing.T) {
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

	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 2 BackfillAllRelationshipEvidence() error = %v", err)
	}
	if err := store.ReopenCodeImportRepoEdgeWorkItems(ctx, nil, nil); err != nil {
		t.Fatalf("ReopenCodeImportRepoEdgeWorkItems() error = %v", err)
	}

	if got, want := workItemStatus(t, ctx, db, "work-code-import-a"), "succeeded"; got != want {
		t.Fatalf("code_import_repo_edge work item status after gated reopen = %q, want %q (memo-hit partition must be skipped)", got, want)
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
