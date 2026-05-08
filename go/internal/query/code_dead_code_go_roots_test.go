package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestHandleDeadCodeExcludesGoFrameworkRootsBySignature(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"entity_id": "go-http-handler", "name": "ServePayments", "labels": []any{"Function"},
						"file_path": "internal/http/payments.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "go-cobra-run", "name": "runPayments", "labels": []any{"Function"},
						"file_path": "cmd/payments/root.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "go-reconcile", "name": "Reconcile", "labels": []any{"Function"},
						"file_path": "internal/controllers/payment_controller.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "go-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "internal/payments/helper.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"go-http-handler": {
					EntityID:     "go-http-handler",
					RelativePath: "internal/http/payments.go",
					EntityType:   "Function",
					EntityName:   "ServePayments",
					Language:     "go",
					SourceCache:  "func ServePayments(w http.ResponseWriter, r *http.Request) {}",
				},
				"go-cobra-run": {
					EntityID:     "go-cobra-run",
					RelativePath: "cmd/payments/root.go",
					EntityType:   "Function",
					EntityName:   "runPayments",
					Language:     "go",
					SourceCache:  "func runPayments(cmd *cobra.Command, args []string) error { return nil }",
				},
				"go-reconcile": {
					EntityID:     "go-reconcile",
					RelativePath: "internal/controllers/payment_controller.go",
					EntityType:   "Function",
					EntityName:   "Reconcile",
					Language:     "go",
					SourceCache:  "func (r *PaymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) { return ctrl.Result{}, nil }",
				},
				"go-helper": {
					EntityID:     "go-helper",
					RelativePath: "internal/payments/helper.go",
					EntityType:   "Function",
					EntityName:   "helper",
					Language:     "go",
					SourceCache:  "func helper() {}",
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
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "go-helper"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	analysis, ok := resp["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", resp["analysis"])
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(0); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	if got, want := analysis["framework_roots_from_source_fallback"], float64(3); got != want {
		t.Fatalf("analysis[framework_roots_from_source_fallback] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeReportsModeledGoFrameworkRootsInAnalysis(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"entity_id":  "go-helper",
						"name":       "helper",
						"labels":     []any{"Function"},
						"file_path":  "internal/payments/helper.go",
						"repo_id":    "repo-1",
						"repo_name":  "payments",
						"language":   "go",
						"start_line": int64(10),
						"end_line":   int64(20),
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"go-helper": {
					EntityID:     "go-helper",
					RepoID:       "repo-1",
					RelativePath: "internal/payments/helper.go",
					EntityType:   "Function",
					EntityName:   "helper",
					StartLine:    10,
					EndLine:      20,
					Language:     "go",
					SourceCache:  "func helper() {}",
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
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", resp["data"])
	}
	analysis, ok := data["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", data["analysis"])
	}
	rootCategories, ok := analysis["root_categories_used"].([]any)
	if !ok {
		t.Fatalf("analysis[root_categories_used] type = %T, want []any", analysis["root_categories_used"])
	}
	if got, want := len(rootCategories), 6; got != want {
		t.Fatalf("len(analysis[root_categories_used]) = %d, want %d", got, want)
	}
	if got, want := rootCategories[3], "cli_command_roots"; got != want {
		t.Fatalf("analysis[root_categories_used][3] = %#v, want %#v", got, want)
	}
	if got, want := rootCategories[4], "http_and_rpc_roots"; got != want {
		t.Fatalf("analysis[root_categories_used][4] = %#v, want %#v", got, want)
	}
	if got, want := rootCategories[5], "framework_callback_roots"; got != want {
		t.Fatalf("analysis[root_categories_used][5] = %#v, want %#v", got, want)
	}

	modeledFrameworkRoots, ok := analysis["modeled_framework_roots"].([]any)
	if !ok {
		t.Fatalf("analysis[modeled_framework_roots] type = %T, want []any", analysis["modeled_framework_roots"])
	}
	wantModeledFrameworkRoots := []any{
		"go.cobra_run_registration",
		"go.cobra_run_signature",
		"go.net_http_handler_registration",
		"go.net_http_handler_signature",
		"go.controller_runtime_reconcile_signature",
		"python.fastapi_route_decorator",
		"python.flask_route_decorator",
		"python.celery_task_decorator",
		"javascript.nextjs_route_export",
		"javascript.nextjs_app_export",
		"javascript.express_route_registration",
		"javascript.express_middleware_registration",
		"javascript.koa_middleware_registration",
		"javascript.koa_route_registration",
		"javascript.fastify_hook_registration",
		"javascript.fastify_route_registration",
		"javascript.fastify_plugin_registration",
		"javascript.nestjs_controller_method",
		"javascript.commonjs_default_export",
		"javascript.commonjs_mixin_export",
		"javascript.node_package_export",
		"javascript.node_seed_execute",
		"javascript.node_migration_export",
		"javascript.hapi_amqp_consumer",
		"javascript.hapi_handler_export",
		"javascript.hapi_plugin_register",
		"javascript.hapi_route_config_handler",
		"javascript.hapi_proxy_callback",
		"typescript.interface_method_implementation",
		"typescript.module_contract_export",
		"typescript.static_registry_member",
	}
	if !reflect.DeepEqual(modeledFrameworkRoots, wantModeledFrameworkRoots) {
		t.Fatalf("analysis[modeled_framework_roots] = %#v, want %#v", modeledFrameworkRoots, wantModeledFrameworkRoots)
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(0); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	if got, want := analysis["framework_roots_from_source_fallback"], float64(0); got != want {
		t.Fatalf("analysis[framework_roots_from_source_fallback] = %#v, want %#v", got, want)
	}
	if got, want := analysis["roots_skipped_missing_source"], float64(0); got != want {
		t.Fatalf("analysis[roots_skipped_missing_source] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeDoesNotTreatGoCommentSubstringsAsFrameworkRoots(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"entity_id": "go-comment-only", "name": "helper", "labels": []any{"Function"},
						"file_path": "internal/http/comments.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"go-comment-only": {
					EntityID:     "go-comment-only",
					RelativePath: "internal/http/comments.go",
					EntityType:   "Function",
					EntityName:   "helper",
					Language:     "go",
					SourceCache:  "func helper() {}\n// func fake(w http.ResponseWriter, r *http.Request) {}\n/* func run(cmd *cobra.Command, args []string) error { return nil } */",
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
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "go-comment-only"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeReportsMissingSourceForGoFrameworkRootChecks(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"entity_id": "go-missing-source", "name": "ServePayments", "labels": []any{"Function"},
						"file_path": "internal/http/payments.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"go-missing-source": {
					EntityID:     "go-missing-source",
					RelativePath: "internal/http/payments.go",
					EntityType:   "Function",
					EntityName:   "ServePayments",
					Language:     "go",
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
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", resp["data"])
	}
	analysis, ok := data["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", data["analysis"])
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(0); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	if got, want := analysis["framework_roots_from_source_fallback"], float64(0); got != want {
		t.Fatalf("analysis[framework_roots_from_source_fallback] = %#v, want %#v", got, want)
	}
	if got, want := analysis["roots_skipped_missing_source"], float64(1); got != want {
		t.Fatalf("analysis[roots_skipped_missing_source] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeUsesParserRootMetadataWithoutSourceCache(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"entity_id": "go-parser-root", "name": "ServePayments", "labels": []any{"Function"},
						"file_path": "internal/http/payments.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "go-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "internal/payments/helper.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"go-parser-root": {
					EntityID:     "go-parser-root",
					RelativePath: "internal/http/payments.go",
					EntityType:   "Function",
					EntityName:   "ServePayments",
					Language:     "go",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"go.net_http_handler_signature"},
					},
				},
				"go-helper": {
					EntityID:     "go-helper",
					RelativePath: "internal/payments/helper.go",
					EntityType:   "Function",
					EntityName:   "helper",
					Language:     "go",
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
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", resp["data"])
	}
	results, ok := data["results"].([]any)
	if !ok {
		t.Fatalf("results type = %T, want []any", data["results"])
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "go-helper"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}

	analysis, ok := data["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", data["analysis"])
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(1); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	if got, want := analysis["framework_roots_from_source_fallback"], float64(0); got != want {
		t.Fatalf("analysis[framework_roots_from_source_fallback] = %#v, want %#v", got, want)
	}
	if got, want := analysis["roots_skipped_missing_source"], float64(1); got != want {
		t.Fatalf("analysis[roots_skipped_missing_source] = %#v, want %#v", got, want)
	}
}
