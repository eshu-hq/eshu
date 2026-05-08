package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesPythonFrameworkRootsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "python-fastapi", "name": "fastapi_health", "labels": []any{"Function"},
						"file_path": "app/api.py", "repo_id": "repo-1", "repo_name": "payments", "language": "python",
						"decorators": []any{"@app.get(\"/health\")"},
					},
					{
						"entity_id": "python-flask", "name": "flask_status", "labels": []any{"Function"},
						"file_path": "app/flask_app.py", "repo_id": "repo-1", "repo_name": "payments", "language": "python",
						"decorators": []any{"@app.route(\"/status\")"},
					},
					{
						"entity_id": "python-celery", "name": "sync_payments", "labels": []any{"Function"},
						"file_path": "workers/tasks.py", "repo_id": "repo-1", "repo_name": "payments", "language": "python",
						"decorators": []any{"@shared_task"},
					},
					{
						"entity_id": "python-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "app/helpers.py", "repo_id": "repo-1", "repo_name": "payments", "language": "python",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"python-fastapi": {
					EntityID:     "python-fastapi",
					RelativePath: "app/api.py",
					EntityType:   "Function",
					EntityName:   "fastapi_health",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.fastapi_route_decorator"},
						"decorators":           []any{"@app.get(\"/health\")"},
					},
				},
				"python-flask": {
					EntityID:     "python-flask",
					RelativePath: "app/flask_app.py",
					EntityType:   "Function",
					EntityName:   "flask_status",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.flask_route_decorator"},
						"decorators":           []any{"@app.route(\"/status\")"},
					},
				},
				"python-celery": {
					EntityID:     "python-celery",
					RelativePath: "workers/tasks.py",
					EntityType:   "Function",
					EntityName:   "sync_payments",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.celery_task_decorator"},
						"decorators":           []any{"@shared_task"},
					},
				},
				"python-helper": {
					EntityID:     "python-helper",
					RelativePath: "app/helpers.py",
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

	analysis, ok := resp["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", resp["analysis"])
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(3); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesPythonCLIRootsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "python-click", "name": "sync", "labels": []any{"Function"},
						"file_path": "app/cli.py", "repo_id": "repo-1", "repo_name": "payments", "language": "python",
					},
					{
						"entity_id": "python-typer", "name": "serve", "labels": []any{"Function"},
						"file_path": "app/cli.py", "repo_id": "repo-1", "repo_name": "payments", "language": "python",
					},
					{
						"entity_id": "python-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "app/helpers.py", "repo_id": "repo-1", "repo_name": "payments", "language": "python",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"python-click": {
					EntityID:     "python-click",
					RelativePath: "app/cli.py",
					EntityType:   "Function",
					EntityName:   "sync",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.click_command_decorator"},
					},
				},
				"python-typer": {
					EntityID:     "python-typer",
					RelativePath: "app/cli.py",
					EntityType:   "Function",
					EntityName:   "serve",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.typer_command_decorator"},
					},
				},
				"python-helper": {
					EntityID:     "python-helper",
					RelativePath: "app/helpers.py",
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

	analysis, ok := resp["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", resp["analysis"])
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(2); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	maturity, ok := analysis["dead_code_language_maturity"].(map[string]any)
	if !ok {
		t.Fatalf("analysis[dead_code_language_maturity] type = %T, want map[string]any", analysis["dead_code_language_maturity"])
	}
	if got, want := maturity["python"], "derived"; got != want {
		t.Fatalf("dead_code_language_maturity[python] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesPythonScriptMainAndAnonymousLambdaRoots(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "python-main", "name": "main", "labels": []any{"Function"},
						"file_path": "script.py", "repo_id": "repo-1", "repo_name": "tools", "language": "python",
					},
					{
						"entity_id": "python-lambda", "name": "lambda@12_8", "labels": []any{"Function"},
						"file_path": "script.py", "repo_id": "repo-1", "repo_name": "tools", "language": "python",
						"semantic_kind": "lambda",
					},
					{
						"entity_id": "python-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "script.py", "repo_id": "repo-1", "repo_name": "tools", "language": "python",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"python-main": {
					EntityID:     "python-main",
					RelativePath: "script.py",
					EntityType:   "Function",
					EntityName:   "main",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.script_main_guard"},
					},
				},
				"python-lambda": {
					EntityID:     "python-lambda",
					RelativePath: "script.py",
					EntityType:   "Function",
					EntityName:   "lambda@12_8",
					Language:     "python",
					Metadata: map[string]any{
						"semantic_kind": "lambda",
					},
				},
				"python-helper": {
					EntityID:     "python-helper",
					RelativePath: "script.py",
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
	only := results[0].(map[string]any)
	if got, want := only["entity_id"], "python-helper"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesPythonLambdaHandlerRootFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "python-lambda", "name": "lambda_handler", "labels": []any{"Function"},
						"file_path": "app.py", "repo_id": "repo-1", "repo_name": "cloudfront-logs", "language": "python",
					},
					{
						"entity_id": "python-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "app.py", "repo_id": "repo-1", "repo_name": "cloudfront-logs", "language": "python",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"python-lambda": {
					EntityID:     "python-lambda",
					RelativePath: "app.py",
					EntityType:   "Function",
					EntityName:   "lambda_handler",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.aws_lambda_handler"},
					},
				},
				"python-helper": {
					EntityID:     "python-helper",
					RelativePath: "app.py",
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

	analysis, ok := resp["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", resp["analysis"])
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(1); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesPythonDataclassAndPropertyRootsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "python-dataclass", "name": "S3Event", "labels": []any{"Class"},
						"file_path": "models.py", "repo_id": "repo-1", "repo_name": "events", "language": "python",
					},
					{
						"entity_id": "python-post-init", "name": "__post_init__", "labels": []any{"Function"},
						"file_path": "models.py", "repo_id": "repo-1", "repo_name": "events", "language": "python",
					},
					{
						"entity_id": "python-property", "name": "object_url", "labels": []any{"Function"},
						"file_path": "models.py", "repo_id": "repo-1", "repo_name": "events", "language": "python",
					},
					{
						"entity_id": "python-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "models.py", "repo_id": "repo-1", "repo_name": "events", "language": "python",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"python-dataclass": {
					EntityID:     "python-dataclass",
					RelativePath: "models.py",
					EntityType:   "Class",
					EntityName:   "S3Event",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.dataclass_model"},
					},
				},
				"python-post-init": {
					EntityID:     "python-post-init",
					RelativePath: "models.py",
					EntityType:   "Function",
					EntityName:   "__post_init__",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.dataclass_post_init"},
					},
				},
				"python-property": {
					EntityID:     "python-property",
					RelativePath: "models.py",
					EntityType:   "Function",
					EntityName:   "object_url",
					Language:     "python",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"python.property_decorator"},
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
