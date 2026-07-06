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

// seedSucceededReopenWorkItem inserts one succeeded reducer work item for the
// given domain and (scope_id, generation_id) partition, matching the shape
// generation_liveness_sql.go's writer produces for a completed reducer run.
func seedSucceededReopenWorkItem(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID, scopeID, genID, domain string,
	at time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', $4, 'succeeded', '{}'::jsonb, $5, $5)`,
		workItemID, scopeID, genID, domain, at); err != nil {
		t.Fatalf("seed succeeded %s work item %q: %v", domain, workItemID, err)
	}
}

// workItemStatus reads back one work item's current status.
func workItemStatus(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(
		ctx,
		"SELECT status FROM fact_work_items WHERE work_item_id = $1", workItemID,
	).Scan(&status); err != nil {
		t.Fatalf("read status for work item %q: %v", workItemID, err)
	}
	return status
}

// TestReopenDeploymentMappingWorkItemsSkipsMemoHitPartitionEquivalence is the
// MANDATORY equivalence regression proof for issue #4770: for a partition
// whose backward evidence already committed under the CURRENT catalog
// fingerprint (a real memo-hit, produced by running BackfillAllRelationshipEvidence
// twice over an unchanged catalog+fact corpus, exactly like
// TestDeferredBackfillPartitionMemoNoChangeRerunSkipsAndIsIdentical proves for
// the fact-load side), the gated ReopenDeploymentMappingWorkItems call
// produces the IDENTICAL resulting fact_work_items status ('succeeded',
// unchanged — 0 rows reopened) as simply never reopening it at all: the
// partition's evidence set is byte-identical before and after the skipped
// reopen (proved by the shared evidenceEdgeSet comparison), so a reopened
// replay of this work item would recompute the same intents the reducer
// already resolved from that unchanged evidence — the skip is provably
// redundant, not merely likely so.
func TestReopenDeploymentMappingWorkItemsSkipsMemoHitPartitionEquivalence(t *testing.T) {
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

	if err := store.ReopenDeploymentMappingWorkItems(ctx, nil, nil); err != nil {
		t.Fatalf("ReopenDeploymentMappingWorkItems() error = %v", err)
	}

	// EQUIVALENCE ASSERTION: the gated reopen must leave the work item
	// 'succeeded' — identical to the resulting status set an unconditional
	// reopen followed by re-resolving to convergence over UNCHANGED evidence
	// would produce (a no-op replay converging back to the same intents),
	// because the replay was skipped, not merely because nothing happened to
	// run.
	if got, want := workItemStatus(t, ctx, db, "work-deployment-mapping-a"), "succeeded"; got != want {
		t.Fatalf("work item status after gated reopen = %q, want %q (memo-hit partition must be skipped, 0 rows reopened)", got, want)
	}
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

// seedArgoCDControlFixture seeds the same ArgoCD ApplicationSet + external
// config repo shape TestDeferredBackfillPartitionMemoArgoCDCarveOutAlwaysReloads
// uses, so this file's reopen-layer ArgoCD proof exercises the identical
// evidence shape the fact-load-layer proof already covers.
func seedArgoCDControlFixture(t *testing.T, ctx context.Context, db *sql.DB, base time.Time) {
	t.Helper()

	if _, err := db.ExecContext(ctx,
		"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, NULL)", "git:scope-control"); err != nil {
		t.Fatalf("seed scope-control: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
		"gen-control", "git:scope-control", base); err != nil {
		t.Fatalf("seed gen-control: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)`,
		"repo-fact-control", "git:scope-control", "gen-control", base,
		`{"repo_id":"repo-control","name":"control-service"}`); err != nil {
		t.Fatalf("seed repo-control repository fact: %v", err)
	}

	appSetYAML := `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: demo
spec:
  generators:
  - git:
      repoURL: https://github.com/example/repo-config.git
      files:
      - path: services/*/service.yaml
  template:
    spec:
      source:
        repoURL: "{{ .service.repoURL }}"
`
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'file', $1, 'git', $1, $4, $4, $5::jsonb)`,
		"appset-control", "git:scope-control", "gen-control", base,
		mustJSONPayload(t, "repo-control", "argocd", "appset.yaml", appSetYAML)); err != nil {
		t.Fatalf("seed ApplicationSet fact: %v", err)
	}

	if _, err := db.ExecContext(ctx,
		"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, NULL)", "git:scope-config"); err != nil {
		t.Fatalf("seed scope-config: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
		"gen-config-1", "git:scope-config", base); err != nil {
		t.Fatalf("seed gen-config-1: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2",
		"gen-config-1", "git:scope-config"); err != nil {
		t.Fatalf("activate gen-config-1: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)`,
		"repo-fact-config", "git:scope-config", "gen-config-1", base,
		`{"repo_id":"repo-config","name":"repo-config"}`); err != nil {
		t.Fatalf("seed repo-config repository fact: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'content', $1, 'git', $1, $4, $4, $5::jsonb)`,
		"content-config-1", "git:scope-config", "gen-config-1", base,
		`{"repo_id":"repo-config","artifact_type":"yaml","relative_path":"services/demo/service.yaml","content":"service:\n  repoURL: https://github.com/example/repo-target-v1.git\n"}`); err != nil {
		t.Fatalf("seed repo-config v1 content fact: %v", err)
	}

	for _, id := range []string{"target-v1", "target-v2"} {
		scopeID := "git:scope-" + id
		genID := "gen-" + id
		repoID := "repo-" + id
		if _, err := db.ExecContext(ctx,
			"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, NULL)", scopeID); err != nil {
			t.Fatalf("seed scope %q: %v", scopeID, err)
		}
		if _, err := db.ExecContext(ctx,
			"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
			genID, scopeID, base); err != nil {
			t.Fatalf("seed generation %q: %v", genID, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)`,
			"repo-fact-"+repoID, scopeID, genID, base,
			`{"repo_id":"`+repoID+`","name":"`+repoID+`"}`); err != nil {
			t.Fatalf("seed repository fact for %q: %v", repoID, err)
		}
	}
}

// mustJSONPayload builds the file-fact payload JSON for the ArgoCD fixture,
// matching the shape TestDeferredBackfillPartitionMemoArgoCDCarveOutAlwaysReloads
// constructs inline via fmt.Sprintf.
func mustJSONPayload(t *testing.T, repoID, artifactType, relativePath, content string) string {
	t.Helper()
	escaped := ""
	for _, r := range content {
		switch r {
		case '\n':
			escaped += `\n`
		case '"':
			escaped += `\"`
		default:
			escaped += string(r)
		}
	}
	return `{"repo_id":"` + repoID + `","artifact_type":"` + artifactType + `","relative_path":"` + relativePath + `","content":"` + escaped + `"}`
}
