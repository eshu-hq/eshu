package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeadCodeDogfoodSuppressesGoSemanticRoots(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		deadCodeDogfoodRow("wire-api", "wireAPI", "Function", "go/cmd/api/wiring.go"),
		deadCodeDogfoodRow("execute-cypher", "ExecuteCypher", "Function", "go/cmd/bootstrap-data-plane/main.go"),
		deadCodeDogfoodRow("claim", "Claim", "Function", "go/cmd/bootstrap-index/main.go"),
		deadCodeDogfoodRow("close", "Close", "Function", "go/cmd/bootstrap-index/main.go"),
		deadCodeDogfoodRow("apply-schema", "applySchema", "Function", "go/cmd/bootstrap-index/main.go"),
		deadCodeDogfoodRow("open-bootstrap-db", "openBootstrapDB", "Function", "go/cmd/bootstrap-index/main.go"),
		deadCodeDogfoodRow("open-bootstrap-graph", "openBootstrapGraph", "Function", "go/cmd/bootstrap-index/main.go"),
		deadCodeDogfoodRow("kubernetes-rewrite-metric", "renameMetric", "Function", "cluster/images/etcd-version-monitor/etcd-version-monitor.go"),
		deadCodeDogfoodRow("neo4j-deps", "neo4jDeps", "Struct", "go/cmd/bootstrap-data-plane/main.go"),
		deadCodeDogfoodRow("neo4j-schema-executor", "neo4jSchemaExecutor", "Struct", "go/cmd/bootstrap-data-plane/main.go"),
		deadCodeDogfoodRow("bootstrap-canonical-writer-config", "bootstrapCanonicalWriterConfig", "Struct", "go/cmd/bootstrap-index/canonical_writer_config.go"),
		deadCodeDogfoodRow("bootstrap-committer", "bootstrapCommitter", "Interface", "go/cmd/bootstrap-index/main.go"),
		deadCodeDogfoodRow("collector-deps", "collectorDeps", "Struct", "go/cmd/bootstrap-index/main.go"),
		deadCodeDogfoodRow("draining-work-source", "drainingWorkSource", "Struct", "go/cmd/bootstrap-index/main.go"),
		deadCodeDogfoodRow("truly-dead", "trulyDeadHelper", "Function", "go/internal/query/truly_dead.go"),
	}
	content := map[string]EntityContent{
		"wire-api": {
			EntityID:     "wire-api",
			RelativePath: "go/cmd/api/wiring.go",
			EntityType:   "Function",
			EntityName:   "wireAPI",
			Language:     "go",
			Metadata: map[string]any{
				"dead_code_root_kinds": []string{"go.function_value_reference"},
			},
		},
		"execute-cypher": {
			EntityID:     "execute-cypher",
			RelativePath: "go/cmd/bootstrap-data-plane/main.go",
			EntityType:   "Function",
			EntityName:   "ExecuteCypher",
			Language:     "go",
			Metadata: map[string]any{
				"dead_code_root_kinds": []string{"go.interface_method_implementation"},
			},
		},
		"claim": {
			EntityID:     "claim",
			RelativePath: "go/cmd/bootstrap-index/main.go",
			EntityType:   "Function",
			EntityName:   "Claim",
			Language:     "go",
			Metadata: map[string]any{
				"dead_code_root_kinds": []string{"go.interface_method_implementation"},
			},
		},
		"close": {
			EntityID:     "close",
			RelativePath: "go/cmd/bootstrap-index/main.go",
			EntityType:   "Function",
			EntityName:   "Close",
			Language:     "go",
			Metadata: map[string]any{
				"dead_code_root_kinds": []string{"go.interface_method_implementation"},
			},
		},
		"apply-schema": {
			EntityID:     "apply-schema",
			RelativePath: "go/cmd/bootstrap-index/main.go",
			EntityType:   "Function",
			EntityName:   "applySchema",
			Language:     "go",
			Metadata: map[string]any{
				"dead_code_root_kinds": []string{"go.dependency_injection_callback"},
			},
		},
		"open-bootstrap-db": {
			EntityID:     "open-bootstrap-db",
			RelativePath: "go/cmd/bootstrap-index/main.go",
			EntityType:   "Function",
			EntityName:   "openBootstrapDB",
			Language:     "go",
			Metadata: map[string]any{
				"dead_code_root_kinds": []string{"go.dependency_injection_callback"},
			},
		},
		"open-bootstrap-graph": {
			EntityID:     "open-bootstrap-graph",
			RelativePath: "go/cmd/bootstrap-index/main.go",
			EntityType:   "Function",
			EntityName:   "openBootstrapGraph",
			Language:     "go",
			Metadata: map[string]any{
				"dead_code_root_kinds": []string{"go.dependency_injection_callback"},
			},
		},
		"kubernetes-rewrite-metric": {
			EntityID:     "kubernetes-rewrite-metric",
			RelativePath: "cluster/images/etcd-version-monitor/etcd-version-monitor.go",
			EntityType:   "Function",
			EntityName:   "renameMetric",
			Language:     "go",
			Metadata: map[string]any{
				"dead_code_root_kinds": []string{"go.function_literal_reachable_call"},
			},
		},
		"truly-dead": {
			EntityID:     "truly-dead",
			RelativePath: "go/internal/query/truly_dead.go",
			EntityType:   "Function",
			EntityName:   "trulyDeadHelper",
			Language:     "go",
			SourceCache:  "func trulyDeadHelper() {}",
		},
		"neo4j-deps": deadCodeDogfoodContent("neo4j-deps", "go/cmd/bootstrap-data-plane/main.go", "Struct", "neo4jDeps", "go.type_reference"),
		"neo4j-schema-executor": deadCodeDogfoodContent(
			"neo4j-schema-executor",
			"go/cmd/bootstrap-data-plane/main.go",
			"Struct",
			"neo4jSchemaExecutor",
			"go.interface_implementation_type",
		),
		"bootstrap-canonical-writer-config": deadCodeDogfoodContent(
			"bootstrap-canonical-writer-config",
			"go/cmd/bootstrap-index/canonical_writer_config.go",
			"Struct",
			"bootstrapCanonicalWriterConfig",
			"go.type_reference",
		),
		"bootstrap-committer": deadCodeDogfoodContent(
			"bootstrap-committer",
			"go/cmd/bootstrap-index/main.go",
			"Interface",
			"bootstrapCommitter",
			"go.interface_type_reference",
		),
		"collector-deps": deadCodeDogfoodContent("collector-deps", "go/cmd/bootstrap-index/main.go", "Struct", "collectorDeps", "go.type_reference"),
		"draining-work-source": deadCodeDogfoodContent(
			"draining-work-source",
			"go/cmd/bootstrap-index/main.go",
			"Struct",
			"drainingWorkSource",
			"go.interface_implementation_type",
		),
	}

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repository:r_0ed63f4d"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return rows, nil
			},
		},
		Content: fakeDeadCodeContentStore{entities: content},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repository:r_0ed63f4d"}`),
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
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d body=%s", got, want, w.Body.String())
	}
	result := results[0].(map[string]any)
	if got, want := result["entity_id"], "truly-dead"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}

	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["go_semantic_roots_from_parser_metadata"], float64(14); got != want {
		t.Fatalf("analysis[go_semantic_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	if !deadCodeDogfoodAnalysisListContains(analysis, "modeled_go_semantic_roots", "go.function_literal_reachable_call") {
		t.Fatalf("analysis[modeled_go_semantic_roots] missing go.function_literal_reachable_call: %#v", analysis["modeled_go_semantic_roots"])
	}
}

