// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

type fakeDeadCodeContentStore struct {
	fakePortContentStore
	entities          map[string]EntityContent
	incomingEntityIDs map[string]bool
}

func (f fakeDeadCodeContentStore) GetEntityContent(_ context.Context, entityID string) (*EntityContent, error) {
	entity, ok := f.entities[entityID]
	if !ok {
		return nil, nil
	}
	cloned := entity
	return &cloned, nil
}

func (f fakeDeadCodeContentStore) DeadCodeIncomingEntityIDs(_ context.Context, _ string, entityIDs []string) (map[string]deadCodeIncomingEdge, error) {
	incoming := make(map[string]deadCodeIncomingEdge)
	for _, entityID := range entityIDs {
		if f.incomingEntityIDs[entityID] {
			incoming[entityID] = deadCodeIncomingEdge{
				MaxConfidence: codeprovenance.LegacyConfidence,
				Method:        codeprovenance.MethodUnspecified,
			}
		}
	}
	return incoming, nil
}

func TestHandleDeadCodeReturnsDerivedTruthAndAnalysisMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{{
					"entity_id": "function-1", "name": "helper", "labels": []any{"Function"},
					"file_path": "internal/payments/helper.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					"start_line": int64(10), "end_line": int64(20),
				}}, nil
			},
		},
		Content: fakeDeadCodeContentStore{entities: map[string]EntityContent{
			"function-1": {
				EntityID: "function-1", RepoID: "repo-1", RelativePath: "internal/payments/helper.go",
				EntityType: "Function", EntityName: "helper", StartLine: 10, EndLine: 20,
				Language: "go", SourceCache: "func helper() {}",
			},
		}},
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
	if got, want := truth["basis"], string(TruthBasisHybrid); got != want {
		t.Fatalf("truth[basis] = %#v, want %#v", got, want)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", resp["data"])
	}
	analysis, ok := data["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", data["analysis"])
	}
	if got, want := analysis["tests_excluded"], true; got != want {
		t.Fatalf("analysis[tests_excluded] = %#v, want %#v", got, want)
	}
	if got, want := analysis["generated_code_excluded"], true; got != want {
		t.Fatalf("analysis[generated_code_excluded] = %#v, want %#v", got, want)
	}
	if got, want := analysis["reflection_modeled"], false; got != want {
		t.Fatalf("analysis[reflection_modeled] = %#v, want %#v", got, want)
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
	if got, want := analysis["user_overrides_applied"], false; got != want {
		t.Fatalf("analysis[user_overrides_applied] = %#v, want %#v", got, want)
	}
	if got, want := analysis["iac_reachability_mode"], "not_modeled_by_code_dead_code"; got != want {
		t.Fatalf("analysis[iac_reachability_mode] = %#v, want %#v", got, want)
	}
	if got, want := analysis["iac_deadness_capability"], "iac_usage.reachability"; got != want {
		t.Fatalf("analysis[iac_deadness_capability] = %#v, want %#v", got, want)
	}
	rootCategories, ok := analysis["root_categories_used"].([]any)
	if !ok {
		t.Fatalf("analysis[root_categories_used] type = %T, want []any", analysis["root_categories_used"])
	}
	if got, want := len(rootCategories), 6; got != want {
		t.Fatalf("len(analysis[root_categories_used]) = %d, want %d", got, want)
	}
	if got, want := rootCategories[2], "library_public_api"; got != want {
		t.Fatalf("analysis[root_categories_used][2] = %#v, want %#v", got, want)
	}
	if got, want := rootCategories[5], "framework_callback_roots"; got != want {
		t.Fatalf("analysis[root_categories_used][5] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesDefaultEntrypointsTestsAndGeneratedCode(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{"entity_id": "go-main", "name": "main", "labels": []any{"Function"}, "file_path": "cmd/payments/main.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go"},
					{"entity_id": "go-init", "name": "init", "labels": []any{"Function"}, "file_path": "internal/payments/bootstrap.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go"},
					{"entity_id": "go-test", "name": "TestHelper", "labels": []any{"Function"}, "file_path": "internal/payments/helper_test.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go"},
					{"entity_id": "go-generated", "name": "GeneratedClient", "labels": []any{"Function"}, "file_path": "gen/client.pb.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go"},
					{"entity_id": "go-helper", "name": "helper", "labels": []any{"Function"}, "file_path": "internal/payments/helper.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go"},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{entities: map[string]EntityContent{
			"go-main": {EntityID: "go-main", RelativePath: "cmd/payments/main.go", EntityType: "Function", EntityName: "main", Language: "go", SourceCache: "func main() {}"},
			"go-init": {EntityID: "go-init", RelativePath: "internal/payments/bootstrap.go", EntityType: "Function", EntityName: "init", Language: "go", SourceCache: "func init() {}"},
			"go-test": {EntityID: "go-test", RelativePath: "internal/payments/helper_test.go", EntityType: "Function", EntityName: "TestHelper", Language: "go", SourceCache: "func TestHelper(t *testing.T) {}"},
			"go-generated": {
				EntityID: "go-generated", RelativePath: "gen/client.pb.go", EntityType: "Function", EntityName: "GeneratedClient", Language: "go",
				SourceCache: "// Code generated by protoc-gen-go. DO NOT EDIT.\nfunc GeneratedClient() {}",
			},
			"go-helper": {EntityID: "go-helper", RelativePath: "internal/payments/helper.go", EntityType: "Function", EntityName: "helper", Language: "go", SourceCache: "func helper() {}"},
		}},
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
}
