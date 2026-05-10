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
		"python.click_command_decorator",
		"python.typer_command_decorator",
		"python.typer_callback_decorator",
		"python.script_main_guard",
		"python.aws_lambda_handler",
		"python.dataclass_model",
		"python.dataclass_post_init",
		"python.property_decorator",
		"python.module_all_export",
		"python.package_init_export",
		"python.dunder_method",
		"python.public_api_member",
		"python.public_api_base",
		"java.constructor",
		"java.override_method",
		"java.ant_task_setter",
		"java.gradle_plugin_apply",
		"java.gradle_task_action",
		"java.gradle_task_property",
		"java.gradle_task_setter",
		"java.gradle_task_interface_method",
		"java.gradle_dsl_public_method",
		"java.method_reference_target",
		"java.spring_component_class",
		"java.spring_configuration_properties_class",
		"java.spring_request_mapping_method",
		"java.spring_bean_method",
		"java.spring_event_listener_method",
		"java.spring_scheduled_method",
		"java.lifecycle_callback_method",
		"java.junit_test_method",
		"java.junit_lifecycle_method",
		"java.jenkins_extension_class",
		"java.jenkins_symbol_class",
		"java.jenkins_symbol_method",
		"java.jenkins_initializer_method",
		"java.jenkins_databound_setter_method",
		"java.stapler_web_method",
		"java.serialization_hook_method",
		"java.externalizable_hook_method",
		"java.reflection_class_reference",
		"java.reflection_method_reference",
		"java.service_loader_provider",
		"java.spring_autoconfiguration_class",
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
		"rust.main_function",
		"rust.test_function",
		"rust.tokio_main",
		"rust.tokio_test",
		"rust.public_api_item",
		"rust.benchmark_function",
		"typescript.interface_method_implementation",
		"typescript.module_contract_export",
		"typescript.static_registry_member",
	}
	if !reflect.DeepEqual(modeledFrameworkRoots, wantModeledFrameworkRoots) {
		t.Fatalf("analysis[modeled_framework_roots] = %#v, want %#v", modeledFrameworkRoots, wantModeledFrameworkRoots)
	}
	modeledPublicAPI, ok := analysis["modeled_public_api"].([]any)
	if !ok {
		t.Fatalf("analysis[modeled_public_api] type = %T, want []any", analysis["modeled_public_api"])
	}
	if !queryTestStringSliceContains(modeledPublicAPI, "rust.public_api_item") {
		t.Fatalf("analysis[modeled_public_api] missing rust.public_api_item in %#v", modeledPublicAPI)
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