func TestDeadCodeDogfoodUsesGraphSemanticRootMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id":            "wire-api",
						"name":                 "wireAPI",
						"labels":               []any{"Function"},
						"file_path":            "go/cmd/api/wiring.go",
						"repo_id":              "repository:r_0ed63f4d",
						"repo_name":            "eshu",
						"language":             "go",
						"dead_code_root_kinds": []any{"go.function_value_reference"},
					},
					{
						"entity_id": "truly-dead", "name": "trulyDeadHelper", "labels": []any{"Function"},
						"file_path": "go/internal/query/truly_dead.go", "repo_id": "repository:r_0ed63f4d", "repo_name": "eshu", "language": "go",
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repository:r_0ed63f4d"}`),
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
		t.Fatalf("len(results) = %d, want %d body=%s", got, want, w.Body.String())
	}
	result := results[0].(map[string]any)
	if got, want := result["entity_id"], "truly-dead"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["go_semantic_roots_from_parser_metadata"], float64(1); got != want {
		t.Fatalf("analysis[go_semantic_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
}

func deadCodeDogfoodRow(entityID string, name string, label string, filePath string) map[string]any {
	return map[string]any{
		"entity_id":  entityID,
		"name":       name,
		"labels":     []any{label},
		"file_path":  filePath,
		"repo_id":    "repository:r_0ed63f4d",
		"repo_name":  "eshu",
		"language":   "go",
		"start_line": int64(1),
		"end_line":   int64(1),
	}
}

func deadCodeDogfoodAnalysisListContains(analysis map[string]any, key string, want string) bool {
	values, ok := analysis[key].([]any)
	if !ok {
		return false
	}
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func deadCodeDogfoodContent(
	entityID string,
	relativePath string,
	entityType string,
	entityName string,
	rootKind string,
) EntityContent {
	return EntityContent{
		EntityID:     entityID,
		RelativePath: relativePath,
		EntityType:   entityType,
		EntityName:   entityName,
		Language:     "go",
		Metadata: map[string]any{
			"dead_code_root_kinds": []string{rootKind},
		},
	}
}
