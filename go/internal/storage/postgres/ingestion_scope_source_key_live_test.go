// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// DSN-gated persist-then-readback proof for the #5192 contract chain,
// exercising the real upsertIngestionScope write path (ingestion.go:451)
// against live Postgres instead of the hermetic scopeSourceKey unit pins in
// ingestion_scope_source_key_test.go. Reuses
// openRepositoryFreshnessDBIntegrationSchema (#5148,
// repository_freshness_db_integration_schema_test.go), which already applies
// the ingestion_scopes DDL in an isolated throwaway schema, so this adds one
// focused test rather than a new schema-bootstrap path. Skips cleanly when
// ESHU_POSTGRES_DSN is unset.
//
// The readback SELECT inlines the same coalesce(payload->>'repo_id',
// payload->>'id', scope_id) expression as repositoryCatalogIDExpr
// (go/internal/query/content_reader_repository_catalog.go, pinned by
// TestRepositoryCatalogIDExprCoalesceOrder) rather than importing
// internal/query: internal/query already imports internal/storage/postgres
// (see admin_store.go), so importing internal/query here would be a package
// cycle.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestIngestionScopeSourceKeyMatchesCatalogIDExprLiveDB upserts a
// repository-kind scope through the real production write path
// (upsertIngestionScope) with a buildScope-shaped Metadata map, then reads
// the persisted row back with the SQL text ListRepositories/MatchRepositories
// actually run and asserts source_key equals the catalog id expression. This
// proves the full producer -> storage -> catalog chain agrees on live
// Postgres, not just that each half's Go helper is internally consistent.
func TestIngestionScopeSourceKeyMatchesCatalogIDExprLiveDB(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN is not set; skipping ingestion scope source_key/catalog-id Postgres proof")
	}

	ctx := context.Background()
	db := openRepositoryFreshnessDBIntegrationSchema(t, ctx, dsn)

	now := time.Date(2026, time.July, 13, 9, 0, 0, 0, time.UTC)
	const repoID = "r_5192_live_proof"

	scopeValue := scope.IngestionScope{
		ScopeID:       "git-repository-scope:" + repoID,
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  repoID,
		Metadata: map[string]string{
			"repo_id":    repoID,
			"repo_name":  "eshu-5192-live-proof",
			"source_key": repoID,
		},
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-5192-live-proof",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusActive,
		TriggerKind:  scope.TriggerKindSnapshot,
	}

	if err := upsertIngestionScope(ctx, SQLDB{DB: db}, scopeValue, generation); err != nil {
		t.Fatalf("upsertIngestionScope() error = %v, want nil", err)
	}

	var sourceKey, catalogID string
	err := db.QueryRowContext(ctx, `
		SELECT source_key,
		       coalesce(payload->>'repo_id', payload->>'id', scope_id) AS catalog_id
		FROM ingestion_scopes
		WHERE scope_id = $1 AND scope_kind = 'repository'
	`, scopeValue.ScopeID).Scan(&sourceKey, &catalogID)
	if err != nil {
		t.Fatalf("read back persisted ingestion_scopes row: %v", err)
	}

	if sourceKey == "" {
		t.Fatal("persisted source_key is empty, want non-empty")
	}
	if sourceKey != catalogID {
		t.Fatalf("persisted source_key = %q, catalog id expression = %q; want equal", sourceKey, catalogID)
	}
	if sourceKey != repoID {
		t.Fatalf("persisted source_key = %q, want repo.ID %q", sourceKey, repoID)
	}
}
