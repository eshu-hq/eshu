// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_global_name_comparison

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	issue5318BaselineGlobalCodeQuerySource = "origin/main@2395e65c4edd9e8009a91be4eec55c3a80212f3e:go/internal/query/code_graph_search_query.go sha256=4cba27767b92a7846c946f5440a156caec9442389215c14076488e5fea183bc7"
	issue5318BaselineResolveEntitySource   = "origin/main@2395e65c4edd9e8009a91be4eec55c3a80212f3e:go/internal/query/entity.go sha256=10705d5d42dfef252a410938099f0eca94826b7cbbabf8d70aecf4eafb814ef7"
)

func TestIssue5318SameLogicalCorpusOldGraphAndNewContentRoute(t *testing.T) {
	ctx, db, driver, databaseName := openIssue5318SameLogicalCorpus(t)

	oldQuery := issue5318BaselineGlobalCodeQuery()
	graphSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode: neo4jdriver.AccessModeRead, DatabaseName: databaseName,
	})
	defer func() { _ = graphSession.Close(context.Background()) }()
	oldStarted := time.Now()
	oldResult, err := graphSession.Run(ctx, oldQuery, map[string]any{"query": "Target", "limit": 50})
	if err != nil {
		t.Fatalf("run baseline global graph route: %v", err)
	}
	oldIDs := make([]string, 0, 12)
	for oldResult.Next(ctx) {
		value, _ := oldResult.Record().Get("entity_id")
		oldIDs = append(oldIDs, fmt.Sprint(value))
	}
	if err := oldResult.Err(); err != nil {
		t.Fatalf("iterate baseline graph route: %v", err)
	}
	oldDuration := time.Since(oldStarted)

	newStarted := time.Now()
	newRows, err := NewContentReader(db).SearchEntityNames(ctx, EntityNameSearch{
		Name: "Target", Match: EntityNameMatchExact, Scope: EntityNameScopeAll, Limit: 50,
	})
	if err != nil {
		t.Fatalf("run accepted content route: %v", err)
	}
	newDuration := time.Since(newStarted)
	newIDs := make([]string, 0, len(newRows))
	for _, row := range newRows {
		newIDs = append(newIDs, row.EntityID)
	}
	sort.Strings(oldIDs)
	sort.Strings(newIDs)
	if !slices.Equal(oldIDs, newIDs) || len(newIDs) != 12 {
		t.Fatalf("same-corpus identity diff: old=%v new=%v", oldIDs, newIDs)
	}

	search, _, err := normalizeEntityNameSearch(EntityNameSearch{
		Name: "Target", Match: EntityNameMatchExact, Scope: EntityNameScopeAll, Limit: 50,
	})
	if err != nil {
		t.Fatalf("normalize accepted plan query: %v", err)
	}
	newQuery, args := buildEntityNameSearchQuery(search)
	var postgresPlan []byte
	if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+newQuery, args...).Scan(&postgresPlan); err != nil {
		t.Fatalf("explain accepted content route: %v", err)
	}
	profileResult, err := graphSession.Run(ctx, "PROFILE "+oldQuery, map[string]any{"query": "Target", "limit": 50})
	if err != nil {
		t.Fatalf("profile baseline global graph route: %v", err)
	}
	profileSummary, err := profileResult.Consume(ctx)
	if err != nil {
		t.Fatalf("consume baseline graph profile: %v", err)
	}
	profile := profileSummary.Profile()
	if profile == nil {
		t.Fatal("baseline graph PROFILE returned no plan")
	}
	t.Logf(
		"same_logical_rows=12000 matches=12 symmetric_identity_diff=0/0 baseline_source=%q old_graph_seconds=%.6f new_postgres_seconds=%.6f cross_store_latency_non_comparable=true old_graph_profile=%s new_postgres_plan=%s",
		issue5318BaselineGlobalCodeQuerySource,
		oldDuration.Seconds(),
		newDuration.Seconds(),
		summarizeIssue5318Profile(profile),
		postgresPlan,
	)
}

