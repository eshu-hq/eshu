package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleDeadCodeReportsSQLFunctionsAsDerivedCandidates(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "e:SqlFunction") {
					return nil, nil
				}
				return []map[string]any{
					{
						"entity_id": "sql-refresh", "name": "public.refresh_users", "labels": []any{"SqlFunction"},
						"file_path": "db/functions.sql", "repo_id": "repo-1", "repo_name": "warehouse", "language": "sql",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"sql-refresh": {
					EntityID:     "sql-refresh",
					RepoID:       "repo-1",
					RelativePath: "db/functions.sql",
					EntityType:   "SqlFunction",
					EntityName:   "public.refresh_users",
					Language:     "sql",
					SourceCache:  "CREATE FUNCTION public.refresh_users() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp["data"].(map[string]any)
	results := data["results"].([]any)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	result := results[0].(map[string]any)
	if got, want := result["entity_id"], "sql-refresh"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["classification"], "unused"; got != want {
		t.Fatalf("result[classification] = %#v, want %#v", got, want)
	}

	analysis := data["analysis"].(map[string]any)
	maturity := analysis["dead_code_language_maturity"].(map[string]any)
	if got, want := maturity["sql"], "derived"; got != want {
		t.Fatalf("maturity[sql] = %#v, want %#v", got, want)
	}
	blockers := analysis["dead_code_language_exactness_blockers"].(map[string]any)
	sqlBlockers := blockers["sql"].([]any)
	for _, want := range []string{
		"dynamic_sql_unresolved",
		"dialect_specific_routine_resolution_unavailable",
		"migration_order_resolution_unavailable",
	} {
		if !queryTestStringSliceContains(sqlBlockers, want) {
			t.Fatalf("blockers[sql] missing %q in %#v", want, sqlBlockers)
		}
	}
}

func TestHandleDeadCodeSuppressesSQLFunctionsReachedByGraphExecutesEdge(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "e:SqlFunction") {
					return nil, nil
				}
				return []map[string]any{
					{
						"entity_id": "sql-refresh", "name": "public.refresh_users", "labels": []any{"SqlFunction"},
						"file_path": "db/functions.sql", "repo_id": "repo-1", "repo_name": "warehouse", "language": "sql",
					},
				}, nil
			},
			runIncoming: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "EXECUTES") {
					t.Fatalf("incoming cypher missing EXECUTES:\n%s", cypher)
				}
				if got, want := params["entity_ids"], []string{"sql-refresh"}; !equalStringSlices(got.([]string), want) {
					t.Fatalf("params[entity_ids] = %#v, want %#v", got, want)
				}
				return []map[string]any{{"incoming_entity_id": "sql-refresh"}}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"sql-refresh": {
					EntityID:     "sql-refresh",
					RepoID:       "repo-1",
					RelativePath: "db/functions.sql",
					EntityType:   "SqlFunction",
					EntityName:   "public.refresh_users",
					Language:     "sql",
					SourceCache:  "CREATE FUNCTION public.refresh_users() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp["data"].(map[string]any)
	results := data["results"].([]any)
	if got, want := len(results), 0; got != want {
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
}

func TestHandleDeadCodeLanguageFilterScansSQLFunctionsWithoutFunctionStarvation(t *testing.T) {
	t.Parallel()

	var queriedLabels []string
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "e:Function"):
					queriedLabels = append(queriedLabels, "Function")
					return []map[string]any{
						{
							"entity_id": "ts-helper", "name": "helper", "labels": []any{"Function"},
							"file_path": "src/helper.ts", "repo_id": "repo-1", "repo_name": "warehouse", "language": "typescript",
						},
					}, nil
				case strings.Contains(cypher, "e:SqlFunction"):
					queriedLabels = append(queriedLabels, "SqlFunction")
					return []map[string]any{
						{
							"entity_id": "sql-refresh", "name": "public.refresh_users", "labels": []any{"SqlFunction"},
							"file_path": "db/functions.sql", "repo_id": "repo-1", "repo_name": "warehouse", "language": "sql",
						},
					}, nil
				default:
					return nil, nil
				}
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"sql-refresh": {
					EntityID:     "sql-refresh",
					RepoID:       "repo-1",
					RelativePath: "db/functions.sql",
					EntityType:   "SqlFunction",
					EntityName:   "public.refresh_users",
					Language:     "sql",
					SourceCache:  "CREATE FUNCTION public.refresh_users() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"sql","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	if got, want := queriedLabels, []string{"SqlFunction"}; !equalStringSlices(got, want) {
		t.Fatalf("queried labels = %#v, want %#v", got, want)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp["data"].(map[string]any)
	if got, want := data["language"], "sql"; got != want {
		t.Fatalf("data[language] = %#v, want %#v", got, want)
	}
	results := data["results"].([]any)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
	result := results[0].(map[string]any)
	if got, want := result["entity_id"], "sql-refresh"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeSuppressesContentSQLFunctionsReachedByGraphExecutesEdge(t *testing.T) {
	t.Parallel()

	content := &sqlCandidateDeadCodeContentStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"sql-refresh": {
					EntityID:     "sql-refresh",
					RepoID:       "repo-1",
					RelativePath: "db/functions.sql",
					EntityType:   "SqlFunction",
					EntityName:   "public.refresh_users",
					Language:     "sql",
					SourceCache:  "CREATE FUNCTION public.refresh_users() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;",
				},
				"sql-archive": {
					EntityID:     "sql-archive",
					RepoID:       "repo-1",
					RelativePath: "db/functions.sql",
					EntityType:   "SqlFunction",
					EntityName:   "public.archive_users",
					Language:     "sql",
					SourceCache:  "CREATE FUNCTION public.archive_users() RETURNS void AS $$ BEGIN END; $$ LANGUAGE plpgsql;",
				},
			},
		},
		rows: []map[string]any{
			{
				"entity_id": "sql-refresh", "name": "public.refresh_users", "labels": []any{"SqlFunction"},
				"file_path": "db/functions.sql", "repo_id": "repo-1", "repo_name": "warehouse", "language": "sql",
			},
			{
				"entity_id": "sql-archive", "name": "public.archive_users", "labels": []any{"SqlFunction"},
				"file_path": "db/functions.sql", "repo_id": "repo-1", "repo_name": "warehouse", "language": "sql",
			},
		},
	}
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				t.Fatalf("dead-code SQL scan should use content candidates before graph scan: cypher=%s params=%#v", cypher, params)
				return nil, nil
			},
			runIncoming: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "EXECUTES") {
					t.Fatalf("incoming cypher missing EXECUTES:\n%s", cypher)
				}
				if stringSliceContains(params["entity_ids"].([]string), "sql-refresh") {
					return []map[string]any{{"incoming_entity_id": "sql-refresh"}}, nil
				}
				return nil, nil
			},
		},
		Content: content,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"sql","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := content.repoID, "repo-1"; got != want {
		t.Fatalf("content repo id = %q, want %q", got, want)
	}
	if got, want := content.label, "SqlFunction"; got != want {
		t.Fatalf("content label = %q, want %q", got, want)
	}
	if got, want := content.language, "sql"; got != want {
		t.Fatalf("content language = %q, want %q", got, want)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp["data"].(map[string]any)
	results := data["results"].([]any)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
	result := results[0].(map[string]any)
	if got, want := result["entity_id"], "sql-archive"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := data["candidate_scan_rows"], float64(2); got != want {
		t.Fatalf("data[candidate_scan_rows] = %#v, want %#v", got, want)
	}
}

type sqlCandidateDeadCodeContentStore struct {
	fakeDeadCodeContentStore
	rows     []map[string]any
	repoID   string
	label    string
	language string
}

func (f *sqlCandidateDeadCodeContentStore) DeadCodeCandidateRows(
	_ context.Context,
	repoID string,
	label string,
	language string,
	limit int,
	offset int,
) ([]map[string]any, error) {
	f.repoID = repoID
	f.label = label
	f.language = language
	if label != "SqlFunction" {
		return nil, nil
	}
	filtered := make([]map[string]any, 0, len(f.rows))
	for _, row := range f.rows {
		if language == "" || strings.EqualFold(StringVal(row, "language"), language) {
			filtered = append(filtered, row)
		}
	}
	if offset >= len(filtered) {
		return nil, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], nil
}
