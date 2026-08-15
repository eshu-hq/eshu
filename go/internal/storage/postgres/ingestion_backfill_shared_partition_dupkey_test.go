// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Shared-partition duplicate-conflict-key proof for the deferred-backfill
// dup-key bug found by a live Ifá gate run.
//
// writeDeferredBackfillBatch (ingestion_backfill.go) loops per repository and
// appends one reducer.GraphProjectionPhaseState and one scopeGenerationPartition
// memo candidate per repo. graph_projection_phase_state's conflict target
// (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase)
// and deferred_backfill_partition_memo's conflict target (scope_id,
// generation_id) are both functions of (ScopeID, GenerationID) alone -- the
// per-repo phase/memo rows built from repoGeneration carry AcceptanceUnitID =
// ScopeID and SourceRunID = GenerationID, and a constant Keyspace/Phase. When N
// repositories in one batch share a (scope, generation) partition (the
// ingestion commit path accepts multi-repo scopes; production git sync just
// happens to commit one repo per scope), the batch upsert carries N
// byte-identical conflict keys in one INSERT ... ON CONFLICT DO UPDATE and
// Postgres rejects the whole batch transaction with SQLSTATE 21000 ("ON
// CONFLICT DO UPDATE command cannot affect row a second time"). The rollback
// means NO repository in the batch gets its readiness published, including
// well-formed ones sharing the batch with no other bug of their own.
//
// This test drives the real IngestionStore.writeDeferredBackfillBatch against a
// live Postgres instance: two repositories committed under the SAME
// scope+generation must publish exactly ONE phase row and ONE memo row for that
// shared partition (a (scope, generation) fact, not a per-repo fact), while two
// repositories in DISTINCT scopes must still publish one row each -- the fix
// must not over-collapse unrelated partitions.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// sharedPartitionDupKeyProofDSN reads the throwaway live-Postgres DSN this
// proof runs against, following the same ESHU_POSTGRES_DSN convention as the
// sibling derived-evidence fencing proof
// (ingestion_derived_evidence_fencing_proof_test.go).
func sharedPartitionDupKeyProofDSN() string {
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
}

func TestWriteDeferredBackfillBatchSharedScopeGenerationDedupesConflictKey(t *testing.T) {
	dsn := sharedPartitionDupKeyProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the deferred-backfill shared-partition dedupe proof")
	}

	ctx := context.Background()
	db := openSharedPartitionDupKeySchema(t, ctx, dsn)
	store := NewIngestionStore(SQLDB{DB: db})
	store.SkipRelationshipBackfill = true
	now := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	// repo-alpha and repo-beta committed under the SAME scope+generation in one
	// CommitScopeGeneration call -- the multi-repo-per-scope shape the ingestion
	// commit path accepts today.
	commitSharedPartitionRepositories(t, ctx, store, "scope-shared", "gen-shared", []string{"repo-alpha", "repo-beta"}, now)

	repoGenerations, err := loadActiveRepositoryGenerations(ctx, SQLDB{DB: db})
	if err != nil {
		t.Fatalf("loadActiveRepositoryGenerations() error = %v, want nil", err)
	}
	for _, repoID := range []string{"repo-alpha", "repo-beta"} {
		if _, ok := repoGenerations[repoID]; !ok {
			t.Fatalf("%s missing from active repository generations: %+v", repoID, repoGenerations)
		}
	}

	published, err := store.writeDeferredBackfillBatch(
		ctx,
		[]string{"repo-alpha", "repo-beta"},
		map[string][]relationships.EvidenceFact{},
		nil,
		"catalog-fingerprint-shared",
	)
	if err != nil {
		var sqlState interface{ SQLState() string }
		if errors.As(err, &sqlState) {
			t.Fatalf("writeDeferredBackfillBatch() error = %v (SQLSTATE %s), want nil", err, sqlState.SQLState())
		}
		t.Fatalf("writeDeferredBackfillBatch() error = %v, want nil", err)
	}
	if published != 1 {
		t.Fatalf(
			"writeDeferredBackfillBatch() published = %d readiness rows, want 1 (one per shared partition, not one per repo)",
			published,
		)
	}

	assertPhaseStateRowCount(t, ctx, db, "scope-shared", "gen-shared", 1)
	assertPartitionMemoRowCount(t, ctx, db, "scope-shared", "gen-shared", 1)
}

