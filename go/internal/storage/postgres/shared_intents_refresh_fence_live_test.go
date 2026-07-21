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

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestSharedIntentStoreGenerationRefreshFenceAgainstPostgres proves the exact
// durable lifecycle used by the shared-projection worker: an exact
// same-generation retry reuses its deterministic intent ID and does not reopen
// completed work, while an older generation cannot open a next-generation
// refresh that reuses its source-run ID. Set
// ESHU_SHARED_REFRESH_FENCE_PROOF_DSN or ESHU_POSTGRES_DSN to run it.
func TestSharedIntentStoreGenerationRefreshFenceAgainstPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_SHARED_REFRESH_FENCE_PROOF_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		t.Skip("set ESHU_SHARED_REFRESH_FENCE_PROOF_DSN or ESHU_POSTGRES_DSN to run the real-Postgres refresh-fence proof")
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })

	schemaName := fmt.Sprintf("shared_refresh_fence_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set proof search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, SharedIntentSchemaSQL()); err != nil {
		t.Fatalf("create shared-intent proof schema: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	store := NewSharedIntentStore(SQLDB{DB: db})
	initial := refreshFenceProofIntent("gen-1", now)
	if err := store.UpsertIntents(ctx, []reducer.SharedProjectionIntentRow{initial}); err != nil {
		t.Fatalf("upsert initial refresh: %v", err)
	}
	if err := store.MarkIntentsCompleted(ctx, []string{initial.IntentID}, now.Add(time.Second)); err != nil {
		t.Fatalf("complete initial refresh: %v", err)
	}
	assertGenerationRefreshFenceReady(t, ctx, store, "gen-1", initial.PartitionKey, true)

	exactRetry := refreshFenceProofIntent("gen-1", now.Add(2*time.Second))
	if exactRetry.IntentID != initial.IntentID {
		t.Fatalf("exact retry intent ID = %q, want stable %q", exactRetry.IntentID, initial.IntentID)
	}
	if err := store.UpsertIntents(ctx, []reducer.SharedProjectionIntentRow{exactRetry}); err != nil {
		t.Fatalf("upsert exact same-generation retry: %v", err)
	}
	pending, err := store.ListPendingDomainIntents(ctx, reducer.DomainSQLRelationships, 10)
	if err != nil {
		t.Fatalf("list pending after exact retry: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending intents after exact retry = %d, want 0; completed durable work must not reopen", len(pending))
	}
	assertGenerationRefreshFenceReady(t, ctx, store, "gen-1", initial.PartitionKey, true)

	nextGeneration := refreshFenceProofIntent("gen-2", now.Add(3*time.Second))
	if nextGeneration.IntentID == initial.IntentID {
		t.Fatalf("next-generation intent ID reused %q; generation must participate in durable identity", initial.IntentID)
	}
	if err := store.UpsertIntents(ctx, []reducer.SharedProjectionIntentRow{nextGeneration}); err != nil {
		t.Fatalf("upsert next-generation refresh: %v", err)
	}
	assertGenerationRefreshFenceReady(t, ctx, store, "gen-2", nextGeneration.PartitionKey, false)
	if err := store.MarkIntentsCompleted(ctx, []string{nextGeneration.IntentID}, now.Add(4*time.Second)); err != nil {
		t.Fatalf("complete next-generation refresh: %v", err)
	}
	assertGenerationRefreshFenceReady(t, ctx, store, "gen-2", nextGeneration.PartitionKey, true)
}

func refreshFenceProofIntent(generationID string, createdAt time.Time) reducer.SharedProjectionIntentRow {
	const repositoryID = "repo-sql"
	return reducer.BuildSharedProjectionIntent(reducer.SharedProjectionIntentInput{
		ProjectionDomain: reducer.DomainSQLRelationships,
		PartitionKey:     "sql_relationships:refresh:v1:whole:" + repositoryID,
		ScopeID:          "scope-sql",
		AcceptanceUnitID: repositoryID,
		RepositoryID:     repositoryID,
		SourceRunID:      "run-reused",
		GenerationID:     generationID,
		Payload: map[string]any{
			"repo_id":     repositoryID,
			"intent_type": "repo_refresh",
			"action":      "refresh",
		},
		CreatedAt: createdAt,
	})
}

func assertGenerationRefreshFenceReady(
	t *testing.T,
	ctx context.Context,
	store *SharedIntentStore,
	generationID string,
	partitionKey string,
	want bool,
) {
	t.Helper()
	got, err := store.HasCompletedAcceptanceUnitSourceRunGenerationPartitionDomainIntents(
		ctx,
		reducer.SharedProjectionAcceptanceKey{
			ScopeID:          "scope-sql",
			AcceptanceUnitID: "repo-sql",
			SourceRunID:      "run-reused",
		},
		generationID,
		partitionKey,
		reducer.DomainSQLRelationships,
	)
	if err != nil {
		t.Fatalf("query generation refresh fence: %v", err)
	}
	if got != want {
		t.Fatalf("generation refresh fence ready = %v, want %v", got, want)
	}
}
