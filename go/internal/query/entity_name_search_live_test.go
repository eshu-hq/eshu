// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

func TestGlobalEntityNameAPIDifferentialAndPerformanceLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		2*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedGlobalEntityNameProofCorpus(t, ctx, db)

	reader := NewContentReader(db)
	codeHandler := &CodeHandler{Content: reader, Profile: ProfileLocalAuthoritative}
	entityHandler := &EntityHandler{Content: reader, Profile: ProfileLocalAuthoritative}
	for _, tc := range []struct {
		name      string
		body      string
		auth      *AuthContext
		reference string
		args      []any
	}{
		{name: "exact duplicates all scope", body: `{"query":"Server","exact":true,"limit":50}`, reference: "entity_name = $1", args: []any{"Server"}},
		{name: "literal substring metacharacters", body: `{"query":"Server_100%","limit":50}`, reference: "position($1 in entity_name) > 0", args: []any{"Server_100%"}},
		{name: "literal substring backslash", body: `{"query":"Server\\Path","limit":50}`, reference: "position($1 in entity_name) > 0", args: []any{`Server\Path`}},
		{name: "language before limit", body: `{"query":"Server","language":"go","exact":true,"limit":50}`, reference: "entity_name = $1 AND language = $2", args: []any{"Server", "go"}},
		{
			name: "scoped before limit", body: `{"query":"Server","exact":true,"limit":50}`,
			auth:      &AuthContext{Mode: AuthModeScoped, AllowedRepositoryIDs: []string{"repo-a"}},
			reference: "entity_name = $1 AND repo_id = $2", args: []any{"Server", "repo-a"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v0/code/search", bytes.NewBufferString(tc.body))
			if tc.auth != nil {
				request = request.WithContext(ContextWithAuthContext(request.Context(), *tc.auth))
			}
			recorder := httptest.NewRecorder()
			started := time.Now()
			codeHandler.handleSearch(recorder, request)
			duration := time.Since(started)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
			}
			if duration > 500*time.Millisecond {
				t.Fatalf("API duration = %s, want <=500ms", duration)
			}
			got := codeSearchEntityIDs(t, recorder.Body.Bytes())
			want := referenceEntityIDs(t, ctx, db, tc.reference, tc.args...)
			if !slices.Equal(got, want) {
				t.Fatalf("entity IDs = %v, want same-data reference %v", got, want)
			}
		})
	}
	assertEntityNameSearchLargeCatalogPlan(t, ctx, db)

	request := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve", bytes.NewBufferString(`{"name":"is_valid","type":"guard","limit":50}`))
	recorder := httptest.NewRecorder()
	started := time.Now()
	entityHandler.resolveEntity(recorder, request)
	if duration := time.Since(started); duration > 500*time.Millisecond {
		t.Fatalf("typed entity API duration = %s, want <=500ms", duration)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("typed entity status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode typed entity response: %v", err)
	}
	entities, _ := response["entities"].([]any)
	if len(entities) != 1 || entities[0].(map[string]any)["entity_id"] != "guard-good" {
		t.Fatalf("typed semantic entities = %#v, want only guard-good", entities)
	}
}

func seedGlobalEntityNameProofCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
)
VALUES
  ('scope-a', 'repository', 'git', 'repo-a', 'git', 'repo-a', clock_timestamp(), clock_timestamp(), 'active', '{"repo_id":"repo-a","name":"Repository A"}'),
  ('scope-b', 'repository', 'git', 'repo-b', 'git', 'repo-b', clock_timestamp(), clock_timestamp(), 'active', '{"repo_id":"repo-b","name":"Repository B"}');
INSERT INTO content_entities (
  entity_id, repo_id, relative_path, entity_type, entity_name, start_line,
  end_line, language, source_cache, metadata, indexed_at
) VALUES
  ('server-a-go', 'repo-a', 'a.go', 'Function', 'Server', 1, 2, 'go', '', '{}', clock_timestamp()),
  ('server-a-ts', 'repo-a', 'a.ts', 'Function', 'Server', 1, 2, 'typescript', '', '{}', clock_timestamp()),
  ('server-b-go', 'repo-b', 'b.go', 'Function', 'Server', 1, 2, 'go', '', '{}', clock_timestamp()),
  ('server-lower', 'repo-b', 'lower.go', 'Function', 'server', 1, 2, 'go', '', '{}', clock_timestamp()),
  ('literal', 'repo-a', 'literal.go', 'Function', 'Server_100%', 1, 2, 'go', '', '{}', clock_timestamp()),
  ('literal-backslash', 'repo-a', 'literal-backslash.go', 'Function', E'Server\\Path', 1, 2, 'go', '', '{}', clock_timestamp()),
  ('wildcard-only', 'repo-a', 'wild.go', 'Function', 'ServerX100ZZ', 1, 2, 'go', '', '{}', clock_timestamp()),
  ('guard-good', 'repo-a', 'guard.ex', 'Function', 'is_valid', 1, 2, 'elixir', '', '{"semantic_kind":"guard"}', clock_timestamp()),
  ('guard-plain', 'repo-a', 'plain.ex', 'Function', 'is_valid', 3, 4, 'elixir', '', '{}', clock_timestamp());
