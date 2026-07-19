// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestSearchEntityNamesPushesFiltersBeforeLimit(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"entity_id", "repo_id", "relative_path", "entity_type", "entity_name", "start_line", "end_line", "language", "source_cache", "metadata", "repo_name"},
		rows: [][]driver.Value{
			{"entity-1", "repo-a", "a.go", "Function", "Server", int64(1), int64(2), "go", "", []byte(`{}`), "Repository A"},
			{"entity-2", "repo-a", "a.go", "Function", "Server", int64(3), int64(4), "go", "", []byte(`{}`), "Repository A"},
		},
		queryContainsInOrder: []string{
			"eshu_require_content_substring_indexes_ready()",
			"entity_name = $1",
			"repo_id = ANY($2::text[])",
			"coalesce(language, '') = ANY($3::text[])",
			"entity_type = $4",
			"ORDER BY repo_id, relative_path, start_line, entity_name, entity_id",
			"LIMIT $5",
		},
	}})
	rows, err := NewContentReader(db).SearchEntityNames(context.Background(), EntityNameSearch{
		Name: "Server", Match: EntityNameMatchExact, Scope: EntityNameScopeRepositories,
		RepositoryIDs: []string{"repo-a", "repo-a"}, Languages: []string{"go"}, EntityType: "Function", Limit: 2,
	})
	if err != nil {
		t.Fatalf("SearchEntityNames() error = %v", err)
	}
	if len(rows) != 2 || rows[0].EntityID != "entity-1" || rows[1].EntityID != "entity-2" {
		t.Fatalf("rows = %#v, want deterministic duplicate-name rows", rows)
	}
}

func TestSearchEntityNamesEscapesLiteralSubstringMetacharacters(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{"entity_id", "repo_id", "relative_path", "entity_type", "entity_name", "start_line", "end_line", "language", "source_cache", "metadata", "repo_name"},
	}})
	_, err := NewContentReader(db).SearchEntityNames(context.Background(), EntityNameSearch{
		Name: `server_100%\δοκιμή`, Match: EntityNameMatchSubstring, Scope: EntityNameScopeAll, Limit: 5,
	})
	if err != nil {
		t.Fatalf("SearchEntityNames() error = %v", err)
	}
	if len(recorder.queries) != 1 || !strings.Contains(recorder.queries[0], `entity_name LIKE '%' || $1 || '%' ESCAPE '\'`) {
		t.Fatalf("query = %q, want explicit literal-substring escape", recorder.queries)
	}
	if got, want := recorder.args[0][0], driver.Value(`server\_100\%\\δοκιμή`); got != want {
		t.Fatalf("substring arg = %#v, want %#v", got, want)
	}
}

func TestSearchEntityNamesAppliesSemanticAndAuthorizationFiltersBeforePageAndCatalogJoin(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{"entity_id", "repo_id", "relative_path", "entity_type", "entity_name", "start_line", "end_line", "language", "source_cache", "metadata", "repo_name"},
	}})
	_, err := NewContentReader(db).SearchEntityNames(context.Background(), EntityNameSearch{
		Name: "is_valid", Match: EntityNameMatchExact, Scope: EntityNameScopeRepositories,
		RepositoryIDs: []string{"repo-a"}, EntityType: "Function",
		MetadataKey: "semantic_kind", MetadataValue: "guard", Limit: 5,
	})
	if err != nil {
		t.Fatalf("SearchEntityNames() error = %v", err)
	}
	if len(recorder.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(recorder.queries))
	}
	if err := contentReaderQueryContainsInOrder(recorder.queries[0], []string{
		"WITH matched AS MATERIALIZED", "entity_name = $1", "repo_id = ANY($2::text[])",
		"entity_type = $3", "coalesce(metadata ->> 'semantic_kind', '') = $4",
		"ORDER BY repo_id, relative_path, start_line, entity_name, entity_id", "LIMIT $5",
		"matched_repository_ids AS MATERIALIZED", "SELECT DISTINCT repo_id", "FROM matched",
		"repository_catalog AS MATERIALIZED", "FROM ingestion_scopes", "JOIN matched_repository_ids",
		"LEFT JOIN repository_catalog",
	}); err != nil {
		t.Fatalf("entity name query shape: %v\nquery=%s", err, recorder.queries[0])
	}
	if strings.Contains(recorder.queries[0], "LATERAL") {
		t.Fatalf("entity name query contains per-row LATERAL catalog scan:\n%s", recorder.queries[0])
	}
}

