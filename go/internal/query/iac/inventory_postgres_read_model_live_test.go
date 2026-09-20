// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// recordingQueryer wraps an inventoryQueryer and records every statement
// text, so tests can assert which store path (active-inventory CTE vs
// infra_resource_entities table) served a read.
type recordingQueryer struct {
	inner inventoryQueryer
	seen  *[]string
}

func (r recordingQueryer) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	*r.seen = append(*r.seen, query)
	return r.inner.QueryContext(ctx, query, args...)
}

// TestPostgresIaCInventoryReadModelLive proves the #6858 table path: with the
// backfill marker recorded, unscoped search and summary come from
// infra_resource_entities and agree with the CTE path on identities, order,
// totals, and facets; scoped reads and reads without the marker stay on the
// CTE. Candidates from the table carry the backfill's empty generation
// provenance.
func TestPostgresIaCInventoryReadModelLive(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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

	schema := fmt.Sprintf("iac_read_model_proof_%d", time.Now().UnixNano())
	if _, err := conn.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
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
		t.Fatalf("create proof fact tables: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
CREATE TABLE infra_resource_entities (
  entity_id text PRIMARY KEY,
  repo_id text NOT NULL,
  scope_id text NOT NULL DEFAULT '',
  generation_id text NOT NULL DEFAULT '',
  relative_path text NOT NULL,
  label text NOT NULL,
  entity_name text NOT NULL,
  kind text NOT NULL DEFAULT '',
  resource_type text NOT NULL DEFAULT '',
  data_type text NOT NULL DEFAULT '',
  provider text NOT NULL DEFAULT '',
  environment text NOT NULL DEFAULT '',
  resource_service text NOT NULL DEFAULT '',
  resource_category text NOT NULL DEFAULT '',
  service_kind text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL
);
CREATE TABLE infra_resource_entity_backfill_markers (
  marker_name text PRIMARY KEY,
  completed_at timestamptz NOT NULL
);
CREATE TABLE infra_resource_entity_dirty_repos (
  repo_id text PRIMARY KEY,
  marked_at timestamptz NOT NULL
)`); err != nil {
		t.Fatalf("create proof read model tables: %v", err)
	}
	seedIaCInventoryLiveProof(t, ctx, conn)

	unscoped := querycontract.RepositoryAccessFilter{AllScopes: true}
	scoped := issue5262ScopedAccess("repository:r1", "scope:s1")

	searches := []InventorySearch{
		{Kind: resourceKindResource, Limit: 10},
		{Kind: resourceKindResource, Query: "aws_s3_bucket", Limit: 10},
		{Kind: resourceKindResource, Query: "logging.tf", Limit: 10},
		{Kind: resourceKindResource, Type: "aws_s3_bucket", Limit: 10},
		{Kind: resourceKindResource, Provider: "aws", Limit: 10},
		{Kind: resourceKindModule, Limit: 10},
		{Kind: resourceKindModule, Query: "module", Limit: 10},
		{Kind: resourceKindDataSource, Limit: 10},
		{Kind: resourceKindResource, Repository: "repository:r2", Limit: 10},
		{Kind: resourceKindResource, Limit: 1},
	}

	var cteStatements []string
	cteStore := NewPostgresIaCInventoryStore(recordingQueryer{inner: conn, seen: &cteStatements})

	// Pre-marker phase: everything serves from the CTE.
	cteCandidates := make([][]InventoryCandidate, len(searches))
	for i, search := range searches {
		got, err := cteStore.SearchActive(ctx, search, unscoped)
		if err != nil {
			t.Fatalf("CTE search %d: %v", i, err)
		}
		cteCandidates[i] = got
	}
	cteSummary, err := cteStore.Summary(ctx, unscoped, 200)
	if err != nil {
		t.Fatalf("CTE summary: %v", err)
	}
	// walkPages resumes the (entity_name, entity_id) keyset one row per
	// page until the store reports an empty page, recording every page.
	walkPages := func(store PostgresIaCInventoryStore) [][]InventoryCandidate {
		t.Helper()
		var pages [][]InventoryCandidate
		cursor := InventorySearch{Kind: resourceKindResource, Limit: 1}
		for {
			page, err := store.SearchActive(ctx, cursor, unscoped)
			if err != nil {
				t.Fatalf("walk page after %q/%q: %v", cursor.AfterName, cursor.AfterID, err)
			}
			pages = append(pages, page)
			if len(page) == 0 {
				return pages
			}
			last := page[len(page)-1]
			cursor.AfterName, cursor.AfterID = last.Name, last.ID
		}
	}
	cteWalk := walkPages(cteStore)
	for _, stmt := range cteStatements {
		if strings.Contains(stmt, "infra_resource_entities") {
			t.Fatalf("pre-marker read touched the table: %.120s", stmt)
		}
	}

	// Mirror the current CTE identities into the table with the backfill's
	// empty generation provenance, then record the marker.
	insertTableRow := func(entityID, repoID, relativePath, label, name, resourceType, dataType, provider string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, `
INSERT INTO infra_resource_entities (
  entity_id, repo_id, relative_path, label, entity_name,
  resource_type, data_type, provider, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())`,
			entityID, repoID, relativePath, label, name, resourceType, dataType, provider); err != nil {
			t.Fatalf("insert table row %s: %v", entityID, err)
		}
	}
	insertTableRow("content-entity:resource-app", "repository:r2", "replacement.tf",
		"TerraformResource", "aws_s3_bucket.replacement", "aws_s3_bucket", "", "aws")
	insertTableRow("content-entity:resource-logs", "repository:r1", "logging.tf",
		"TerraformResource", "aws_s3_bucket.logs", "aws_s3_bucket", "", "aws")
	insertTableRow("content-entity:module", "repository:r1", "modules/network/main.tf",
		"TerraformModule", "network", "module", "", "")
	insertTableRow("content-entity:data", "repository:r1", "identity.tf",
		"TerraformDataSource", "data.aws_caller_identity.current", "", "aws_caller_identity", "aws")
	insertTableRow("content-entity:private", "repository:r2", "private.tf",
		"TerraformResource", "aws_s3_bucket.private", "aws_s3_bucket", "", "aws")
	if _, err := conn.ExecContext(ctx, `
INSERT INTO infra_resource_entity_backfill_markers (marker_name, completed_at)
VALUES ($1, now())`, inventory.BackfillMarker); err != nil {
		t.Fatalf("record backfill marker: %v", err)
	}

	var tableStatements []string
	tableStore := NewPostgresIaCInventoryStore(recordingQueryer{inner: conn, seen: &tableStatements})

	// Post-marker phase: unscoped reads come from the table with parity.
	tableCandidates := make([][]InventoryCandidate, len(searches))
	for i, search := range searches {
		got, err := tableStore.SearchActive(ctx, search, unscoped)
		if err != nil {
			t.Fatalf("table search %d: %v", i, err)
		}
		tableCandidates[i] = got
		if len(got) != len(cteCandidates[i]) {
			t.Fatalf("search %d: table returned %d candidates, CTE %d", i, len(got), len(cteCandidates[i]))
		}
		for j := range got {
			if got[j].ID != cteCandidates[i][j].ID || got[j].Name != cteCandidates[i][j].Name {
				t.Fatalf("search %d candidate %d: table %#v, CTE %#v", i, j, got[j], cteCandidates[i][j])
			}
			if got[j].GenerationID != "" {
				t.Fatalf("search %d candidate %d: table generation = %q, want backfill empty", i, j, got[j].GenerationID)
			}
		}
	}
	tableSummary, err := tableStore.Summary(ctx, unscoped, 200)
	if err != nil {
		t.Fatalf("table summary: %v", err)
	}
	if !reflect.DeepEqual(tableSummary, cteSummary) {
		t.Fatalf("table summary = %#v, want CTE-identical %#v", tableSummary, cteSummary)
	}
	// Keyset continuation: a CTE-issued cursor must resume identically on
	// the table path, page by page through the whole kind.
	tableWalk := walkPages(tableStore)
	if len(tableWalk) != len(cteWalk) {
		t.Fatalf("table walk took %d pages, CTE %d", len(tableWalk), len(cteWalk))
	}
	for i := range tableWalk {
		if len(tableWalk[i]) != len(cteWalk[i]) {
			t.Fatalf("walk page %d: table %d rows, CTE %d", i, len(tableWalk[i]), len(cteWalk[i]))
		}
		for j := range tableWalk[i] {
			if tableWalk[i][j].ID != cteWalk[i][j].ID || tableWalk[i][j].Name != cteWalk[i][j].Name {
				t.Fatalf("walk page %d row %d: table %#v, CTE %#v", i, j, tableWalk[i][j], cteWalk[i][j])
			}
		}
	}
	sawTable := false
	for _, stmt := range tableStatements {
		if strings.Contains(stmt, "FROM infra_resource_entities") {
			sawTable = true
		}
	}
	if !sawTable {
		t.Fatal("post-marker unscoped reads never touched infra_resource_entities")
	}

	// Scoped reads stay on the CTE even with the marker recorded.
	var scopedStatements []string
	scopedStore := NewPostgresIaCInventoryStore(recordingQueryer{inner: conn, seen: &scopedStatements})
	if _, err := scopedStore.SearchActive(ctx, InventorySearch{Kind: resourceKindResource, Limit: 10}, scoped); err != nil {
		t.Fatalf("scoped search: %v", err)
	}
	if _, err := scopedStore.Summary(ctx, scoped, 200); err != nil {
		t.Fatalf("scoped summary: %v", err)
	}
	for _, stmt := range scopedStatements {
		if strings.Contains(stmt, "infra_resource_entities") {
			t.Fatalf("scoped read touched the table: %.120s", stmt)
		}
	}

	// Clearing the marker returns unscoped reads to the CTE.
	if _, err := conn.ExecContext(ctx, "DELETE FROM infra_resource_entity_backfill_markers"); err != nil {
		t.Fatalf("clear backfill marker: %v", err)
	}
	var fallbackStatements []string
	fallbackStore := NewPostgresIaCInventoryStore(recordingQueryer{inner: conn, seen: &fallbackStatements})
	fallback, err := fallbackStore.SearchActive(ctx, InventorySearch{Kind: resourceKindResource, Limit: 10}, unscoped)
	if err != nil {
		t.Fatalf("fallback search: %v", err)
	}
	if len(fallback) != len(cteCandidates[0]) {
		t.Fatalf("fallback returned %d candidates, want CTE %d", len(fallback), len(cteCandidates[0]))
	}
	for _, stmt := range fallbackStatements {
		if strings.Contains(stmt, "FROM infra_resource_entities") {
			t.Fatalf("pre-ready fallback touched the table: %.120s", stmt)
		}
	}
}

// TestPostgresIaCInventoryFailedGenerationServesLastProjected pins the
// intended currency divergence the owner called out on #6858: a repository
// whose newest generation failed or is pending has content and graph rows
// but no active row in scope_generations. The active-inventory CTE drops
// such repositories; the table keeps their last-projected content (the
// documented infra-inventory semantic the aggregate reads already ship).
// The table answer is the intended one on this path because it agrees with
// the authoritative graph the route hydrates from, where the CTE view would
// serve partial content or disagree and fail.
func TestPostgresIaCInventoryFailedGenerationServesLastProjected(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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

	schema := fmt.Sprintf("iac_failed_gen_proof_%d", time.Now().UnixNano())
	if _, err := conn.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := conn.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop proof schema: %v", err)
		}
	})
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatalf("set proof search path: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE scope_generations (scope_id text NOT NULL, generation_id text NOT NULL, status text NOT NULL, ingested_at timestamptz NOT NULL, PRIMARY KEY (scope_id, generation_id))`,
		`CREATE TABLE fact_records (fact_id text PRIMARY KEY, scope_id text NOT NULL, generation_id text NOT NULL, fact_kind text NOT NULL, is_tombstone boolean NOT NULL DEFAULT false, payload jsonb NOT NULL)`,
		`CREATE TABLE infra_resource_entities (entity_id text PRIMARY KEY, repo_id text NOT NULL, scope_id text NOT NULL DEFAULT '', generation_id text NOT NULL DEFAULT '', relative_path text NOT NULL, label text NOT NULL, entity_name text NOT NULL, kind text NOT NULL DEFAULT '', resource_type text NOT NULL DEFAULT '', data_type text NOT NULL DEFAULT '', provider text NOT NULL DEFAULT '', environment text NOT NULL DEFAULT '', resource_service text NOT NULL DEFAULT '', resource_category text NOT NULL DEFAULT '', service_kind text NOT NULL DEFAULT '', updated_at timestamptz NOT NULL)`,
		`CREATE TABLE infra_resource_entity_backfill_markers (marker_name text PRIMARY KEY, completed_at timestamptz NOT NULL)`,
		`CREATE TABLE infra_resource_entity_dirty_repos (repo_id text PRIMARY KEY, marked_at timestamptz NOT NULL)`,
	} {
		if _, err := conn.ExecContext(ctx, ddl); err != nil {
			t.Fatalf("create proof table: %v", err)
		}
	}
	// Scope s9 has only a failed generation: no active row, so the CTE
	// drops the repository while content (and the graph) still holds it.
	if _, err := conn.ExecContext(ctx, `
INSERT INTO scope_generations (scope_id, generation_id, status, ingested_at)
VALUES ('scope:s9', 'generation:failed', 'failed', now())`); err != nil {
		t.Fatalf("seed failed generation: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, payload)
VALUES ('fact:orphan', 'scope:s9', 'generation:failed', 'content_entity',
  '{"entity_id": "content-entity:orphaned-cache", "entity_name": "zzz_orphaned_cache", "entity_type": "TerraformResource", "relative_path": "orphan.tf", "repo_id": "repository:r9", "entity_metadata": {"resource_type": "orphaned_widget"}}')`); err != nil {
		t.Fatalf("seed failed fact: %v", err)
	}
	// The table keeps the last-projected content with backfill provenance.
	if _, err := conn.ExecContext(ctx, `
INSERT INTO infra_resource_entities (entity_id, repo_id, relative_path, label, entity_name, resource_type, updated_at)
VALUES ('content-entity:orphaned-cache', 'repository:r9', 'orphan.tf', 'TerraformResource', 'zzz_orphaned_cache', 'orphaned_widget', now())`); err != nil {
		t.Fatalf("seed table row: %v", err)
	}

	unscoped := querycontract.RepositoryAccessFilter{AllScopes: true}
	store := NewPostgresIaCInventoryStore(conn)
	orphan := InventorySearch{Kind: resourceKindResource, Query: "orphan", Limit: 10}

	cteCandidates, err := store.SearchActive(ctx, orphan, unscoped)
	if err != nil {
		t.Fatalf("CTE search: %v", err)
	}
	if len(cteCandidates) != 0 {
		t.Fatalf("CTE search = %#v, want empty: the failed generation has no active row", cteCandidates)
	}
	cteSummary, err := store.Summary(ctx, unscoped, 200)
	if err != nil {
		t.Fatalf("CTE summary: %v", err)
	}
	if cteSummary.Total != 0 {
		t.Fatalf("CTE summary total = %d, want 0", cteSummary.Total)
	}

	if _, err := conn.ExecContext(ctx, `
INSERT INTO infra_resource_entity_backfill_markers (marker_name, completed_at)
VALUES ($1, now())`, inventory.BackfillMarker); err != nil {
		t.Fatalf("record backfill marker: %v", err)
	}
	tableCandidates, err := store.SearchActive(ctx, orphan, unscoped)
	if err != nil {
		t.Fatalf("table search: %v", err)
	}
	if len(tableCandidates) != 1 || tableCandidates[0].ID != "content-entity:orphaned-cache" ||
		tableCandidates[0].Name != "zzz_orphaned_cache" {
		t.Fatalf("table search = %#v, want the retained last-projected row", tableCandidates)
	}
	tableSummary, err := store.Summary(ctx, unscoped, 200)
	if err != nil {
		t.Fatalf("table summary: %v", err)
	}
	if tableSummary.Total != 1 {
		t.Fatalf("table summary total = %d, want 1", tableSummary.Total)
	}
	foundRepo := false
	for _, facet := range tableSummary.Repositories {
		if facet.Value == "repository:r9" && facet.Count == 1 {
			foundRepo = true
		}
	}
	if !foundRepo {
		t.Fatalf("table repositories = %#v, want repository:r9 with count 1", tableSummary.Repositories)
	}
}
