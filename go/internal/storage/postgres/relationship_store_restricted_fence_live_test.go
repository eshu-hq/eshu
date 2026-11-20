// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"
)

// Per-item read-set waits (#10877): restricted fence live proofs.
//
// On the full production bootstrap schema, on a disposable database: the
// blocking computation excludes proven non-producers and only them, the
// restricted read admits exactly the global read's rows under a complete
// fence, and it defers with the verdict-time blocking set while a producer
// is open. Gated on ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN.

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10877 restricted corpus fence: blocking computation and read verdict on the production schema with a disposable database
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

// seedNarrowFenceScope seeds an active scope whose current relationship
// generation carries the given status.
func seedNarrowFenceScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID, genID, rgStatus string, at time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', $3, '{}'::jsonb)`,
		scopeID, at, genID); err != nil {
		t.Fatalf("seed narrow scope %s: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO relationship_generations (generation_id, scope, status, created_at, activated_at)
VALUES ($1, $2, $3, $4, $4)`, genID, scopeID, rgStatus, at); err != nil {
		t.Fatalf("seed narrow relationship generation %s: %v", genID, err)
	}
}

// seedNarrowFenceRow writes one resolved row into a generation.
func seedNarrowFenceRow(t *testing.T, ctx context.Context, db *sql.DB, id, generationID, sourceRepo, targetRepo string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO resolved_relationships
  (resolved_id, generation_id, source_repo_id, target_repo_id, relationship_type,
   confidence, evidence_count, rationale, resolution_source, details)
VALUES ($1, $2, $3, $4, 'DEPLOYS_FROM', 0.9, 1, 'narrow fence proof', 'inferred', '{}'::jsonb)`,
		id, generationID, sourceRepo, targetRepo); err != nil {
		t.Fatalf("seed narrow resolved row %s: %v", id, err)
	}
}

// seedNarrowLiveDeploymentMapping seeds one live deployment_mapping queue row
// for a scope, which keeps its holder status unexcludable (#10877: a scope
// with live producer work can never be a proven non-producer).
func seedNarrowLiveDeploymentMapping(t *testing.T, ctx context.Context, db *sql.DB, scopeID, genID string, at time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status
) VALUES ($2, $1, 'synthetic', FALSE, $3, $3, 'active')
ON CONFLICT DO NOTHING`, scopeID, genID, at); err != nil {
		t.Fatalf("seed narrow scope generation %s: %v", genID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
    last_attempt_at, payload, created_at, updated_at
) VALUES ($1, $2, $3, 'reducer', 'deployment_mapping', 'pending', 0, $4, '{}'::jsonb, $4, $4)`,
		"dm-live-"+scopeID, scopeID, genID, at); err != nil {
		t.Fatalf("seed live deployment mapping for %s: %v", scopeID, err)
	}
}

func openNarrowFenceDB(t *testing.T, ctx context.Context) (*sql.DB, string) {
	t.Helper()
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #10877 narrowed fence proofs")
	}
	return openReducerFairnessDBWithSchema(t, ctx, dsn)
}

func TestRestrictedFenceBlockingScopesLive(t *testing.T) {
	ctx := context.Background()
	db, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)
	store := NewRelationshipStore(SQLDB{DB: db})

	// Consumer scope: active generation, resolves nothing itself.
	seedNarrowFenceScope(t, ctx, db, "scope-consumer", "gen-consumer", "active", at)
	// Producer B: retired generation holding the fence, with a resolved row
	// touching the read set. Blocking.
	seedNarrowFenceScope(t, ctx, db, "scope-producer", "gen-producer-old", "retired", at)
	seedNarrowFenceRow(t, ctx, db, "row-b-a", "gen-producer-old", "repo-b", "repo-a")
	// Unrelated C: retired generation holding the fence, rows never touching
	// the read set, no live producer work. Proven non-producer: excluded.
	seedNarrowFenceScope(t, ctx, db, "scope-unrelated", "gen-unrelated-old", "retired", at)
	seedNarrowFenceRow(t, ctx, db, "row-c-z", "gen-unrelated-old", "repo-c", "repo-z")
	// Busy D: retired generation with no touching rows but live
	// deployment_mapping work. Never excludable: included.
	seedNarrowFenceScope(t, ctx, db, "scope-busy", "gen-busy-old", "retired", at)
	seedNarrowFenceRow(t, ctx, db, "row-d-z", "gen-busy-old", "repo-d", "repo-z")
	seedNarrowLiveDeploymentMapping(t, ctx, db, "scope-busy", "gen-busy-old", at)

	blocking, err := store.BlockingRelationshipScopes(ctx, []string{"repo-a"})
	if err != nil {
		t.Fatalf("BlockingRelationshipScopes() error = %v", err)
	}
	want := []string{"scope-busy", "scope-producer"}
	if !slices.Equal(blocking, want) {
		t.Fatalf("BlockingRelationshipScopes([repo-a]) = %v, want %v", blocking, want)
	}

	// The restricted read defers with exactly that blocking set and no rows.
	rows, gotBlocking, complete, err := store.GetResolvedRelationshipsForReposWithRestrictedFence(ctx, []string{"repo-a"})
	if err != nil {
		t.Fatalf("restricted read error = %v", err)
	}
	if complete {
		t.Fatal("restricted read complete = true, want false while a producer generation is retired")
	}
	if len(rows) != 0 {
		t.Fatalf("restricted read rows = %d, want 0 on an incomplete fence", len(rows))
	}
	if !slices.Equal(gotBlocking, want) {
		t.Fatalf("restricted read blocking = %v, want %v", gotBlocking, want)
	}
}

