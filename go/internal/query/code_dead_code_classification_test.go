package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeResultClassificationPreservesDerivedTruth(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "go-helper", "name": "privateHelper", "labels": []any{"Function"},
						"file_path": "internal/payments/helper.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "rust-helper", "name": "private_helper", "labels": []any{"Function"},
						"file_path": "crates/payments/src/lib.rs", "repo_id": "repo-1", "repo_name": "payments", "language": "rust",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"go-helper": {
					EntityID: "go-helper", RelativePath: "internal/payments/helper.go", EntityType: "Function", EntityName: "privateHelper", Language: "go", SourceCache: "func privateHelper() {}",
				},
				"rust-helper": {
					EntityID: "rust-helper", RelativePath: "crates/payments/src/lib.rs", EntityType: "Function", EntityName: "private_helper", Language: "rust", SourceCache: "fn private_helper() {}",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1"}`),
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
	truth := resp["truth"].(map[string]any)
	if got, want := truth["level"], string(TruthLevelDerived); got != want {
		t.Fatalf("truth[level] = %#v, want %#v", got, want)
	}
	data := resp["data"].(map[string]any)
	results := data["results"].([]any)
	if got, want := len(results), 2; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}

	got := make(map[string]string, len(results))
	for _, raw := range results {
		result := raw.(map[string]any)
		got[result["entity_id"].(string)] = result["classification"].(string)
	}
	if got, want := got["go-helper"], "unused"; got != want {
		t.Fatalf("go-helper classification = %#v, want %#v", got, want)
	}
	if got, want := got["rust-helper"], "derived_candidate_only"; got != want {
		t.Fatalf("rust-helper classification = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeClassificationKeepsDefaultExclusionsSuppressed(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "public-api", "name": "PublicAPI", "labels": []any{"Function"},
						"file_path": "pkg/payments/api.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "private-helper", "name": "privateHelper", "labels": []any{"Function"},
						"file_path": "internal/payments/helper.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"public-api": {
					EntityID: "public-api", RelativePath: "pkg/payments/api.go", EntityType: "Function", EntityName: "PublicAPI", Language: "go", SourceCache: "func PublicAPI() {}",
				},
				"private-helper": {
					EntityID: "private-helper", RelativePath: "internal/payments/helper.go", EntityType: "Function", EntityName: "privateHelper", Language: "go", SourceCache: "func privateHelper() {}",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	results := resp["results"].([]any)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	result := results[0].(map[string]any)
	if got, want := result["entity_id"], "private-helper"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["classification"], "unused"; got != want {
		t.Fatalf("result[classification] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeSuppressesUnsupportedLanguageCandidates(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "package-script", "name": "build", "labels": []any{"Function"},
						"file_path": "package.json", "repo_id": "repo-1", "repo_name": "service", "language": "json",
					},
					{
						"entity_id": "service-handler", "name": "handleRequest", "labels": []any{"Function"},
						"file_path": "src/handler.ts", "repo_id": "repo-1", "repo_name": "service", "language": "typescript",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"package-script": {
					EntityID: "package-script", RelativePath: "package.json", EntityType: "Function", EntityName: "build", Language: "json",
				},
				"service-handler": {
					EntityID: "service-handler", RelativePath: "src/handler.ts", EntityType: "Function", EntityName: "handleRequest", Language: "typescript",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	results := resp["results"].([]any)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	result := results[0].(map[string]any)
	if got, want := result["entity_id"], "service-handler"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["classification"], "unused"; got != want {
		t.Fatalf("result[classification] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeSuppressesRepositoryRootTestDirectories(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "test-helper", "name": "initTestServer", "labels": []any{"Function"},
						"file_path": "test/helpers/server.ts", "repo_id": "repo-1", "repo_name": "service", "language": "typescript",
					},
					{
						"entity_id": "service-handler", "name": "handleRequest", "labels": []any{"Function"},
						"file_path": "src/handler.ts", "repo_id": "repo-1", "repo_name": "service", "language": "typescript",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"test-helper": {
					EntityID: "test-helper", RelativePath: "test/helpers/server.ts", EntityType: "Function", EntityName: "initTestServer", Language: "typescript",
				},
				"service-handler": {
					EntityID: "service-handler", RelativePath: "src/handler.ts", EntityType: "Function", EntityName: "handleRequest", Language: "typescript",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	results := resp["results"].([]any)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	result := results[0].(map[string]any)
	if got, want := result["entity_id"], "service-handler"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
}
