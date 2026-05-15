package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleDeadCodeInvestigationReturnsBucketsCoverageAndPaging(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	content := &contentCandidateDeadCodeStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			fakePortContentStore: fakePortContentStore{
				coverage: RepositoryContentCoverage{
					Available:       true,
					FileCount:       12,
					EntityCount:     34,
					FileIndexedAt:   indexedAt,
					EntityIndexedAt: indexedAt,
					Languages: []RepositoryLanguageCount{
						{Language: "go", FileCount: 8},
						{Language: "python", FileCount: 4},
					},
				},
				repositories: []RepositoryCatalogEntry{{ID: "repo-1", Name: "payments"}},
			},
			entities: map[string]EntityContent{
				"go-helper": {
					EntityID:     "go-helper",
					RepoID:       "repo-1",
					RelativePath: "internal/payments/helper.go",
					EntityType:   "Function",
					EntityName:   "helper",
					StartLine:    10,
					EndLine:      12,
					Language:     "go",
					SourceCache:  "func helper() {}",
				},
				"go-dynamic": {
					EntityID:     "go-dynamic",
					RepoID:       "repo-1",
					RelativePath: "internal/payments/dynamic.go",
					EntityType:   "Function",
					EntityName:   "dynamic",
					StartLine:    20,
					EndLine:      25,
					Language:     "go",
					SourceCache:  "func dynamic() {}",
					Metadata:     map[string]any{"exactness_blockers": []string{"reflection_unresolved"}},
				},
				"py-route": {
					EntityID:     "py-route",
					RepoID:       "repo-1",
					RelativePath: "api/routes.py",
					EntityType:   "Function",
					EntityName:   "handler",
					StartLine:    30,
					EndLine:      35,
					Language:     "python",
					SourceCache:  "@app.route('/pay')\ndef handler(): pass",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"python.flask_route_decorator"}},
				},
			},
		},
		rows: []map[string]any{
			deadCodeInvestigationRow("go-helper", "helper", "go", "internal/payments/helper.go", 10, 12),
			deadCodeInvestigationRow("go-dynamic", "dynamic", "go", "internal/payments/dynamic.go", 20, 25),
			deadCodeInvestigationRow("py-route", "handler", "python", "api/routes.py", 30, 35),
		},
	}
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j:   fakeGraphReader{},
		Content: content,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code/investigate",
		bytes.NewBufferString(`{"repo_id":"payments","limit":10,"offset":0}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	data := decodeEnvelopeData(t, w.Body.Bytes())
	if got, want := data["repo_id"], "repo-1"; got != want {
		t.Fatalf("repo_id = %#v, want %#v", got, want)
	}
	coverage := requireDeadCodeInvestigationMap(t, data, "coverage")
	if got, want := coverage["query_shape"], "bounded_dead_code_investigation"; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
	if got, want := coverage["content_last_indexed_at"], indexedAt.Format(time.RFC3339Nano); got != want {
		t.Fatalf("coverage.content_last_indexed_at = %#v, want %#v", got, want)
	}
	if got, want := coverage["truncated"], false; got != want {
		t.Fatalf("coverage.truncated = %#v, want %#v", got, want)
	}

	buckets := requireDeadCodeInvestigationMap(t, data, "candidate_buckets")
	cleanupReady := requireDeadCodeInvestigationSlice(t, buckets, "cleanup_ready")
	if got, want := len(cleanupReady), 1; got != want {
		t.Fatalf("len(cleanup_ready) = %d, want %d", got, want)
	}
	ambiguous := requireDeadCodeInvestigationSlice(t, buckets, "ambiguous")
	if got, want := len(ambiguous), 1; got != want {
		t.Fatalf("len(ambiguous) = %d, want %d", got, want)
	}
	suppressed := requireDeadCodeInvestigationSlice(t, buckets, "suppressed")
	if got, want := len(suppressed), 1; got != want {
		t.Fatalf("len(suppressed) = %d, want %d", got, want)
	}
	firstCleanup := cleanupReady[0].(map[string]any)
	sourceHandle := requireDeadCodeInvestigationMapValue(t, firstCleanup, "source_handle")
	if got, want := sourceHandle["relative_path"], "internal/payments/helper.go"; got != want {
		t.Fatalf("source_handle.relative_path = %#v, want %#v", got, want)
	}
	nextCalls := requireDeadCodeInvestigationSlice(t, data, "recommended_next_calls")
	if len(nextCalls) == 0 {
		t.Fatal("recommended_next_calls is empty, want source drill-down handle")
	}
}

func TestHandleDeadCodeInvestigationKeepsTypeScriptCandidatesAmbiguous(t *testing.T) {
	t.Parallel()

	content := &contentCandidateDeadCodeStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			fakePortContentStore: fakePortContentStore{
				coverage:     RepositoryContentCoverage{Available: true},
				repositories: []RepositoryCatalogEntry{{ID: "repo-1", Name: "web"}},
			},
			entities: map[string]EntityContent{
				"ts-helper": {
					EntityID:     "ts-helper",
					RepoID:       "repo-1",
					RelativePath: "src/routes/account.ts",
					EntityType:   "Function",
					EntityName:   "loadAccount",
					StartLine:    8,
					EndLine:      16,
					Language:     "typescript",
					SourceCache:  "export async function loadAccount() {}",
				},
			},
		},
		rows: []map[string]any{
			deadCodeInvestigationRow("ts-helper", "loadAccount", "typescript", "src/routes/account.ts", 8, 16),
		},
	}
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j:   fakeGraphReader{},
		Content: content,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code/investigate",
		bytes.NewBufferString(`{"repo_id":"web","language":"typescript","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	data := decodeEnvelopeData(t, w.Body.Bytes())
	buckets := requireDeadCodeInvestigationMap(t, data, "candidate_buckets")
	if cleanupReady := requireDeadCodeInvestigationSlice(t, buckets, "cleanup_ready"); len(cleanupReady) != 0 {
		t.Fatalf("cleanup_ready = %#v, want empty for TypeScript precision risk", cleanupReady)
	}
	ambiguous := requireDeadCodeInvestigationSlice(t, buckets, "ambiguous")
	if got, want := len(ambiguous), 1; got != want {
		t.Fatalf("len(ambiguous) = %d, want %d", got, want)
	}
	candidate := ambiguous[0].(map[string]any)
	if got, want := candidate["classification"], "ambiguous"; got != want {
		t.Fatalf("classification = %#v, want %#v", got, want)
	}
	reasons := requireDeadCodeInvestigationSlice(t, candidate, "ambiguity_reasons")
	if len(reasons) == 0 || reasons[0] != "typescript_dead_code_precision_unvalidated" {
		t.Fatalf("ambiguity_reasons = %#v, want typescript precision blocker", reasons)
	}
}

func TestDeadCodeInvestigationNextCallsMatchCandidateType(t *testing.T) {
	t.Parallel()

	scan := deadCodeInvestigationScan{
		CleanupReady: []map[string]any{
			{
				"entity_id": "go-helper",
				"name":      "helper",
				"labels":    []string{"Function"},
				"language":  "go",
			},
			{
				"entity_id": "go-service",
				"name":      "Service",
				"labels":    []string{"Class"},
				"language":  "go",
			},
			{
				"entity_id": "sql-fn",
				"name":      "refresh_order_totals",
				"labels":    []string{"SqlFunction"},
				"language":  "sql",
			},
		},
	}

	calls := deadCodeInvestigationNextCalls(scan)
	if !hasDeadCodeInvestigationRelationshipCall(calls, "go-helper", "CALLS") {
		t.Fatalf("next calls missing CALLS story for function: %#v", calls)
	}
	if !hasDeadCodeInvestigationRelationshipCall(calls, "go-helper", "REFERENCES") {
		t.Fatalf("next calls missing REFERENCES story for function: %#v", calls)
	}
	if !hasDeadCodeInvestigationRelationshipCall(calls, "go-service", "INHERITS") {
		t.Fatalf("next calls missing INHERITS story for class: %#v", calls)
	}
	if hasDeadCodeInvestigationRelationshipCall(calls, "sql-fn", "CALLS") {
		t.Fatalf("next calls should not suggest CALLS story for SQL function: %#v", calls)
	}
	if !hasDeadCodeInvestigationToolCall(calls, "execute_cypher_query", "sql-fn") {
		t.Fatalf("next calls missing bounded EXECUTES diagnostic for SQL function: %#v", calls)
	}
}

func deadCodeInvestigationRow(entityID, name, language, path string, startLine, endLine int) map[string]any {
	return map[string]any{
		"entity_id":  entityID,
		"name":       name,
		"labels":     []any{"Function"},
		"file_path":  path,
		"repo_id":    "repo-1",
		"repo_name":  "payments",
		"language":   language,
		"start_line": int64(startLine),
		"end_line":   int64(endLine),
	}
}

func decodeEnvelopeData(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", resp["data"])
	}
	return data
}