INSERT INTO content_entities (
  entity_id, repo_id, relative_path, entity_type, entity_name, start_line,
  end_line, language, source_cache, metadata, indexed_at
)
SELECT 'noise-' || n, 'repo-' || (n % 100), 'noise/' || n || '.go',
       'Function', 'Noise' || n, 1, 2, 'go', '', '{}', clock_timestamp()
FROM generate_series(1, 100000) AS n;
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
)
SELECT 'catalog-proof-' || md5(n::text), 'repository', 'git', 'noise-repo-' || n, 'git',
       'noise-repo-' || n, clock_timestamp(), clock_timestamp(), 'active',
       jsonb_build_object('repo_id', 'noise-repo-' || n, 'name', 'Noise Repository ' || n)
FROM generate_series(1, 100000) AS n;
ANALYZE content_entities;
ANALYZE ingestion_scopes;
`); err != nil {
		t.Fatalf("seed global entity-name proof corpus: %v", err)
	}
	var catalogCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM ingestion_scopes").Scan(&catalogCount); err != nil {
		t.Fatalf("count large repository catalog: %v", err)
	}
	if catalogCount != 100002 {
		t.Fatalf("repository catalog count = %d, want 100002", catalogCount)
	}
}

type entityNameSearchPlanNode struct {
	RelationName     string                     `json:"Relation Name"`
	ActualLoops      float64                    `json:"Actual Loops"`
	SharedHitBlocks  int64                      `json:"Shared Hit Blocks"`
	SharedReadBlocks int64                      `json:"Shared Read Blocks"`
	Plans            []entityNameSearchPlanNode `json:"Plans"`
}

func assertEntityNameSearchLargeCatalogPlan(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	search, empty, err := normalizeEntityNameSearch(EntityNameSearch{
		Name: "Server", Match: EntityNameMatchExact, Scope: EntityNameScopeAll, Limit: 51,
	})
	if err != nil || empty {
		t.Fatalf("normalize plan proof search: empty=%t err=%v", empty, err)
	}
	productionQuery, args := buildEntityNameSearchQuery(search)
	var raw []byte
	if err := db.QueryRowContext(
		ctx,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+productionQuery,
		args...,
	).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN production entity-name query: %v", err)
	}
	var plan []struct {
		Plan          entityNameSearchPlanNode `json:"Plan"`
		ExecutionTime float64                  `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil || len(plan) != 1 {
		t.Fatalf("decode entity-name query plan: count=%d err=%v", len(plan), err)
	}
	loops, hitBlocks, readBlocks := ingestionScopePlanTotals(plan[0].Plan)
	if loops != 1 {
		t.Fatalf("ingestion_scopes plan loops = %.0f, want one catalog hydration; plan=%s", loops, raw)
	}
	if plan[0].ExecutionTime > 500 {
		t.Fatalf("large-catalog execution time = %.3fms, want <=500ms; plan=%s", plan[0].ExecutionTime, raw)
	}
	t.Logf(
		"production entity-name plan: execution_ms=%.3f ingestion_scopes_loops=%.0f shared_hit_blocks=%d shared_read_blocks=%d",
		plan[0].ExecutionTime,
		loops,
		hitBlocks,
		readBlocks,
	)
}

func ingestionScopePlanTotals(node entityNameSearchPlanNode) (float64, int64, int64) {
	var loops float64
	var hitBlocks, readBlocks int64
	if node.RelationName == "ingestion_scopes" {
		loops += node.ActualLoops
		hitBlocks += node.SharedHitBlocks
		readBlocks += node.SharedReadBlocks
	}
	for _, child := range node.Plans {
		childLoops, childHitBlocks, childReadBlocks := ingestionScopePlanTotals(child)
		loops += childLoops
		hitBlocks += childHitBlocks
		readBlocks += childReadBlocks
	}
	return loops, hitBlocks, readBlocks
}

func codeSearchEntityIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode code search response: %v", err)
	}
	rows, _ := response["results"].([]any)
	ids := make([]string, 0, len(rows))
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		ids = append(ids, row["entity_id"].(string))
	}
	return ids
}

func referenceEntityIDs(t *testing.T, ctx context.Context, db *sql.DB, predicate string, args ...any) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT entity_id
FROM content_entities
WHERE `+predicate+`
ORDER BY repo_id, relative_path, start_line, entity_name, entity_id
LIMIT 50
`, args...)
	if err != nil {
		t.Fatalf("reference entity query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan reference entity ID: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate reference entity IDs: %v", err)
	}
	return ids
}
