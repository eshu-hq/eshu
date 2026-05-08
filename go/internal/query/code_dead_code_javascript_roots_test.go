package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesJavaScriptFrameworkRootsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "js-next", "name": "GET", "labels": []any{"Function"},
						"file_path": "app/api/health/route.ts", "repo_id": "repo-1", "repo_name": "payments", "language": "typescript",
					},
					{
						"entity_id": "js-express", "name": "login", "labels": []any{"Function"},
						"file_path": "server/routes.ts", "repo_id": "repo-1", "repo_name": "payments", "language": "typescript",
					},
					{
						"entity_id": "js-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "server/helpers.ts", "repo_id": "repo-1", "repo_name": "payments", "language": "typescript",
					},
					{
						"entity_id": "js-nested-helper", "name": "findAndUpdate", "labels": []any{"Function"},
						"file_path": "scripts/create-new-version.js", "repo_id": "repo-1", "repo_name": "payments", "language": "javascript",
						"metadata": map[string]any{"enclosing_function": "updateSpecs"},
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"js-next": {
					EntityID:     "js-next",
					RelativePath: "app/api/health/route.ts",
					EntityType:   "Function",
					EntityName:   "GET",
					Language:     "typescript",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"javascript.nextjs_route_export"},
					},
				},
				"js-express": {
					EntityID:     "js-express",
					RelativePath: "server/routes.ts",
					EntityType:   "Function",
					EntityName:   "login",
					Language:     "typescript",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"javascript.express_route_registration"},
					},
				},
				"js-helper": {
					EntityID:     "js-helper",
					RelativePath: "server/helpers.ts",
					EntityType:   "Function",
					EntityName:   "helper",
					Language:     "typescript",
				},
				"js-nested-helper": {
					EntityID:     "js-nested-helper",
					RelativePath: "scripts/create-new-version.js",
					EntityType:   "Function",
					EntityName:   "findAndUpdate",
					Language:     "javascript",
					Metadata: map[string]any{
						"enclosing_function": "updateSpecs",
					},
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
	if got, want := only["entity_id"], "js-helper"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}

	analysis, ok := resp["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", resp["analysis"])
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(2); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesJavaScriptNodeAndHapiRootsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "js-entry", "name": "bootstrap", "labels": []any{"Function"},
						"file_path": "service-sample.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-bin", "name": "runCli", "labels": []any{"Function"},
						"file_path": "cli.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-export", "name": "publicApi", "labels": []any{"Function"},
						"file_path": "server/public-api.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-export-class", "name": "PublicClient", "labels": []any{"Class"},
						"file_path": "server/public-api.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-hapi", "name": "post", "labels": []any{"Function"},
						"file_path": "server/handlers/chat/response.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-hapi-route-config", "name": "handler", "labels": []any{"Function"},
						"file_path": "server/controllers/orders.js", "repo_id": "repo-1", "repo_name": "service-sample", "language": "javascript",
					},
					{
						"entity_id": "js-commonjs-default", "name": "registerRoutes", "labels": []any{"Function"},
						"file_path": "server/init/routes.js", "repo_id": "repo-1", "repo_name": "service-sample", "language": "javascript",
					},
					{
						"entity_id": "js-commonjs-class", "name": "ExportedError", "labels": []any{"Class"},
						"file_path": "server/errors/exported-error.js", "repo_id": "repo-1", "repo_name": "service-sample", "language": "javascript",
					},
					{
						"entity_id": "js-seed", "name": "execute", "labels": []any{"Function"},
						"file_path": "seed/20260508_add_records.js", "repo_id": "repo-1", "repo_name": "service-sample", "language": "javascript",
					},
					{
						"entity_id": "js-consumer", "name": "consume", "labels": []any{"Function"},
						"file_path": "server/resources/consumers/order-updated.js", "repo_id": "repo-1", "repo_name": "service-sample", "language": "javascript",
					},
					{
						"entity_id": "js-mixin", "name": "getFromListing", "labels": []any{"Function"},
						"file_path": "server/resources/order-wrapper-mixin.js", "repo_id": "repo-1", "repo_name": "service-sample", "language": "javascript",
					},
					{
						"entity_id": "js-helper", "name": "unusedHelper", "labels": []any{"Function"},
						"file_path": "server/private-helper.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-next-page", "name": "Home", "labels": []any{"Function"},
						"file_path": "admin-next/app/page.tsx", "repo_id": "repo-1", "repo_name": "service-sample", "language": "tsx",
					},
					{
						"entity_id": "js-migration-up", "name": "up", "labels": []any{"Function"},
						"file_path": "migrations/20260508120000_create-records.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "ts-module-contract", "name": "validate", "labels": []any{"Function"},
						"file_path": "server/resources/rules/spam-email-regex.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "ts-impl", "name": "createResponse", "labels": []any{"Function"},
						"file_path": "server/providers/GeminiAdapter.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-express-middleware", "name": "requireAuth", "labels": []any{"Function"},
						"file_path": "server/routes.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-koa-middleware", "name": "auditRequest", "labels": []any{"Function"},
						"file_path": "server/koa-routes.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-koa-route", "name": "koaHandler", "labels": []any{"Function"},
						"file_path": "server/koa-routes.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-fastify-hook", "name": "authHook", "labels": []any{"Function"},
						"file_path": "server/fastify.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-fastify-route", "name": "healthHandler", "labels": []any{"Function"},
						"file_path": "server/fastify.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-fastify-plugin", "name": "pluginHandler", "labels": []any{"Function"},
						"file_path": "server/fastify.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-nest-controller", "name": "listUsers", "labels": []any{"Function"},
						"file_path": "server/users.controller.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"js-entry": {
					EntityID: "js-entry", RelativePath: "service-sample.ts", EntityType: "Function", EntityName: "bootstrap", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.node_package_entrypoint"}},
				},
				"js-bin": {
					EntityID: "js-bin", RelativePath: "cli.ts", EntityType: "Function", EntityName: "runCli", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.node_package_bin"}},
				},
				"js-export": {
					EntityID: "js-export", RelativePath: "server/public-api.ts", EntityType: "Function", EntityName: "publicApi", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.node_package_export"}},
				},
				"js-export-class": {
					EntityID: "js-export-class", RelativePath: "server/public-api.ts", EntityType: "Class", EntityName: "PublicClient", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.node_package_export"}},
				},
				"js-hapi": {
					EntityID: "js-hapi", RelativePath: "server/handlers/chat/response.ts", EntityType: "Function", EntityName: "post", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.hapi_handler_export"}},
				},
				"js-hapi-route-config": {
					EntityID: "js-hapi-route-config", RelativePath: "server/controllers/orders.js", EntityType: "Function", EntityName: "handler", Language: "javascript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.hapi_route_config_handler"}},
				},
				"js-commonjs-default": {
					EntityID: "js-commonjs-default", RelativePath: "server/init/routes.js", EntityType: "Function", EntityName: "registerRoutes", Language: "javascript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.commonjs_default_export"}},
				},
				"js-commonjs-class": {
					EntityID: "js-commonjs-class", RelativePath: "server/errors/exported-error.js", EntityType: "Class", EntityName: "ExportedError", Language: "javascript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.commonjs_default_export"}},
				},
				"js-seed": {
					EntityID: "js-seed", RelativePath: "seed/20260508_add_records.js", EntityType: "Function", EntityName: "execute", Language: "javascript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.node_seed_execute"}},
				},
				"js-consumer": {
					EntityID: "js-consumer", RelativePath: "server/resources/consumers/order-updated.js", EntityType: "Function", EntityName: "consume", Language: "javascript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.hapi_amqp_consumer"}},
				},
				"js-mixin": {
					EntityID: "js-mixin", RelativePath: "server/resources/order-wrapper-mixin.js", EntityType: "Function", EntityName: "getFromListing", Language: "javascript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.commonjs_mixin_export"}},
				},
				"js-helper": {
					EntityID: "js-helper", RelativePath: "server/private-helper.ts", EntityType: "Function", EntityName: "unusedHelper", Language: "typescript",
				},
				"js-next-page": {
					EntityID: "js-next-page", RelativePath: "admin-next/app/page.tsx", EntityType: "Function", EntityName: "Home", Language: "tsx",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.nextjs_app_export"}},
				},
				"js-migration-up": {
					EntityID: "js-migration-up", RelativePath: "migrations/20260508120000_create-records.ts", EntityType: "Function", EntityName: "up", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.node_migration_export"}},
				},
				"ts-module-contract": {
					EntityID: "ts-module-contract", RelativePath: "server/resources/rules/spam-email-regex.ts", EntityType: "Function", EntityName: "validate", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"typescript.module_contract_export"}},
				},
				"ts-impl": {
					EntityID: "ts-impl", RelativePath: "server/providers/GeminiAdapter.ts", EntityType: "Function", EntityName: "createResponse", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"typescript.interface_method_implementation"}},
				},
				"js-express-middleware": {
					EntityID: "js-express-middleware", RelativePath: "server/routes.ts", EntityType: "Function", EntityName: "requireAuth", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.express_middleware_registration"}},
				},
				"js-koa-middleware": {
					EntityID: "js-koa-middleware", RelativePath: "server/koa-routes.ts", EntityType: "Function", EntityName: "auditRequest", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.koa_middleware_registration"}},
				},
				"js-koa-route": {
					EntityID: "js-koa-route", RelativePath: "server/koa-routes.ts", EntityType: "Function", EntityName: "koaHandler", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.koa_route_registration"}},
				},
				"js-fastify-hook": {
					EntityID: "js-fastify-hook", RelativePath: "server/fastify.ts", EntityType: "Function", EntityName: "authHook", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.fastify_hook_registration"}},
				},
				"js-fastify-route": {
					EntityID: "js-fastify-route", RelativePath: "server/fastify.ts", EntityType: "Function", EntityName: "healthHandler", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.fastify_route_registration"}},
				},
				"js-fastify-plugin": {
					EntityID: "js-fastify-plugin", RelativePath: "server/fastify.ts", EntityType: "Function", EntityName: "pluginHandler", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.fastify_plugin_registration"}},
				},
				"js-nest-controller": {
					EntityID: "js-nest-controller", RelativePath: "server/users.controller.ts", EntityType: "Function", EntityName: "listUsers", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.nestjs_controller_method"}},
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
	if got, want := only["entity_id"], "js-helper"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}

	analysis, ok := resp["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", resp["analysis"])
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(22); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeJavaScriptRootsRemainDerivedMaturity(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "js-route", "name": "listUsers", "labels": []any{"Function"},
						"file_path": "server/routes.js", "repo_id": "repo-1", "repo_name": "payments", "language": "javascript",
					},
					{
						"entity_id": "js-unused", "name": "unusedLocalHelper", "labels": []any{"Function"},
						"file_path": "src/app.js", "repo_id": "repo-1", "repo_name": "payments", "language": "javascript",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"js-route": {
					EntityID:     "js-route",
					RelativePath: "server/routes.js",
					EntityType:   "Function",
					EntityName:   "listUsers",
					Language:     "javascript",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"javascript.express_route_registration"},
					},
				},
				"js-unused": {
					EntityID:     "js-unused",
					RelativePath: "src/app.js",
					EntityType:   "Function",
					EntityName:   "unusedLocalHelper",
					Language:     "javascript",
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
	truth, ok := resp["truth"].(map[string]any)
	if !ok {
		t.Fatalf("truth type = %T, want map[string]any", resp["truth"])
	}
	if got, want := truth["level"], string(TruthLevelDerived); got != want {
		t.Fatalf("truth[level] = %#v, want %#v", got, want)
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
	analysis, ok := data["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", data["analysis"])
	}
	maturity, ok := analysis["dead_code_language_maturity"].(map[string]any)
	if !ok {
		t.Fatalf("analysis[dead_code_language_maturity] type = %T, want map[string]any", analysis["dead_code_language_maturity"])
	}
	if got, want := maturity["javascript"], "derived"; got != want {
		t.Fatalf("dead_code_language_maturity[javascript] = %#v, want %#v", got, want)
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(1); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
}