func TestSearchEntityNamesSubstringAndEmptyGrantContracts(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns:       []string{"entity_id", "repo_id", "relative_path", "entity_type", "entity_name", "start_line", "end_line", "language", "source_cache", "metadata", "repo_name"},
		queryContains: []string{"entity_name LIKE '%' || $1 || '%'", "LIMIT $2"},
	}})
	reader := NewContentReader(db)
	if _, err := reader.SearchEntityNames(context.Background(), EntityNameSearch{
		Name: "Serv", Match: EntityNameMatchSubstring, Scope: EntityNameScopeAll, Limit: 5,
	}); err != nil {
		t.Fatalf("substring SearchEntityNames() error = %v", err)
	}
	if rows, err := reader.SearchEntityNames(context.Background(), EntityNameSearch{
		Name: "Server", Match: EntityNameMatchExact, Scope: EntityNameScopeRepositories, Limit: 5,
	}); err != nil || len(rows) != 0 {
		t.Fatalf("empty-grant SearchEntityNames() = %#v, %v; want empty without query", rows, err)
	}
}

func TestNormalizeEntityNameSearchRejectsAmbiguousOrUnboundedRequests(t *testing.T) {
	t.Parallel()

	valid := EntityNameSearch{Name: "Server", Match: EntityNameMatchExact, Scope: EntityNameScopeAll, Limit: 10}
	for _, tc := range []struct {
		name   string
		mutate func(*EntityNameSearch)
	}{
		{name: "whitespace name", mutate: func(s *EntityNameSearch) { s.Name = " \t " }},
		{name: "invalid match", mutate: func(s *EntityNameSearch) { s.Match = "fuzzy" }},
		{name: "invalid scope", mutate: func(s *EntityNameSearch) { s.Scope = "implicit" }},
		{name: "all scope with repositories", mutate: func(s *EntityNameSearch) { s.RepositoryIDs = []string{"repo-a"} }},
		{name: "zero limit", mutate: func(s *EntityNameSearch) { s.Limit = 0 }},
		{name: "over internal probe limit", mutate: func(s *EntityNameSearch) { s.Limit = entityNameSearchProbeLimit + 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			search := valid
			tc.mutate(&search)
			if _, _, err := normalizeEntityNameSearch(search); err == nil {
				t.Fatalf("normalizeEntityNameSearch(%#v) error = nil", search)
			}
		})
	}
}

func TestNormalizeEntityNameSearchAcceptsOneRowPaginationProbe(t *testing.T) {
	t.Parallel()

	search, empty, err := normalizeEntityNameSearch(EntityNameSearch{
		Name: "Server", Match: EntityNameMatchExact, Scope: EntityNameScopeAll,
		Limit: entityNameSearchProbeLimit,
	})
	if err != nil || empty || search.Limit != entityNameSearchMaxLimit+1 {
		t.Fatalf("normalizeEntityNameSearch() = %#v, empty=%t, err=%v", search, empty, err)
	}
}

func TestNormalizeEntityNameSearchCanonicalizesExplicitScope(t *testing.T) {
	t.Parallel()

	search, empty, err := normalizeEntityNameSearch(EntityNameSearch{
		Name: " Server ", Match: EntityNameMatchSubstring, Scope: EntityNameScopeRepositories,
		RepositoryIDs: []string{"repo-b", "", "repo-a", "repo-b"},
		Languages:     []string{"TSX", "typescript", ""}, Limit: 10,
	})
	if err != nil || empty {
		t.Fatalf("normalizeEntityNameSearch() = %#v, empty=%t, err=%v", search, empty, err)
	}
	if search.Name != "Server" || !slices.Equal(search.RepositoryIDs, []string{"repo-a", "repo-b"}) ||
		!slices.Equal(search.Languages, []string{"tsx", "typescript"}) {
		t.Fatalf("normalized search = %#v", search)
	}

	search, empty, err = normalizeEntityNameSearch(EntityNameSearch{
		Name: "Server", Match: EntityNameMatchExact, Scope: EntityNameScopeRepositories,
		RepositoryIDs: []string{"", "  "}, Limit: 10,
	})
	if err != nil || !empty || len(search.RepositoryIDs) != 0 {
		t.Fatalf("blank repository grant = %#v, empty=%t, err=%v; want empty/no error", search, empty, err)
	}
}