func TestRestrictedFenceReadMatchesGlobalLive(t *testing.T) {
	ctx := context.Background()
	db, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)
	store := NewRelationshipStore(SQLDB{DB: db})

	seedNarrowFenceScope(t, ctx, db, "scope-g", "gen-g", "active", at)
	seedNarrowFenceRow(t, ctx, db, "row-g-1", "gen-g", "repo-g", "repo-a")
	seedNarrowFenceScope(t, ctx, db, "scope-h", "gen-h", "active", at)
	seedNarrowFenceRow(t, ctx, db, "row-h-1", "gen-h", "repo-h", "repo-a")

	// Empty read set is vacuous: nothing to wait on.
	rows, blocking, complete, err := store.GetResolvedRelationshipsForReposWithRestrictedFence(ctx, nil)
	if err != nil {
		t.Fatalf("restricted empty read error = %v", err)
	}
	if !complete || len(rows) != 0 || len(blocking) != 0 {
		t.Fatalf("restricted empty read = complete:%v rows:%d blocking:%v, want true/0/[]",
			complete, len(rows), blocking)
	}

	global, globalComplete, err := store.GetResolvedRelationshipsForReposWithCorpusFence(ctx, []string{"repo-a"})
	if err != nil {
		t.Fatalf("global read error = %v", err)
	}
	narrowed, narrowBlocking, narrowComplete, err := store.GetResolvedRelationshipsForReposWithRestrictedFence(ctx, []string{"repo-a"})
	if err != nil {
		t.Fatalf("restricted read error = %v", err)
	}
	if !globalComplete || !narrowComplete {
		t.Fatalf("complete = global:%v narrowed:%v, want true/true on an all-active corpus",
			globalComplete, narrowComplete)
	}
	if len(narrowBlocking) != 0 {
		t.Fatalf("restricted blocking = %v, want empty on an all-active corpus", narrowBlocking)
	}
	if len(narrowed) != len(global) {
		t.Fatalf("restricted rows = %d, global rows = %d: the narrowed read must admit exactly the global set",
			len(narrowed), len(global))
	}
	globalIDs := map[string]bool{}
	for _, row := range global {
		globalIDs[row.SourceRepoID+"|"+row.TargetRepoID] = true
	}
	for _, row := range narrowed {
		if !globalIDs[row.SourceRepoID+"|"+row.TargetRepoID] {
			t.Fatalf("restricted row %s -> %s missing from the global set", row.SourceRepoID, row.TargetRepoID)
		}
	}

	// The fingerprinted restricted read computes the same input fingerprint
	// as the global fingerprinted read over the same rows: the reopen gate
	// compares them across runs.
	_, _, globalFP, err := store.GetFingerprintedResolvedRelationshipsForReposWithCorpusFence(ctx, []string{"repo-a"})
	if err != nil {
		t.Fatalf("global fingerprinted read error = %v", err)
	}
	_, _, _, narrowFP, err := store.GetFingerprintedResolvedRelationshipsForReposWithRestrictedFence(ctx, []string{"repo-a"})
	if err != nil {
		t.Fatalf("restricted fingerprinted read error = %v", err)
	}
	if globalFP.Value == "" || narrowFP.Value != globalFP.Value {
		t.Fatalf("fingerprints differ: global %q vs restricted %q", globalFP.Value, narrowFP.Value)
	}
	if len(narrowFP.RepoIDs) != 1 || narrowFP.RepoIDs[0] != "repo-a" {
		t.Fatalf("restricted fingerprint repos = %v, want [repo-a]", narrowFP.RepoIDs)
	}

	// A holder that owns no row of the read set does not narrow the verdict:
	// retiring an unrelated scope leaves the restricted read complete.
	seedNarrowFenceScope(t, ctx, db, "scope-far", "gen-far-old", "retired", at)
	_, _, completeAfter, err := store.GetResolvedRelationshipsForReposWithRestrictedFence(ctx, []string{"repo-a"})
	if err != nil {
		t.Fatalf("restricted read after unrelated retire error = %v", err)
	}
	if !completeAfter {
		t.Fatal("restricted read complete = false, want true: the retired scope produces nothing for this read set")
	}
}