func openIssue5318SameLogicalCorpus(
	t *testing.T,
) (context.Context, *sql.DB, neo4jdriver.DriverWithContext, string) {
	t.Helper()
	if os.Getenv("ESHU_TEST_ENTITY_NAME_GRAPH_DISPOSABLE") != "1" {
		t.Skip("ESHU_TEST_ENTITY_NAME_GRAPH_DISPOSABLE=1 is required")
	}
	graphURI := os.Getenv("ESHU_TEST_ENTITY_NAME_GRAPH_BOLT_URI")
	if graphURI == "" {
		t.Skip("ESHU_TEST_ENTITY_NAME_GRAPH_BOLT_URI is not set")
	}
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		2*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}

	driver, err := neo4jdriver.NewDriverWithContext(graphURI, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open disposable graph: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify disposable graph: %v", err)
	}
	databaseName := os.Getenv("ESHU_TEST_ENTITY_NAME_GRAPH_DATABASE")
	if databaseName == "" {
		databaseName = "neo4j"
	}
	runID := fmt.Sprintf("issue-5318-%d", time.Now().UnixNano())
	seedIssue5318SameLogicalCorpus(t, ctx, db, driver, databaseName, runID)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		session := driver.NewSession(cleanupCtx, neo4jdriver.SessionConfig{
			AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: databaseName,
		})
		defer func() { _ = session.Close(cleanupCtx) }()
		_, _ = session.Run(cleanupCtx, "MATCH (n {proof_run_id: $run_id}) DETACH DELETE n", map[string]any{"run_id": runID})
	})
	return ctx, db, driver, databaseName
}

func TestIssue5318SameLogicalCorpusOldResolveEntityGraphAndNewContentIndex(t *testing.T) {
	ctx, db, driver, databaseName := openIssue5318SameLogicalCorpus(t)
	graphSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode: neo4jdriver.AccessModeRead, DatabaseName: databaseName,
	})
	defer func() { _ = graphSession.Close(context.Background()) }()

	scopedRepositoryIDs := []string{"repository:r_000", "repository:r_040", "repository:r_080"}
	tests := []struct {
		name      string
		access    repositoryAccessFilter
		search    EntityNameSearch
		wantCount int
	}{
		{
			name:   "all-scope typed semantic",
			access: repositoryAccessFilter{allScopes: true},
			search: EntityNameSearch{
				Name: "Target", Match: EntityNameMatchExact, Scope: EntityNameScopeAll,
				EntityType: "Function", MetadataKey: "semantic_kind", MetadataValue: "guard", Limit: 50,
			},
			wantCount: 6,
		},
		{
			name:   "scoped typed semantic",
			access: issue5318ScopedRepositoryAccess(scopedRepositoryIDs),
			search: EntityNameSearch{
				Name: "Target", Match: EntityNameMatchExact, Scope: EntityNameScopeRepositories,
				RepositoryIDs: scopedRepositoryIDs, EntityType: "Function",
				MetadataKey: "semantic_kind", MetadataValue: "guard", Limit: 50,
			},
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baselineQuery, baselineParams := issue5318BaselineBuildResolveEntityGraphQuery(
				resolveEntityRequest{Name: "Target", Type: "guard"},
				50,
				tt.access,
			)
			oldStarted := time.Now()
			oldResult, err := graphSession.Run(ctx, baselineQuery, baselineParams)
			if err != nil {
				t.Fatalf("run baseline resolve-entity graph query: %v", err)
			}
			oldIDs := make([]string, 0, tt.wantCount)
			for oldResult.Next(ctx) {
				value, _ := oldResult.Record().Get("id")
				oldIDs = append(oldIDs, fmt.Sprint(value))
			}
			if err := oldResult.Err(); err != nil {
				t.Fatalf("iterate baseline resolve-entity graph query: %v", err)
			}
			oldDuration := time.Since(oldStarted)

			newStarted := time.Now()
			newRows, err := NewContentReader(db).SearchEntityNames(ctx, tt.search)
			if err != nil {
				t.Fatalf("run accepted entity-name content query: %v", err)
			}
			newDuration := time.Since(newStarted)
			newIDs := make([]string, 0, len(newRows))
			for _, row := range newRows {
				newIDs = append(newIDs, row.EntityID)
			}
			sort.Strings(oldIDs)
			sort.Strings(newIDs)
			if !slices.Equal(oldIDs, newIDs) || len(newIDs) != tt.wantCount {
				t.Fatalf("typed resolve identity diff: old=%v new=%v want_count=%d", oldIDs, newIDs, tt.wantCount)
			}

			normalized, empty, err := normalizeEntityNameSearch(tt.search)
			if err != nil || empty {
				t.Fatalf("normalize accepted entity-name plan query: empty=%v err=%v", empty, err)
			}
			acceptedQuery, acceptedArgs := buildEntityNameSearchQuery(normalized)
			var rawPlan []byte
			if err := db.QueryRowContext(
				ctx,
				"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+acceptedQuery,
				acceptedArgs...,
			).Scan(&rawPlan); err != nil {
				t.Fatalf("explain accepted entity-name query: %v", err)
			}
			var acceptedPlan []struct {
				Plan          entityNameSearchPlanNode `json:"Plan"`
				ExecutionTime float64                  `json:"Execution Time"`
			}
			if err := json.Unmarshal(rawPlan, &acceptedPlan); err != nil || len(acceptedPlan) != 1 {
				t.Fatalf("decode accepted entity-name plan: count=%d err=%v", len(acceptedPlan), err)
			}

			profileResult, err := graphSession.Run(ctx, "PROFILE "+baselineQuery, baselineParams)
			if err != nil {
				t.Fatalf("profile baseline resolve-entity graph query: %v", err)
			}
			profileSummary, err := profileResult.Consume(ctx)
			if err != nil {
				t.Fatalf("consume baseline resolve-entity profile: %v", err)
			}
			profile := profileSummary.Profile()
			if profile == nil {
				t.Fatal("baseline resolve-entity PROFILE returned no plan")
			}
			t.Logf(
				"case=%q same_logical_rows=12000 typed_matches=%d symmetric_identity_diff=0/0 baseline_source=%q old_graph_seconds=%.6f new_postgres_seconds=%.6f new_postgres_plan_execution_ms=%.3f cross_store_latency_non_comparable=true old_graph_profile=%s accepted_sql_plan=%s",
				tt.name,
				tt.wantCount,
				issue5318BaselineResolveEntitySource,
				oldDuration.Seconds(),
				newDuration.Seconds(),
				acceptedPlan[0].ExecutionTime,
				summarizeIssue5318Profile(profile),
				rawPlan,
			)
		})
	}
}