type recordingEntityNameSearcher struct {
	fakePortContentStore
	searches []EntityNameSearch
	rows     []EntityContent
}

func (s *recordingEntityNameSearcher) SearchEntityNames(_ context.Context, search EntityNameSearch) ([]EntityContent, error) {
	s.searches = append(s.searches, search)
	return append([]EntityContent(nil), s.rows...), nil
}

func TestGlobalCodeSearchUsesOneAuthorizedContentNameQuery(t *testing.T) {
	t.Parallel()

	content := &recordingEntityNameSearcher{rows: []EntityContent{{
		EntityID: "entity-b", RepoID: "repo-b", RepoName: "Repository B", RelativePath: "b.go", EntityType: "Function", EntityName: "Server", Language: "go",
	}}}
	graph := &captureGraphQuery{runFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
		t.Fatal("global code search called GraphQuery")
		return nil, nil
	}}
	handler := &CodeHandler{Neo4j: graph, Content: content, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/search", bytes.NewBufferString(`{"query":"Ser","language":"TSX","limit":1}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode: AuthModeScoped, AllowedRepositoryIDs: []string{"repo-b", "repo-a", "repo-b"},
	}))
	rec := httptest.NewRecorder()

	handler.handleSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(content.searches) != 1 {
		t.Fatalf("search calls = %d, want 1", len(content.searches))
	}
	got := content.searches[0]
	if got.Match != EntityNameMatchSubstring || got.Scope != EntityNameScopeRepositories || !slices.Equal(got.RepositoryIDs, []string{"repo-a", "repo-b"}) {
		t.Fatalf("search = %#v, want one sorted scoped substring query", got)
	}
	if !slices.Equal(got.Languages, []string{"typescript", "tsx"}) {
		t.Fatalf("languages = %#v, want [typescript tsx]", got.Languages)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["source"] != "content" || body["source_backend"] != "postgres_content_name_index" {
		t.Fatalf("source truth = %#v/%#v, want content/postgres_content_name_index", body["source"], body["source_backend"])
	}
	results, ok := body["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one legacy-compatible row", body["results"])
	}
	result, _ := results[0].(map[string]any)
	if result["name"] != "Server" || result["entity_name"] != "Server" || result["repo_name"] != "Repository B" {
		t.Fatalf("result identity fields = %#v", result)
	}
	if labels, ok := result["labels"].([]any); !ok || len(labels) != 1 || labels[0] != "Function" {
		t.Fatalf("result labels = %#v, want [Function]", result["labels"])
	}
}

