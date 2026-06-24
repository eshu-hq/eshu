// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"
)

func TestContentReaderMatchRepositoriesReturnsExactMatches(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"id", "name", "path", "local_path", "remote_url", "repo_slug", "has_remote"},
			rows: [][]driver.Value{
				{"repository:r_payments", "payments", "/src/payments", "/src/payments", "", "acme/payments", false},
			},
		},
	})

	reader := NewContentReader(db)
	matches, err := reader.MatchRepositories(context.Background(), "payments")
	if err != nil {
		t.Fatalf("MatchRepositories() error = %v, want nil", err)
	}
	if got, want := len(matches), 1; got != want {
		t.Fatalf("len(matches) = %d, want %d", got, want)
	}
	if got, want := matches[0].ID, "repository:r_payments"; got != want {
		t.Fatalf("matches[0].ID = %q, want %q", got, want)
	}
}

func TestContentReaderMatchRepositoriesPrefersCanonicalRepositoryIDExpression(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"id", "name", "path", "local_path", "remote_url", "repo_slug", "has_remote"},
			rows: [][]driver.Value{
				{"repository:r_payments", "payments", "/src/payments", "/src/payments", "", "acme/payments", false},
			},
		},
	})

	reader := NewContentReader(db)
	matches, err := reader.MatchRepositories(context.Background(), "payments")
	if err != nil {
		t.Fatalf("MatchRepositories() error = %v, want nil", err)
	}
	if got, want := len(matches), 1; got != want {
		t.Fatalf("len(matches) = %d, want %d", got, want)
	}
	if got, want := matches[0].ID, "repository:r_payments"; got != want {
		t.Fatalf("matches[0].ID = %q, want %q", got, want)
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("len(recorder.queries) = %d, want %d", got, want)
	}
	if !strings.Contains(recorder.queries[0], "payload->>'repo_id'") {
		t.Fatalf("query = %q, want canonical payload repo_id selection", recorder.queries[0])
	}
	if !strings.Contains(recorder.queries[0], "scope_id = $1") {
		t.Fatalf("query = %q, want scope_id backward-compat matching", recorder.queries[0])
	}
}

func TestContentReaderResolveRepositoryRejectsAmbiguousMatches(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"id", "name", "path", "local_path", "remote_url", "repo_slug", "has_remote"},
			rows: [][]driver.Value{
				{"repository:r_one", "payments", "/src/payments-one", "/src/payments-one", "", "acme/payments-one", false},
				{"repository:r_two", "payments", "/src/payments-two", "/src/payments-two", "", "acme/payments-two", false},
			},
		},
	})

	reader := NewContentReader(db)
	_, err := reader.ResolveRepository(context.Background(), "payments")
	if err == nil {
		t.Fatal("ResolveRepository() error = nil, want non-nil")
	}
	if got, want := err.Error(), `repository selector "payments" matched multiple repositories: repository:r_one, repository:r_two`; got != want {
		t.Fatalf("ResolveRepository() error = %q, want %q", got, want)
	}
}

func TestContentReaderListRepoFilesIncludesArtifactType(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-1", ".github/workflows/deploy.yaml", "abc123", "",
					"hash-1", int64(20), "yaml", "github_actions_workflow",
				},
			},
		},
	})

	reader := NewContentReader(db)
	results, err := reader.ListRepoFiles(context.Background(), "repo-1", 10)
	if err != nil {
		t.Fatalf("ListRepoFiles() error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if got, want := results[0].ArtifactType, "github_actions_workflow"; got != want {
		t.Fatalf("ArtifactType = %q, want %q", got, want)
	}
}

func TestContentReaderListRepositoriesByLanguageUsesIndexedLanguageScope(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{
				"repo_id", "name", "path", "local_path", "remote_url", "repo_slug", "has_remote",
				"language", "file_count", "last_indexed_at",
			},
			rows: [][]driver.Value{
				{
					"repository:web", "web", "/src/web", "/src/web", "https://example.test/web", "acme/web", true,
					"typescript", int64(7), time.Date(2026, 5, 23, 14, 0, 0, 0, time.UTC),
				},
			},
		},
	})

	reader := NewContentReader(db)
	rows, err := reader.ListRepositoriesByLanguage(context.Background(), []string{"typescript", "tsx"}, 10, 0)
	if err != nil {
		t.Fatalf("ListRepositoriesByLanguage() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0].Repository.RepoSlug, "acme/web"; got != want {
		t.Fatalf("RepoSlug = %q, want %q", got, want)
	}
	if got, want := rows[0].FileCount, 7; got != want {
		t.Fatalf("FileCount = %d, want %d", got, want)
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("len(recorder.queries) = %d, want %d", got, want)
	}
	query := recorder.queries[0]
	for _, want := range []string{
		"WHERE language = ANY($1)",
		"GROUP BY repo_id, language",
		"ORDER BY total_file_count DESC, repo_name, repo_id",
		"LIMIT $2 OFFSET $3",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query = %q, want %q", query, want)
		}
	}
}

