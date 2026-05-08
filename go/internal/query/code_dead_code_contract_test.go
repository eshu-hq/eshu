package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeDeadCodeContentStore struct {
	fakePortContentStore
	entities map[string]EntityContent
}

func (f fakeDeadCodeContentStore) GetEntityContent(_ context.Context, entityID string) (*EntityContent, error) {
	entity, ok := f.entities[entityID]
	if !ok {
		return nil, nil
	}
	cloned := entity
	return &cloned, nil
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
				return []map[string]any{
					{
						"entity_id":  "function-1",
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
				"function-1": {
					EntityID:     "function-1",
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
					{
						"entity_id": "go-main", "name": "main", "labels": []any{"Function"},
						"file_path": "cmd/payments/main.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "go-init", "name": "init", "labels": []any{"Function"},
						"file_path": "internal/payments/bootstrap.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "go-test", "name": "TestHelper", "labels": []any{"Function"},
						"file_path": "internal/payments/helper_test.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "go-generated", "name": "GeneratedClient", "labels": []any{"Function"},
						"file_path": "gen/client.pb.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
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
				"go-main": {
					EntityID: "go-main", RelativePath: "cmd/payments/main.go", EntityType: "Function", EntityName: "main", Language: "go", SourceCache: "func main() {}",
				},
				"go-init": {
					EntityID: "go-init", RelativePath: "internal/payments/bootstrap.go", EntityType: "Function", EntityName: "init", Language: "go", SourceCache: "func init() {}",
				},
				"go-test": {
					EntityID: "go-test", RelativePath: "internal/payments/helper_test.go", EntityType: "Function", EntityName: "TestHelper", Language: "go", SourceCache: "func TestHelper(t *testing.T) {}",
				},
				"go-generated": {
					EntityID: "go-generated", RelativePath: "gen/client.pb.go", EntityType: "Function", EntityName: "GeneratedClient", Language: "go", SourceCache: "// Code generated by protoc-gen-go. DO NOT EDIT.\nfunc GeneratedClient() {}",
				},
				"go-helper": {
					EntityID: "go-helper", RelativePath: "internal/payments/helper.go", EntityType: "Function", EntityName: "helper", Language: "go", SourceCache: "func helper() {}",
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
}

func TestHandleDeadCodeUsesNornicDBCompatibleCandidateQuery(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		GraphBackend: GraphBackendNornicDB,
		Profile:      ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "WHERE NOT ()-[:CALLS|IMPORTS|REFERENCES]->(e)") {
					t.Fatalf("cypher = %q, want NornicDB dead-code query to avoid inline NOT pattern", cypher)
				}
				if !strings.Contains(cypher, "NOT EXISTS { MATCH (e)<-[:CALLS|IMPORTS|REFERENCES]-() }") {
					t.Fatalf("cypher = %q, want NornicDB dead-code query to use NOT EXISTS pattern form", cypher)
				}
				if got, want := params["limit"], deadCodeCandidateQueryLimit(deadCodeDefaultLimit); got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"entity_id":  "function-1",
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
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestHandleDeadCodeCandidateQueryRestrictsCodeEntityLabelsBeforeLimit(t *testing.T) {
	t.Parallel()

	for _, backend := range []GraphBackend{GraphBackendNeo4j, GraphBackendNornicDB} {
		backend := backend
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()

			cypher := buildDeadCodeGraphCypher(false, backend)
			limitIndex := strings.Index(cypher, "LIMIT $limit")
			if limitIndex < 0 {
				t.Fatalf("cypher = %q, want LIMIT $limit", cypher)
			}

			labelGate := "(e:Function OR e:Class OR e:Struct OR e:Interface)"
			labelGateIndex := strings.Index(cypher, labelGate)
			if labelGateIndex < 0 {
				t.Fatalf("cypher = %q, want code-entity label gate %q", cypher, labelGate)
			}
			if labelGateIndex > limitIndex {
				t.Fatalf("cypher = %q, want code-entity label gate before LIMIT", cypher)
			}
		})
	}
}

func TestHandleDeadCodeCandidateQueryAnchorsRepoScopedReads(t *testing.T) {
	t.Parallel()

	for _, backend := range []GraphBackend{GraphBackendNeo4j, GraphBackendNornicDB} {
		backend := backend
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()

			cypher := buildDeadCodeGraphCypher(true, backend)
			if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e)") {
				t.Fatalf("cypher = %q, want repo-scoped query to anchor on Repository id before entity traversal", cypher)
			}
			if strings.Contains(cypher, "AND r.id = $repo_id") {
				t.Fatalf("cypher = %q, want repo id in MATCH anchor, not late WHERE filter", cypher)
			}
		})
	}
}