func TestGlobalCodeSearchRequiresBoundedSubstringAndNameStore(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		handler *CodeHandler
		body    string
		status  int
	}{
		{name: "short substring", handler: &CodeHandler{Content: &recordingEntityNameSearcher{}}, body: `{"query":"λx"}`, status: http.StatusBadRequest},
		{name: "missing narrow store", handler: &CodeHandler{Content: fakePortContentStore{}}, body: `{"query":"Server"}`, status: http.StatusServiceUnavailable},
		{name: "exact short allowed", handler: &CodeHandler{Content: &recordingEntityNameSearcher{}}, body: `{"query":"x","exact":true}`, status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/search", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			tc.handler.handleSearch(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestGlobalEntityResolveFailsClosedOrUsesExactContent(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		body      string
		status    int
		wantCalls int
	}{
		{name: "untyped", body: `{"name":"Server"}`, status: http.StatusBadRequest},
		{name: "unknown", body: `{"name":"Server","type":"mystery"}`, status: http.StatusBadRequest},
		{name: "graph only", body: `{"name":"Server","type":"file"}`, status: http.StatusBadRequest},
		{name: "canonical handle", body: `{"name":"content-entity:e1"}`, status: http.StatusOK},
		{name: "typed content", body: `{"name":"Server","type":"function"}`, status: http.StatusOK, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := &recordingEntityNameSearcher{rows: []EntityContent{{
				EntityID: "entity-1", RepoID: "repo-1", RelativePath: "main.go", EntityType: "Function", EntityName: "Server",
			}}}
			graph := &captureGraphQuery{runFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				t.Fatal("global entity resolve called unsafe GraphQuery")
				return nil, nil
			}}
			handler := &EntityHandler{Neo4j: graph, Content: content}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve", bytes.NewBufferString(tc.body))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			handler.resolveEntity(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if len(content.searches) != tc.wantCalls {
				t.Fatalf("search calls = %d, want %d", len(content.searches), tc.wantCalls)
			}
			if tc.wantCalls == 1 && (content.searches[0].Match != EntityNameMatchExact || content.searches[0].EntityType != "Function") {
				t.Fatalf("search = %#v, want exact Function", content.searches[0])
			}
			if tc.wantCalls == 1 {
				var envelope ResponseEnvelope
				if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
					t.Fatalf("decode envelope: %v", err)
				}
				if envelope.Truth == nil || envelope.Truth.Basis != TruthBasisContentIndex {
					t.Fatalf("truth = %#v, want content-index basis", envelope.Truth)
				}
			}
		})
	}
}

func TestGlobalContentEntityNameFilterPreservesSemanticEntityTypes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, entityType, metadataKey, metadataValue string
	}{
		{name: "guard", entityType: "Function", metadataKey: "semantic_kind", metadataValue: "guard"},
		{name: "module_attribute", entityType: "Variable", metadataKey: "attribute_kind", metadataValue: "module_attribute"},
		{name: "protocol_implementation", entityType: "Module", metadataKey: "module_kind", metadataValue: "protocol_implementation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter, ok := globalContentEntityNameFilter(tc.name)
			if !ok || filter.EntityType != tc.entityType || filter.MetadataKey != tc.metadataKey || filter.MetadataValue != tc.metadataValue {
				t.Fatalf("globalContentEntityNameFilter(%q) = %#v, %t", tc.name, filter, ok)
			}
		})
	}
}

func TestCanonicalContentEntityTruthReflectsActualReadPath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		entity    EntityContent
		graph     GraphQuery
		wantBasis TruthBasis
	}{
		{
			name:      "content only",
			entity:    EntityContent{EntityID: "content-entity:function", RepoID: "repo-a", RepoName: "Repository A", EntityType: "Function", EntityName: "Run"},
			wantBasis: TruthBasisContentIndex,
		},
		{
			name:   "workload graph hydration",
			entity: EntityContent{EntityID: "content-entity:workload", EntityType: "Workload", EntityName: "payments"},
			graph: &captureGraphQuery{runFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return []map[string]any{{"entity_id": "content-entity:workload", "repo_id": "repo-a", "repo_name": "Repository A"}}, nil
			}},
			wantBasis: TruthBasisHybrid,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := resolvingEntityContentStore{
				entitiesByID: map[string]EntityContent{tc.entity.EntityID: tc.entity},
				repositories: []RepositoryCatalogEntry{{ID: "repo-a", Name: "Repository A"}},
			}
			handler := &EntityHandler{Neo4j: tc.graph, Content: content, Profile: ProfileLocalAuthoritative}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve", bytes.NewBufferString(`{"name":"`+tc.entity.EntityID+`"}`))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			handler.resolveEntity(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			var envelope ResponseEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if envelope.Truth == nil || envelope.Truth.Basis != tc.wantBasis {
				t.Fatalf("truth = %#v, want basis %q", envelope.Truth, tc.wantBasis)
			}
		})
	}
}
