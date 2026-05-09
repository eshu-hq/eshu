package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesPythonPublicAPIRootsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "python-public-class", "name": "PublicService", "labels": []any{"Class"},
						"file_path": "library/models.py", "repo_id": "repo-1", "repo_name": "library", "language": "python",
					},
					{
						"entity_id": "python-module-export", "name": "module_factory", "labels": []any{"Function"},
						"file_path": "library/models.py", "repo_id": "repo-1", "repo_name": "library", "language": "python",
					},
					{
						"entity_id": "python-public-method", "name": "run", "labels": []any{"Function"},
						"file_path": "library/models.py", "repo_id": "repo-1", "repo_name": "library", "language": "python",
					},
					{
						"entity_id": "python-public-base", "name": "BaseService", "labels": []any{"Class"},
						"file_path": "library/models.py", "repo_id": "repo-1", "repo_name": "library", "language": "python",
					},
					{
						"entity_id": "python-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "library/models.py", "repo_id": "repo-1", "repo_name": "library", "language": "python",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"python-public-class": {
					EntityID:     "python-public-class",
					RelativePath: "library/models.py",
					EntityType:   "Class",
					EntityName:   "PublicService",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.package_init_export"},
					},
				},
				"python-module-export": {
					EntityID:     "python-module-export",
					RelativePath: "library/models.py",
					EntityType:   "Function",
					EntityName:   "module_factory",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.module_all_export"},
					},
				},
				"python-public-method": {
					EntityID:     "python-public-method",
					RelativePath: "library/models.py",
					EntityType:   "Function",
					EntityName:   "run",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.public_api_member"},
					},
				},
				"python-public-base": {
					EntityID:     "python-public-base",
					RelativePath: "library/models.py",
					EntityType:   "Class",
					EntityName:   "BaseService",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.public_api_base"},
					},
				},
				"python-helper": {
					EntityID:     "python-helper",
					RelativePath: "library/models.py",
					EntityType:   "Function",
					EntityName:   "helper",
					Language:     "python",
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
	results, ok := resp["results"].([]any)
	if !ok {
		t.Fatalf("results type = %T, want []any", resp["results"])
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	only, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("results[0] type = %T, want map[string]any", results[0])
	}
	if got, want := only["entity_id"], "python-helper"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesPythonDunderRootsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "python-dunder", "name": "__str__", "labels": []any{"Function"},
						"file_path": "models.py", "repo_id": "repo-1", "repo_name": "models", "language": "python",
					},
					{
						"entity_id": "python-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "models.py", "repo_id": "repo-1", "repo_name": "models", "language": "python",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"python-dunder": {
					EntityID:     "python-dunder",
					RelativePath: "models.py",
					EntityType:   "Function",
					EntityName:   "__str__",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.dunder_method"},
					},
				},
				"python-helper": {
					EntityID:     "python-helper",
					RelativePath: "models.py",
					EntityType:   "Function",
					EntityName:   "helper",
					Language:     "python",
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
	results, ok := resp["results"].([]any)
	if !ok {
		t.Fatalf("results type = %T, want []any", resp["results"])
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	only, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("results[0] type = %T, want map[string]any", results[0])
	}
	if got, want := only["entity_id"], "python-helper"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
}