func requireDeadCodeInvestigationMap(t *testing.T, source map[string]any, key string) map[string]any {
	t.Helper()

	value, ok := source[key].(map[string]any)
	if !ok {
		t.Fatalf("%s type = %T, want map[string]any", key, source[key])
	}
	return value
}

func requireDeadCodeInvestigationMapValue(t *testing.T, source map[string]any, key string) map[string]any {
	t.Helper()

	value, ok := source[key].(map[string]any)
	if !ok {
		t.Fatalf("%s type = %T, want map[string]any", key, source[key])
	}
	return value
}

func requireDeadCodeInvestigationSlice(t *testing.T, source map[string]any, key string) []any {
	t.Helper()

	value, ok := source[key].([]any)
	if !ok {
		t.Fatalf("%s type = %T, want []any", key, source[key])
	}
	return value
}

func hasDeadCodeInvestigationRelationshipCall(calls []map[string]any, entityID string, relationshipType string) bool {
	for _, call := range calls {
		if StringVal(call, "tool") != "get_code_relationship_story" {
			continue
		}
		args, ok := call["arguments"].(map[string]any)
		if !ok {
			continue
		}
		if StringVal(args, "entity_id") == entityID && StringVal(args, "relationship_type") == relationshipType {
			return true
		}
	}
	return false
}

func hasDeadCodeInvestigationToolCall(calls []map[string]any, tool string, entityID string) bool {
	for _, call := range calls {
		if StringVal(call, "tool") != tool {
			continue
		}
		args, ok := call["arguments"].(map[string]any)
		if !ok {
			continue
		}
		if strings.Contains(StringVal(args, "cypher_query"), entityID) || StringVal(args, "entity_id") == entityID {
			return true
		}
	}
	return false
}