func issue5318ScopedRepositoryAccess(repositoryIDs []string) repositoryAccessFilter {
	allowed := make(map[string]struct{}, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		allowed[repositoryID] = struct{}{}
	}
	return repositoryAccessFilter{
		allowedRepositoryIDs: append([]string(nil), repositoryIDs...),
		allowed:              allowed,
	}
}

// issue5318BaselineBuildResolveEntityGraphQuery retains the exact global query
// shape from issue5318BaselineResolveEntitySource for same-data comparison.
func issue5318BaselineBuildResolveEntityGraphQuery(
	req resolveEntityRequest,
	limit int,
	access repositoryAccessFilter,
) (string, map[string]any) {
	repositoryAnchored := req.RepoID != ""
	cypher := `MATCH (e) WHERE e.name = $name`
	params := map[string]any{"name": req.Name}
	if repositoryAnchored {
		cypher = `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e) WHERE e.name = $name`
		params["repo_id"] = req.RepoID
	}
	if req.Type != "" {
		graphLabel, semanticKey, semanticValue, ok := resolveGraphEntityType(req.Type)
		if ok {
			cypher += " AND $type IN labels(e)"
			params["type"] = graphLabel
			if semanticKey != "" {
				cypher += fmt.Sprintf(" AND coalesce(e.%s, '') = $semantic_filter", semanticKey)
				params["semantic_filter"] = semanticValue
			}
		}
	}
	if !repositoryAnchored && access.scoped() {
		cypher += `
			AND EXISTS {
				MATCH (e)<-[:CONTAINS]-(scopeFile:File)<-[:REPO_CONTAINS]-(scopeRepo:Repository)
				WHERE ` + access.graphCondition("scopeRepo") + `
			}
		`
		params = access.graphParams(params)
	}
	if !repositoryAnchored {
		cypher += `
			OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
		`
		if access.scoped() {
			cypher += `
			WHERE ` + access.graphCondition("r") + `
			`
		}
	}
	cypher += `
		RETURN e.id as id, labels(e) as labels, e.name as name,
		       f.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		ORDER BY e.name
		LIMIT $limit
	`
	params["limit"] = limit + 1
	return cypher, params
}