func TestHandleDeadCodeExcludesNonCodeEntitiesFromBackendRows(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "argo-app", "name": "eshu", "labels": []any{"ArgoCDApplication"},
						"file_path": "deploy/argocd/app.yaml", "repo_id": "repo-1", "repo_name": "eshu", "language": "yaml",
					},
					{
						"entity_id": "k8s-resource", "name": "api-deployment", "labels": []any{"K8sResource"},
						"file_path": "deploy/k8s/api.yaml", "repo_id": "repo-1", "repo_name": "eshu", "language": "yaml",
					},
					{
						"entity_id": "terraform-module", "name": "service", "labels": []any{"TerraformModule"},
						"file_path": "infra/modules/service/main.tf", "repo_id": "repo-1", "repo_name": "eshu", "language": "hcl",
					},
					{
						"entity_id": "helm-chart", "name": "api", "labels": []any{"HelmChart"},
						"file_path": "charts/api/Chart.yaml", "repo_id": "repo-1", "repo_name": "eshu", "language": "yaml",
					},
					{
						"entity_id": "kustomize-overlay", "name": "prod", "labels": []any{"KustomizeOverlay"},
						"file_path": "deploy/overlays/prod/kustomization.yaml", "repo_id": "repo-1", "repo_name": "eshu", "language": "yaml",
					},
					{
						"entity_id": "function-1", "name": "helper", "labels": []any{"Function"},
						"file_path": "go/internal/query/helper.go", "repo_id": "repo-1", "repo_name": "eshu", "language": "go",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"argo-app":     {EntityID: "argo-app", RelativePath: "deploy/argocd/app.yaml", EntityType: "ArgoCDApplication", EntityName: "eshu", Language: "yaml"},
				"k8s-resource": {EntityID: "k8s-resource", RelativePath: "deploy/k8s/api.yaml", EntityType: "K8sResource", EntityName: "api-deployment", Language: "yaml"},
				"terraform-module": {
					EntityID: "terraform-module", RelativePath: "infra/modules/service/main.tf", EntityType: "TerraformModule", EntityName: "service", Language: "hcl",
				},
				"helm-chart": {
					EntityID: "helm-chart", RelativePath: "charts/api/Chart.yaml", EntityType: "HelmChart", EntityName: "api", Language: "yaml",
				},
				"kustomize-overlay": {
					EntityID: "kustomize-overlay", RelativePath: "deploy/overlays/prod/kustomization.yaml", EntityType: "KustomizeOverlay", EntityName: "prod", Language: "yaml",
				},
				"function-1": {EntityID: "function-1", RelativePath: "go/internal/query/helper.go", EntityType: "Function", EntityName: "helper", Language: "go", SourceCache: "func helper() {}"},
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
	if got, want := result["entity_id"], "function-1"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesGoPublicAPIRootsOutsideInternalPackages(t *testing.T) {
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
						"entity_id": "go-public-api", "name": "ProcessPayment", "labels": []any{"Function"},
						"file_path": "pkg/payments/api.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "go-internal-exported", "name": "ProcessInternalPayment", "labels": []any{"Function"},
						"file_path": "internal/payments/api.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "go-private-helper", "name": "processPrivatePayment", "labels": []any{"Function"},
						"file_path": "pkg/payments/private.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"go-public-api": {
					EntityID: "go-public-api", RelativePath: "pkg/payments/api.go", EntityType: "Function", EntityName: "ProcessPayment", Language: "go", SourceCache: "func ProcessPayment() {}",
				},
				"go-internal-exported": {
					EntityID: "go-internal-exported", RelativePath: "internal/payments/api.go", EntityType: "Function", EntityName: "ProcessInternalPayment", Language: "go", SourceCache: "func ProcessInternalPayment() {}",
				},
				"go-private-helper": {
					EntityID: "go-private-helper", RelativePath: "pkg/payments/private.go", EntityType: "Function", EntityName: "processPrivatePayment", Language: "go", SourceCache: "func processPrivatePayment() {}",
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
	if got, want := len(results), 2; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}

	gotIDs := make([]string, 0, len(results))
	for _, raw := range results {
		result, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("result type = %T, want map[string]any", raw)
		}
		gotIDs = append(gotIDs, result["entity_id"].(string))
	}
	if got, want := gotIDs, []string{"go-internal-exported", "go-private-helper"}; !equalStringSlices(got, want) {
		t.Fatalf("result entity ids = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeRespectsLimitAndReportsTruncation(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if got, want := params["limit"], deadCodeCandidateQueryLimit(2); got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"entity_id": "fn-1", "name": "alpha", "labels": []any{"Function"},
						"file_path": "pkg/payments/a.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "fn-2", "name": "beta", "labels": []any{"Function"},
						"file_path": "pkg/payments/b.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "fn-3", "name": "gamma", "labels": []any{"Function"},
						"file_path": "pkg/payments/c.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"fn-1": {EntityID: "fn-1", RelativePath: "pkg/payments/a.go", EntityType: "Function", EntityName: "alpha", Language: "go", SourceCache: "func alpha() {}"},
				"fn-2": {EntityID: "fn-2", RelativePath: "pkg/payments/b.go", EntityType: "Function", EntityName: "beta", Language: "go", SourceCache: "func beta() {}"},
				"fn-3": {EntityID: "fn-3", RelativePath: "pkg/payments/c.go", EntityType: "Function", EntityName: "gamma", Language: "go", SourceCache: "func gamma() {}"},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":2}`),
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
	if got, want := len(results), 2; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	if got, want := resp["truncated"], true; got != want {
		t.Fatalf("resp[truncated] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeFetchesPolicyBufferBeforeApplyingLimit(t *testing.T) {
	t.Parallel()

	rawCandidates := []map[string]any{
		{
			"entity_id": "public-api-1", "name": "PublicAlpha", "labels": []any{"Function"},
			"file_path": "pkg/payments/a.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
		},
		{
			"entity_id": "public-api-2", "name": "PublicBeta", "labels": []any{"Function"},
			"file_path": "pkg/payments/b.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
		},
		{
			"entity_id": "public-api-3", "name": "PublicGamma", "labels": []any{"Function"},
			"file_path": "pkg/payments/c.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
		},
		{
			"entity_id": "internal-helper-1", "name": "privateAlpha", "labels": []any{"Function"},
			"file_path": "internal/payments/a.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
		},
		{
			"entity_id": "internal-helper-2", "name": "privateBeta", "labels": []any{"Function"},
			"file_path": "internal/payments/b.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
		},
	}

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				queryLimit, ok := params["limit"].(int)
				if !ok {
					t.Fatalf("params[limit] type = %T, want int", params["limit"])
				}
				if queryLimit <= 3 {
					t.Fatalf("params[limit] = %d, want policy buffer beyond display limit", queryLimit)
				}
				if queryLimit > len(rawCandidates) {
					queryLimit = len(rawCandidates)
				}
				return rawCandidates[:queryLimit], nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"public-api-1":      {EntityID: "public-api-1", RelativePath: "pkg/payments/a.go", EntityType: "Function", EntityName: "PublicAlpha", Language: "go", SourceCache: "func PublicAlpha() {}"},
				"public-api-2":      {EntityID: "public-api-2", RelativePath: "pkg/payments/b.go", EntityType: "Function", EntityName: "PublicBeta", Language: "go", SourceCache: "func PublicBeta() {}"},
				"public-api-3":      {EntityID: "public-api-3", RelativePath: "pkg/payments/c.go", EntityType: "Function", EntityName: "PublicGamma", Language: "go", SourceCache: "func PublicGamma() {}"},
				"internal-helper-1": {EntityID: "internal-helper-1", RelativePath: "internal/payments/a.go", EntityType: "Function", EntityName: "privateAlpha", Language: "go", SourceCache: "func privateAlpha() {}"},
				"internal-helper-2": {EntityID: "internal-helper-2", RelativePath: "internal/payments/b.go", EntityType: "Function", EntityName: "privateBeta", Language: "go", SourceCache: "func privateBeta() {}"},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":2}`),
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
	gotIDs := make([]string, 0, len(results))
	for _, raw := range results {
		result, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("result type = %T, want map[string]any", raw)
		}
		gotIDs = append(gotIDs, result["entity_id"].(string))
	}
	if got, want := gotIDs, []string{"internal-helper-1", "internal-helper-2"}; !equalStringSlices(got, want) {
		t.Fatalf("result entity ids = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeDistinguishesDisplayAndCandidateScanTruncation(t *testing.T) {
	t.Parallel()

	rawCandidates := make([]map[string]any, 0, deadCodeCandidateQueryLimit(2))
	for i := 0; i < deadCodeCandidateQueryLimit(2)-1; i++ {
		rawCandidates = append(rawCandidates, map[string]any{
			"entity_id": "public-api", "name": "PublicAPI", "labels": []any{"Function"},
			"file_path": "pkg/payments/api.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
		})
	}
	rawCandidates = append(rawCandidates, map[string]any{
		"entity_id": "internal-helper", "name": "privateAlpha", "labels": []any{"Function"},
		"file_path": "internal/payments/a.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
	})

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return rawCandidates, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"public-api": {
					EntityID:     "public-api",
					RelativePath: "pkg/payments/api.go",
					EntityType:   "Function",
					EntityName:   "PublicAPI",
					Language:     "go",
					SourceCache:  "func PublicAPI() {}",
				},
				"internal-helper": {
					EntityID:     "internal-helper",
					RelativePath: "internal/payments/a.go",
					EntityType:   "Function",
					EntityName:   "privateAlpha",
					Language:     "go",
					SourceCache:  "func privateAlpha() {}",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":2}`),
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
	if got, want := resp["truncated"], true; got != want {
		t.Fatalf("resp[truncated] = %#v, want %#v", got, want)
	}
	if got, want := resp["display_truncated"], false; got != want {
		t.Fatalf("resp[display_truncated] = %#v, want %#v", got, want)
	}
	if got, want := resp["candidate_scan_truncated"], true; got != want {
		t.Fatalf("resp[candidate_scan_truncated] = %#v, want %#v", got, want)
	}
	if got, want := resp["candidate_scan_limit"], float64(deadCodeCandidateQueryLimit(2)); got != want {
		t.Fatalf("resp[candidate_scan_limit] = %#v, want %#v", got, want)
	}
}

func TestDeadCodeCandidateQueryLimitUsesMinimumPolicyWindowForSmallDisplayLimits(t *testing.T) {
	t.Parallel()

	if got, want := deadCodeCandidateQueryLimit(5), deadCodeCandidateQueryMin; got != want {
		t.Fatalf("deadCodeCandidateQueryLimit(5) = %d, want %d", got, want)
	}
	if got, want := deadCodeCandidateQueryLimit(deadCodeMaxLimit), deadCodeCandidateQueryMax; got != want {
		t.Fatalf("deadCodeCandidateQueryLimit(deadCodeMaxLimit) = %d, want %d", got, want)
	}
}

func equalStringSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
