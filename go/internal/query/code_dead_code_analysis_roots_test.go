package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
	modeledEntrypoints, ok := analysis["modeled_entrypoints"].([]any)
	if !ok {
		t.Fatalf("analysis[modeled_entrypoints] type = %T, want []any", analysis["modeled_entrypoints"])
	}
	for _, want := range []string{"scala.main_method", "scala.app_object"} {
		if !queryTestStringSliceContains(modeledEntrypoints, want) {
			t.Fatalf("analysis[modeled_entrypoints] missing %q in %#v", want, modeledEntrypoints)
		}
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
		"c.main_function",
		"c.public_header_api",
		"c.signal_handler",
		"c.callback_argument_target",
		"c.function_pointer_target",
		"csharp.main_method",
		"csharp.constructor",
		"csharp.override_method",
		"csharp.interface_method",
		"csharp.interface_implementation_method",
		"csharp.aspnet_controller_action",
		"csharp.hosted_service_entrypoint",
		"csharp.test_method",
		"csharp.serialization_callback",
		"cpp.main_function",
		"cpp.public_header_api",
		"cpp.virtual_method",
		"cpp.override_method",
		"cpp.callback_argument_target",
		"cpp.function_pointer_target",
		"cpp.node_addon_entrypoint",
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
		"kotlin.main_function",
		"kotlin.constructor",
		"kotlin.interface_type",
		"kotlin.interface_method",
		"kotlin.interface_implementation_method",
		"kotlin.override_method",
		"kotlin.gradle_plugin_apply",
		"kotlin.gradle_task_action",
		"kotlin.gradle_task_property",
		"kotlin.gradle_task_setter",
		"kotlin.spring_component_class",
		"kotlin.spring_configuration_properties_class",
		"kotlin.spring_request_mapping_method",
		"kotlin.spring_bean_method",
		"kotlin.spring_event_listener_method",
		"kotlin.spring_scheduled_method",
		"kotlin.lifecycle_callback_method",
		"kotlin.junit_test_method",
		"kotlin.junit_lifecycle_method",
		"scala.main_method",
		"scala.app_object",
		"scala.trait_type",
		"scala.trait_method",
		"scala.trait_implementation_method",
		"scala.override_method",
		"scala.play_controller_action",
		"scala.akka_actor_receive",
		"scala.lifecycle_callback_method",
		"scala.junit_test_method",
		"scala.scalatest_suite_class",
		"elixir.application_start",
		"elixir.public_macro",
		"elixir.public_guard",
		"elixir.behaviour_callback",
		"elixir.genserver_callback",
		"elixir.supervisor_callback",
		"elixir.mix_task_run",
		"elixir.protocol_function",
		"elixir.protocol_implementation_function",
		"elixir.phoenix_controller_action",
		"elixir.phoenix_liveview_callback",
		"dart.main_function",
		"dart.constructor",
		"dart.override_method",
		"dart.flutter_widget_build",
		"dart.flutter_create_state",
		"dart.public_library_api",
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
		"rust.trait_impl_method",
		"rust.benchmark_function",
		"ruby.rails_controller_action",
		"ruby.rails_callback_method",
		"ruby.dynamic_dispatch_hook",
		"ruby.method_reference_target",
		"ruby.script_entrypoint",
		"groovy.jenkins_pipeline_entrypoint",
		"groovy.shared_library_call",
		"haskell.main_function",
		"haskell.module_export",
		"haskell.exported_type",
		"haskell.typeclass_method",
		"haskell.instance_method",
		"php.script_entrypoint",
		"php.constructor",
		"php.magic_method",
		"php.interface_method",
		"php.interface_implementation_method",
		"php.trait_method",
		"php.framework_controller_action",
		"php.route_handler",
		"php.symfony_route_attribute",
		"php.wordpress_hook_callback",
		"swift.main_function",
		"swift.main_type",
		"swift.swiftui_app_type",
		"swift.swiftui_body",
		"swift.protocol_type",
		"swift.protocol_method",
		"swift.protocol_implementation_method",
		"swift.constructor",
		"swift.override_method",
		"swift.ui_application_delegate_type",
		"swift.ui_application_delegate_method",
		"swift.vapor_route_handler",
		"swift.xctest_method",
		"swift.swift_testing_method",
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
	if !queryTestStringSliceContains(modeledPublicAPI, "c.public_header_api") {
		t.Fatalf("analysis[modeled_public_api] missing c.public_header_api in %#v", modeledPublicAPI)
	}
	if !queryTestStringSliceContains(modeledPublicAPI, "cpp.public_header_api") {
		t.Fatalf("analysis[modeled_public_api] missing cpp.public_header_api in %#v", modeledPublicAPI)
	}
	if !queryTestStringSliceContains(modeledPublicAPI, "dart.public_library_api") {
		t.Fatalf("analysis[modeled_public_api] missing dart.public_library_api in %#v", modeledPublicAPI)
	}
	if !queryTestStringSliceContains(modeledPublicAPI, "haskell.module_export") {
		t.Fatalf("analysis[modeled_public_api] missing haskell.module_export in %#v", modeledPublicAPI)
	}
	if !queryTestStringSliceContains(modeledPublicAPI, "haskell.exported_type") {
		t.Fatalf("analysis[modeled_public_api] missing haskell.exported_type in %#v", modeledPublicAPI)
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
	notes, ok := analysis["notes"].([]any)
	if !ok {
		t.Fatalf("analysis[notes] type = %T, want []any", analysis["notes"])
	}
	for _, want := range []string{"c.public_header_api", "c.callback_argument_target", "cpp.public_header_api", "Ruby Rails controller", "Scala main/App object"} {
		if !queryTestNotesContain(notes, want) {
			t.Fatalf("analysis[notes] missing %q in %#v", want, notes)
		}
	}
}

func queryTestNotesContain(notes []any, want string) bool {
	for _, note := range notes {
		if got, ok := note.(string); ok && strings.Contains(got, want) {
			return true
		}
	}
	return false
}