func TestContentReaderCountRepositoriesByLanguageUsesDistinctRepoScope(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 5, 23, 14, 15, 0, 0, time.UTC)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"repository_count", "file_count", "last_indexed_at"},
			rows: [][]driver.Value{
				{int64(135), int64(10749), indexedAt},
			},
		},
	})

	reader := NewContentReader(db)
	aggregate, err := reader.CountRepositoriesByLanguage(context.Background(), []string{"typescript", "tsx"})
	if err != nil {
		t.Fatalf("CountRepositoriesByLanguage() error = %v, want nil", err)
	}
	if got, want := aggregate.RepositoryCount, 135; got != want {
		t.Fatalf("RepositoryCount = %d, want %d", got, want)
	}
	if got, want := aggregate.FileCount, 10749; got != want {
		t.Fatalf("FileCount = %d, want %d", got, want)
	}
	if !aggregate.LastIndexedAt.Equal(indexedAt) {
		t.Fatalf("LastIndexedAt = %s, want %s", aggregate.LastIndexedAt, indexedAt)
	}
}

func TestContentReaderRepositoryLanguageInventoryReturnsAggregateRows(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"language", "repository_count", "file_count", "last_indexed_at"},
			rows: [][]driver.Value{
				{"typescript", int64(135), int64(7140), time.Date(2026, 5, 23, 14, 0, 0, 0, time.UTC)},
				{"go", int64(3), int64(30), time.Date(2026, 5, 23, 13, 0, 0, 0, time.UTC)},
			},
		},
	})

	reader := NewContentReader(db)
	rows, err := reader.RepositoryLanguageInventory(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("RepositoryLanguageInventory() error = %v, want nil", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0].Language, "typescript"; got != want {
		t.Fatalf("rows[0].Language = %q, want %q", got, want)
	}
	if got, want := rows[0].RepositoryCount, 135; got != want {
		t.Fatalf("rows[0].RepositoryCount = %d, want %d", got, want)
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("len(recorder.queries) = %d, want %d", got, want)
	}
	if !strings.Contains(recorder.queries[0], "GROUP BY coalesce(NULLIF(language, ''), 'unknown')") {
		t.Fatalf("query = %q, want normalized language grouping", recorder.queries[0])
	}
}

func TestCodeHandlerSearchEntityContentIncludesMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"entity-1", "repo-1", "src/app.py", "Function", "handler",
					int64(1), int64(5), "python", "async def handler(): ...", []byte(`{"decorators":["route"],"async":true}`),
				},
			},
		},
	})

	handler := &CodeHandler{Content: NewContentReader(db)}
	results, err := handler.searchEntityContent(context.Background(), "repo-1", "handler", "", 10)
	if err != nil {
		t.Fatalf("searchEntityContent() error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	metadata, ok := results[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any", results[0]["metadata"])
	}
	if got, want := metadata["async"], true; got != want {
		t.Fatalf("metadata[async] = %#v, want %#v", got, want)
	}
}

func TestCodeHandlerSearchEntityContentIncludesEntityNameMatches(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"component-1", "repo-1", "src/Button.tsx", "Component", "Button",
					int64(1), int64(10), "tsx", "export default memo(() => null)", []byte(`{"framework":"react"}`),
				},
			},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{},
		},
	})

	handler := &CodeHandler{Content: NewContentReader(db)}
	results, err := handler.searchEntityContent(context.Background(), "repo-1", "Button", "typescript", 10)
	if err != nil {
		t.Fatalf("searchEntityContent() error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if got, want := results[0]["entity_name"], "Button"; got != want {
		t.Fatalf("results[0][entity_name] = %#v, want %#v", got, want)
	}
	if got, want := results[0]["language"], "tsx"; got != want {
		t.Fatalf("results[0][language] = %#v, want %#v", got, want)
	}
	if got, want := results[0]["semantic_summary"], "Component Button is associated with the react framework."; got != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", got, want)
	}
	semanticProfile, ok := results[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", results[0]["semantic_profile"])
	}
	if got, want := semanticProfile["surface_kind"], "framework_component"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := semanticProfile["framework"], "react"; got != want {
		t.Fatalf("semantic_profile[framework] = %#v, want %#v", got, want)
	}
}

func TestContentReaderSearchEntitiesReferencingComponent(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"function-1", "repo-1", "src/App.tsx", "Function", "renderApp",
					int64(5), int64(20), "tsx", "return <Button />", []byte(`{"jsx_component_usage":["Button","Panel"]}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	results, err := reader.SearchEntitiesReferencingComponent(context.Background(), "repo-1", "Button", 10)
	if err != nil {
		t.Fatalf("SearchEntitiesReferencingComponent() error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if got, want := results[0].EntityName, "renderApp"; got != want {
		t.Fatalf("results[0].EntityName = %#v, want %#v", got, want)
	}
	usage, ok := results[0].Metadata["jsx_component_usage"].([]any)
	if !ok {
		t.Fatalf("Metadata[jsx_component_usage] type = %T, want []any", results[0].Metadata["jsx_component_usage"])
	}
	if len(usage) != 2 || usage[0] != "Button" || usage[1] != "Panel" {
		t.Fatalf("Metadata[jsx_component_usage] = %#v, want [Button Panel]", usage)
	}
}
