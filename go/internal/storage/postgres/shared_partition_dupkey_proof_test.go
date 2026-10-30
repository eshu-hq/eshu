// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// sharedPartitionDupKeyProofDSN reads the throwaway live-Postgres DSN this
// proof runs against, following the same ESHU_POSTGRES_DSN convention as the
// sibling derived-evidence fencing proof
// (ingestion_derived_evidence_fencing_proof_test.go).
func sharedPartitionDupKeyProofDSN() string {
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
}

func TestDeferredBackfillSharedScopeGenerationPublishesOneRowPerPartition(t *testing.T) {
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

	repoGenerations, err := backfill.LoadActiveRepositoryGenerations(ctx, SQLDB{DB: db})
	if err != nil {
		t.Fatalf("backfill.LoadActiveRepositoryGenerations() error = %v, want nil", err)
	}
	for _, repoID := range []string{"repo-alpha", "repo-beta"} {
		if _, ok := repoGenerations[repoID]; !ok {
			t.Fatalf("%s missing from active repository generations: %+v", repoID, repoGenerations)
		}
	}

	// One repository per batch, so the shared partition is assembled from two
	// separate committed batches before the fan-in publishes it once.
	store.maintenanceBatchSize = 1
	store.maintenanceWorkers = 1

	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	assertPhaseStateRowCount(t, ctx, db, "scope-shared", "gen-shared", 1)
	assertPartitionMemoRowCount(t, ctx, db, "scope-shared", "gen-shared", 1)
}

// TestDeferredBackfillDistinctScopesPublishOneRowEach guards against
// over-collapsing the dedupe fix: two repositories in two DISTINCT
// scope+generation partitions (the ordinary one-repo-per-scope shape) must
// still publish one phase row and one memo row EACH, not merge onto a single
// row.
func TestDeferredBackfillDistinctScopesPublishOneRowEach(t *testing.T) {
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

	store.maintenanceBatchSize = 1
	store.maintenanceWorkers = 1

	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	assertPhaseStateRowCount(t, ctx, db, "scope-gamma", "gen-gamma", 1)
	assertPartitionMemoRowCount(t, ctx, db, "scope-gamma", "gen-gamma", 1)
	assertPhaseStateRowCount(t, ctx, db, "scope-delta", "gen-delta", 1)
	assertPartitionMemoRowCount(t, ctx, db, "scope-delta", "gen-delta", 1)
}

// commitSharedPartitionRepositories onboards one or more "repository" facts
// under the same scope+generation through the real CommitScopeGeneration path,
// so backfill.LoadActiveRepositoryGenerations resolves each repoID to that shared
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
		testfixtures.FactChannel(envelopes),
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

	// Pin pg_trgm to public before creating the schema (#9572): the
	// bootstrap's unqualified CREATE EXTENSION would otherwise install the
	// opclass into this private schema, hiding it from sibling schemas on
	// the same shared proof database. The advisory-locked shared install
	// serializes concurrent bootstraps on one database.
	postgresproof.InstallTrigramExtension(ctx, t, db)
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

// TestSharedPartitionDupKeySchemasShareTrigramLive is the #9572 regression
// for this PR's own pins. Two sibling schemas bootstrap in one shared
// database through openSharedPartitionDupKeySchema, which pins pg_trgm to
// public before the first CREATE SCHEMA: without the pin, the second
// bootstrap fails at migration 004 with `operator class "gin_trgm_ops" does
// not exist` (SQLSTATE 42704). Both schemas stay open at once, covering
// coexisting siblings, and each proves its 004 trigram indexes valid.
func TestSharedPartitionDupKeySchemasShareTrigramLive(t *testing.T) {
	dsn := sharedPartitionDupKeyProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the shared-partition trigram-sharing proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	first := openSharedPartitionDupKeySchema(t, ctx, dsn)
	second := openSharedPartitionDupKeySchema(t, ctx, dsn)
	assertSharedPartitionTrigramIndexesLive(t, ctx, first, "first")
	assertSharedPartitionTrigramIndexesLive(t, ctx, second, "second")
}

func assertSharedPartitionTrigramIndexesLive(t *testing.T, ctx context.Context, db *sql.DB, name string) {
	t.Helper()
	for _, index := range []string{"content_files_content_trgm_idx", "content_entities_source_trgm_idx"} {
		var valid bool
		if err := db.QueryRowContext(ctx, `SELECT indisvalid FROM pg_index WHERE indexrelid = to_regclass($1)`, index).Scan(&valid); err != nil {
			t.Fatalf("%s schema: trigram index %s lookup: %v", name, index, err)
		}
		if !valid {
			t.Fatalf("%s schema: trigram index %s is invalid", name, index)
		}
	}
}
