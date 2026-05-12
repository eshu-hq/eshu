package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesSwiftRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	rootRows := []map[string]any{
		{"entity_id": "swift-main", "name": "main", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"swift.main_function"}},
		{"entity_id": "swift-main-type", "name": "DemoApp", "labels": []any{"Struct"}, "dead_code_root_kinds": []any{"swift.main_type"}},
		{"entity_id": "swift-swiftui-app", "name": "DemoApp", "labels": []any{"Struct"}, "dead_code_root_kinds": []any{"swift.swiftui_app_type"}},
		{"entity_id": "swift-protocol", "name": "Runnable", "labels": []any{"Interface"}, "dead_code_root_kinds": []any{"swift.protocol_type"}},
		{"entity_id": "swift-protocol-method", "name": "run", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"swift.protocol_method"}},
		{"entity_id": "swift-protocol-impl", "name": "run", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"swift.protocol_implementation_method"}},
		{"entity_id": "swift-constructor", "name": "init", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"swift.constructor"}},
		{"entity_id": "swift-override", "name": "start", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"swift.override_method"}},
		{"entity_id": "swift-app-delegate", "name": "AppDelegate", "labels": []any{"Class"}, "dead_code_root_kinds": []any{"swift.ui_application_delegate_type"}},
		{"entity_id": "swift-app-delegate-method", "name": "application", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"swift.ui_application_delegate_method"}},
		{"entity_id": "swift-vapor-route", "name": "health", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"swift.vapor_route_handler"}},
		{"entity_id": "swift-xctest", "name": "testRunsFromXCTest", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"swift.xctest_method"}},
		{"entity_id": "swift-testing", "name": "swiftTestingRunsFromRunner", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"swift.swift_testing_method"}},
	}
	rows := make([]map[string]any, 0, len(rootRows)+1)
	for _, row := range rootRows {
		row["file_path"] = "Sources/App/App.swift"
		row["repo_id"] = "repo-1"
		row["repo_name"] = "swift-app"
		row["language"] = "swift"
		rows = append(rows, row)
	}
	rows = append(rows, map[string]any{
		"entity_id": "swift-unused", "name": "unusedCleanupCandidate", "labels": []any{"Function"},
		"file_path": "Sources/App/App.swift", "repo_id": "repo-1", "repo_name": "swift-app", "language": "swift",
	})

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return rows, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"swift","limit":10}`),
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
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
	result := results[0].(map[string]any)
	if got, want := result["entity_id"], "swift-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["classification"], "unused"; got != want {
		t.Fatalf("result[classification] = %#v, want %#v", got, want)
	}

	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(len(rootRows)); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	roots := analysis["modeled_framework_roots"].([]any)
	for _, rootKind := range []string{
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
	} {
		if !queryTestStringSliceContains(roots, rootKind) {
			t.Fatalf("analysis[modeled_framework_roots] missing %q in %#v", rootKind, roots)
		}
	}
	maturity := analysis["dead_code_language_maturity"].(map[string]any)
	if got, want := maturity["swift"], "derived"; got != want {
		t.Fatalf("dead_code_language_maturity[swift] = %#v, want %#v", got, want)
	}
}