func issue5318BaselineGlobalCodeQuery() string {
	return `
		MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
		WHERE e.name = $query
		RETURN e.id as entity_id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		ORDER BY e.name
		LIMIT $limit
	`
}

func seedIssue5318SameLogicalCorpus(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	driver neo4jdriver.DriverWithContext,
	databaseName string,
	runID string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
)
SELECT 'scope-' || repo_number, 'repository', 'git', 'repository:r_' || lpad(repo_number::text, 3, '0'), 'git',
       'repository:r_' || lpad(repo_number::text, 3, '0'), clock_timestamp(), clock_timestamp(), 'active',
       jsonb_build_object('repo_id', 'repository:r_' || lpad(repo_number::text, 3, '0'), 'name', 'Repository ' || repo_number)
FROM generate_series(0, 119) AS repo_number;
INSERT INTO content_entities (
  entity_id, repo_id, relative_path, entity_type, entity_name, start_line,
  end_line, language, source_cache, metadata, indexed_at
)
SELECT 'entity-' || lpad(entity_number::text, 5, '0'),
       'repository:r_' || lpad((entity_number / 100)::text, 3, '0'),
       'src/file_' || lpad((entity_number / 100)::text, 3, '0') || '.go',
       'Function', CASE WHEN entity_number % 1000 = 0 THEN 'Target' ELSE 'Noise' || entity_number END,
       entity_number + 1, entity_number + 2, 'go', '',
       CASE WHEN entity_number % 2000 = 0
            THEN '{"semantic_kind":"guard"}'::jsonb ELSE '{}'::jsonb END,
       clock_timestamp()
FROM generate_series(0, 11999) AS entity_number;
ANALYZE content_entities;
ANALYZE ingestion_scopes;
`); err != nil {
		t.Fatalf("seed same-corpus Postgres rows: %v", err)
	}

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: databaseName,
	})
	defer func() { _ = session.Close(context.Background()) }()
	for start := 0; start < 12000; start += 1000 {
		rows := make([]map[string]any, 0, 1000)
		for entityNumber := start; entityNumber < start+1000; entityNumber++ {
			repoNumber := entityNumber / 100
			name := fmt.Sprintf("Noise%d", entityNumber)
			if entityNumber%1000 == 0 {
				name = "Target"
			}
			semanticKind := ""
			if entityNumber%2000 == 0 {
				semanticKind = "guard"
			}
			rows = append(rows, map[string]any{
				"entity_id": fmt.Sprintf("entity-%05d", entityNumber),
				"repo_id":   fmt.Sprintf("repository:r_%03d", repoNumber),
				"repo_name": fmt.Sprintf("Repository %d", repoNumber),
				"file_path": fmt.Sprintf("src/file_%03d.go", repoNumber),
				"name":      name, "semantic_kind": semanticKind,
				"start_line": entityNumber + 1, "end_line": entityNumber + 2,
			})
		}
		_, err := session.Run(ctx, `
UNWIND $rows AS row
MERGE (repo:Repository {id: row.repo_id})
SET repo.name = row.repo_name, repo.proof_run_id = $run_id
MERGE (repo)-[:REPO_CONTAINS]->(file:File {relative_path: row.file_path})
SET file.language = 'go', file.proof_run_id = $run_id
CREATE (file)-[:CONTAINS]->(entity:Function {
  id: row.entity_id, name: row.name, language: 'go', start_line: row.start_line,
  end_line: row.end_line, semantic_kind: row.semantic_kind, proof_run_id: $run_id
})
`, map[string]any{"rows": rows, "run_id": runID})
		if err != nil {
			t.Fatalf("seed same-corpus graph batch %d: %v", start/1000, err)
		}
	}
}

func summarizeIssue5318Profile(plan neo4jdriver.ProfiledPlan) string {
	result := fmt.Sprintf("%s(hits=%d,rows=%d)", plan.Operator(), plan.DbHits(), plan.Records())
	for _, child := range plan.Children() {
		result += ">" + summarizeIssue5318Profile(child)
	}
	return result
}
