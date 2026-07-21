// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	pgstore "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestPostgresIaCInventoryStoreLive proves migration execution, generation
// rollover, duplicate collapse, authorization, stable keyset pagination, and
// cancellation against an explicitly selected disposable Postgres database.
func TestPostgresIaCInventoryStoreLive(t *testing.T) {
	if os.Getenv("ESHU_IAC_INVENTORY_LIVE") != "1" {
		t.Skip("set ESHU_IAC_INVENTORY_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close Postgres: %v", err)
		}
	})
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated connection: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close dedicated connection: %v", err)
		}
	})

	schema := fmt.Sprintf("iac_inventory_proof_%d", time.Now().UnixNano())
	if _, err := conn.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := conn.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop proof schema: %v", err)
		}
	})
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatalf("set proof search path: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
CREATE TABLE scope_generations (
  scope_id text NOT NULL,
  generation_id text NOT NULL,
  status text NOT NULL,
  ingested_at timestamptz NOT NULL,
  PRIMARY KEY (scope_id, generation_id)
);
CREATE TABLE fact_records (
  fact_id text PRIMARY KEY,
  scope_id text NOT NULL,
  generation_id text NOT NULL,
  fact_kind text NOT NULL,
  is_tombstone boolean NOT NULL DEFAULT false,
  payload jsonb NOT NULL
)`); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := conn.ExecContext(ctx, pgstore.MigrationSQL("iac_active_inventory_index")); err != nil {
			t.Fatalf("apply IaC inventory migration attempt %d: %v", attempt, err)
		}
	}
	seedIaCInventoryLiveProof(t, ctx, conn)

	store := NewPostgresIaCInventoryStore(conn)
	access := issue5262ScopedAccess("repository:r1", "scope:s1")
	started := time.Now()
	first, err := store.SearchActive(ctx, iacInventorySearch{
		Kind:  iacResourceKindResource,
		Limit: 1,
	}, access)
	if err != nil {
		t.Fatalf("search first current page: %v", err)
	}
	if len(first) != 1 || first[0].ID != "content-entity:resource-app" {
		t.Fatalf("first page = %#v, want current app resource", first)
	}
	second, err := store.SearchActive(ctx, iacInventorySearch{
		Kind:      iacResourceKindResource,
		AfterName: first[0].Name,
		AfterID:   first[0].ID,
		Limit:     1,
	}, access)
	if err != nil {
		t.Fatalf("search second current page: %v", err)
	}
	if len(second) != 1 || second[0].ID != "content-entity:resource-logs" {
		t.Fatalf("second page = %#v, want current logs resource", second)
	}
	if first[0].ID == second[0].ID {
		t.Fatalf("stable pages repeated identity %q", first[0].ID)
	}

	search, err := store.SearchActive(ctx, iacInventorySearch{
		Kind:  iacResourceKindResource,
		Query: "logging.tf",
		Limit: 10,
	}, access)
	if err != nil {
		t.Fatalf("search current source path: %v", err)
	}
	if len(search) != 1 || search[0].ID != "content-entity:resource-logs" ||
		search[0].GenerationID != "generation:active" {
		t.Fatalf("source search = %#v, want active logs identity", search)
	}

	summary, err := store.Summary(ctx, access, 200)
	if err != nil {
		t.Fatalf("summarize current inventory: %v", err)
	}
	if summary.Total != 4 || summary.ByKind[iacResourceKindResource] != 2 ||
		summary.ByKind[iacResourceKindModule] != 1 ||
		summary.ByKind[iacResourceKindDataSource] != 1 {
		t.Fatalf("current summary = %#v, want total 4 with 2/1/1 kinds", summary)
	}

	allScopes := repositoryAccessFilter{allScopes: true}
	newer, err := store.SearchActive(ctx, iacInventorySearch{
		Kind:  iacResourceKindResource,
		Query: "replacement",
		Limit: 10,
	}, allScopes)
	if err != nil {
		t.Fatalf("search duplicate replacement: %v", err)
	}
	if len(newer) != 1 || newer[0].ID != "content-entity:resource-app" ||
		newer[0].GenerationID != "generation:other" {
		t.Fatalf("cross-scope duplicate = %#v, want newest active generation", newer)
	}
	historical, err := store.SearchActive(ctx, iacInventorySearch{
		Kind:  iacResourceKindResource,
		Query: "historical",
		Limit: 10,
	}, allScopes)
	if err != nil {
		t.Fatalf("search historical inventory: %v", err)
	}
	if len(historical) != 0 {
		t.Fatalf("historical inventory leaked: %#v", historical)
	}
	unauthorized, err := store.SearchActive(ctx, iacInventorySearch{
		Kind:  iacResourceKindResource,
		Query: "private",
		Limit: 10,
	}, access)
	if err != nil {
		t.Fatalf("search out-of-grant inventory: %v", err)
	}
	if len(unauthorized) != 0 {
		t.Fatalf("out-of-grant inventory leaked: %#v", unauthorized)
	}
	literalWildcard, err := store.SearchActive(ctx, iacInventorySearch{
		Kind:  iacResourceKindResource,
		Query: "%",
		Limit: 10,
	}, access)
	if err != nil {
		t.Fatalf("search literal wildcard: %v", err)
	}
	if len(literalWildcard) != 0 {
		t.Fatalf("literal wildcard search matched inventory: %#v", literalWildcard)
	}

	cancelled, cancelNow := context.WithCancel(ctx)
	cancelNow()
	if _, err := store.SearchActive(cancelled, iacInventorySearch{
		Kind:  iacResourceKindResource,
		Limit: 10,
	}, allScopes); err == nil {
		t.Fatal("cancelled inventory search succeeded")
	}

	var valid bool
	if err := conn.QueryRowContext(ctx, `
SELECT index.indisvalid
FROM pg_index AS index
JOIN pg_class AS relation ON relation.oid = index.indexrelid
WHERE relation.relname = 'fact_records_iac_active_inventory_idx'
`).Scan(&valid); err != nil {
		t.Fatalf("inspect IaC inventory index: %v", err)
	}
	if !valid {
		t.Fatal("IaC inventory index is not valid")
	}
	t.Logf("current IaC live proof completed in %s", time.Since(started))
}

func issue5262ScopedAccess(repositoryID, scopeID string) repositoryAccessFilter {
	return repositoryAccessFilter{
		allowedRepositoryIDs: []string{repositoryID},
		allowedScopeIDs:      []string{scopeID},
		allowed: map[string]struct{}{
			repositoryID: {},
			scopeID:      {},
		},
	}
}

func seedIaCInventoryLiveProof(t *testing.T, ctx context.Context, conn *sql.Conn) {
	t.Helper()
	for _, generation := range []struct {
		scopeID, generationID, status, ingestedAt string
	}{
		{"scope:s1", "generation:old", "superseded", "2026-01-01T00:00:00Z"},
		{"scope:s1", "generation:active", "active", "2026-01-02T00:00:00Z"},
		{"scope:s2", "generation:other", "active", "2026-01-03T00:00:00Z"},
	} {
		if _, err := conn.ExecContext(ctx, `
INSERT INTO scope_generations (scope_id, generation_id, status, ingested_at)
VALUES ($1, $2, $3, $4::timestamptz)`, generation.scopeID, generation.generationID,
			generation.status, generation.ingestedAt); err != nil {
			t.Fatalf("insert generation %s: %v", generation.generationID, err)
		}
	}

	insertIaCFact := func(
		factID, scopeID, generationID, entityID, entityName, entityType,
		repositoryID, relativePath, itemType, provider string,
		tombstone bool,
	) {
		t.Helper()
		metadata := map[string]string{"provider": provider}
		if entityType == "TerraformDataSource" {
			metadata["data_type"] = itemType
		} else {
			metadata["resource_type"] = itemType
		}
		payload, err := json.Marshal(map[string]any{
			"entity_id":       entityID,
			"entity_name":     entityName,
			"entity_type":     entityType,
			"repo_id":         repositoryID,
			"relative_path":   relativePath,
			"entity_metadata": metadata,
		})
		if err != nil {
			t.Fatalf("marshal fact %s: %v", factID, err)
		}
		if _, err := conn.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, is_tombstone, payload
) VALUES ($1, $2, $3, 'content_entity', $4, $5::jsonb)`, factID, scopeID,
			generationID, tombstone, payload); err != nil {
			t.Fatalf("insert fact %s: %v", factID, err)
		}
	}

	insertIaCFact("fact:old", "scope:s1", "generation:old", "content-entity:old",
		"aws_s3_bucket.historical", "TerraformResource", "repository:r1", "old.tf",
		"aws_s3_bucket", "aws", false)
	insertIaCFact("fact:app-a", "scope:s1", "generation:active", "content-entity:resource-app",
		"aws_s3_bucket.app", "TerraformResource", "repository:r1", "app.tf",
		"aws_s3_bucket", "aws", false)
	insertIaCFact("fact:app-b", "scope:s1", "generation:active", "content-entity:resource-app",
		"aws_s3_bucket.app", "TerraformResource", "repository:r1", "app.tf",
		"aws_s3_bucket", "aws", false)
	insertIaCFact("fact:logs", "scope:s1", "generation:active", "content-entity:resource-logs",
		"aws_s3_bucket.logs", "TerraformResource", "repository:r1", "logging.tf",
		"aws_s3_bucket", "aws", false)
	insertIaCFact("fact:module", "scope:s1", "generation:active", "content-entity:module",
		"network", "TerraformModule", "repository:r1", "modules/network/main.tf",
		"module", "", false)
	insertIaCFact("fact:data", "scope:s1", "generation:active", "content-entity:data",
		"data.aws_caller_identity.current", "TerraformDataSource", "repository:r1", "identity.tf",
		"aws_caller_identity", "aws", false)
	insertIaCFact("fact:tombstone", "scope:s1", "generation:active", "content-entity:tombstone",
		"aws_s3_bucket.deleted", "TerraformResource", "repository:r1", "deleted.tf",
		"aws_s3_bucket", "aws", true)
	insertIaCFact("fact:replacement", "scope:s2", "generation:other", "content-entity:resource-app",
		"aws_s3_bucket.replacement", "TerraformResource", "repository:r2", "replacement.tf",
		"aws_s3_bucket", "aws", false)
	insertIaCFact("fact:private", "scope:s2", "generation:other", "content-entity:private",
		"aws_s3_bucket.private", "TerraformResource", "repository:r2", "private.tf",
		"aws_s3_bucket", "aws", false)
}