// TestWriteDeferredBackfillBatchDistinctScopesPublishOneRowEach guards against
// over-collapsing the dedupe fix: two repositories in two DISTINCT
// scope+generation partitions (the ordinary one-repo-per-scope shape) must
// still publish one phase row and one memo row EACH, not merge onto a single
// row.
func TestWriteDeferredBackfillBatchDistinctScopesPublishOneRowEach(t *testing.T) {
	dsn := sharedPartitionDupKeyProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the deferred-backfill shared-partition dedupe proof")
	}

	ctx := context.Background()
	db := openSharedPartitionDupKeySchema(t, ctx, dsn)
	store := NewIngestionStore(SQLDB{DB: db})
	store.SkipRelationshipBackfill = true
	now := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	commitSharedPartitionRepositories(t, ctx, store, "scope-gamma", "gen-gamma", []string{"repo-gamma"}, now)
	commitSharedPartitionRepositories(t, ctx, store, "scope-delta", "gen-delta", []string{"repo-delta"}, now.Add(time.Minute))

	published, err := store.writeDeferredBackfillBatch(
		ctx,
		[]string{"repo-gamma", "repo-delta"},
		map[string][]relationships.EvidenceFact{},
		nil,
		"catalog-fingerprint-distinct",
	)
	if err != nil {
		t.Fatalf("writeDeferredBackfillBatch() error = %v, want nil", err)
	}
	if published != 2 {
		t.Fatalf("writeDeferredBackfillBatch() published = %d readiness rows, want 2 (one per distinct partition)", published)
	}

	assertPhaseStateRowCount(t, ctx, db, "scope-gamma", "gen-gamma", 1)
	assertPartitionMemoRowCount(t, ctx, db, "scope-gamma", "gen-gamma", 1)
	assertPhaseStateRowCount(t, ctx, db, "scope-delta", "gen-delta", 1)
	assertPartitionMemoRowCount(t, ctx, db, "scope-delta", "gen-delta", 1)
}

// commitSharedPartitionRepositories onboards one or more "repository" facts
// under the same scope+generation through the real CommitScopeGeneration path,
// so loadActiveRepositoryGenerations resolves each repoID to that shared
// partition exactly as production git sync would for a multi-repo scope.
func commitSharedPartitionRepositories(
	t *testing.T,
	ctx context.Context,
	store IngestionStore,
	scopeID, generationID string,
	repoIDs []string,
	observedAt time.Time,
) {
	t.Helper()
	envelopes := make([]facts.Envelope, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		factID := "fact-" + generationID + "-" + repoID
		envelopes = append(envelopes, facts.Envelope{
			FactID:        factID,
			ScopeID:       scopeID,
			GenerationID:  generationID,
			FactKind:      "repository",
			StableFactKey: "repository:" + scopeID + ":" + repoID,
			ObservedAt:    observedAt,
			Payload:       map[string]any{"repo_id": repoID, "name": repoID},
			SourceRef:     facts.Ref{SourceSystem: "git", FactKey: factID},
		})
	}
	if err := store.CommitScopeGeneration(
		ctx,
		sharedPartitionDupKeyScope(scopeID),
		sharedPartitionDupKeyGeneration(scopeID, generationID, observedAt),
		testFactChannel(envelopes),
	); err != nil {
		t.Fatalf("commit shared-partition repositories %v: CommitScopeGeneration() error = %v, want nil", repoIDs, err)
	}
}

func sharedPartitionDupKeyScope(scopeID string) scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  scopeID,
	}
}

func sharedPartitionDupKeyGeneration(scopeID, generationID string, observedAt time.Time) scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: generationID,
		ScopeID:      scopeID,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		Status:       scope.GenerationStatusActive,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

func assertPhaseStateRowCount(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM graph_projection_phase_state WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID,
	).Scan(&got); err != nil {
		t.Fatalf("count graph_projection_phase_state rows: %v", err)
	}
	if got != want {
		t.Fatalf("graph_projection_phase_state rows for %s/%s = %d, want %d", scopeID, generationID, got, want)
	}
}

func assertPartitionMemoRowCount(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM deferred_backfill_partition_memo WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID,
	).Scan(&got); err != nil {
		t.Fatalf("count deferred_backfill_partition_memo rows: %v", err)
	}
	if got != want {
		t.Fatalf("deferred_backfill_partition_memo rows for %s/%s = %d, want %d", scopeID, generationID, got, want)
	}
}

// openSharedPartitionDupKeySchema creates an isolated throwaway schema and
// applies the full bootstrap DDL, mirroring openDerivedEvidenceFencingSchema:
// this proof exercises the real commit transaction (ingestion scopes/
// generations, fact_records, fact_work_items) plus the real deferred-backfill
// batch transaction (graph_projection_phase_state,
// deferred_backfill_partition_memo).
func openSharedPartitionDupKeySchema(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	schemaName := fmt.Sprintf("shared_partition_dupkey_%d", time.Now().UnixNano())

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create shared-partition dupkey schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply full bootstrap: %v", err)
	}
	return db
}
