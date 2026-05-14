package query

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeWorkloadGraphReader dispatches Cypher queries based on content matching.
type fakeWorkloadGraphReader struct {
	runSingleByMatch map[string]map[string]any
	runByMatch       map[string][]map[string]any
	run              func(context.Context, string, map[string]any) ([]map[string]any, error)
	runSingle        func(context.Context, string, map[string]any) (map[string]any, error)
}

func (f fakeWorkloadGraphReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if f.run != nil {
		return f.run(ctx, cypher, params)
	}
	var (
		bestRows []map[string]any
		bestLen  int
	)
	for fragment, rows := range f.runByMatch {
		if strings.Contains(cypher, fragment) && len(fragment) > bestLen {
			bestRows = rows
			bestLen = len(fragment)
		}
	}
	return bestRows, nil
}

func (f fakeWorkloadGraphReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	if f.runSingle != nil {
		return f.runSingle(ctx, cypher, params)
	}
	var (
		bestRow map[string]any
		bestLen int
	)
	for fragment, row := range f.runSingleByMatch {
		if strings.Contains(cypher, fragment) && len(fragment) > bestLen {
			bestRow = row
			bestLen = len(fragment)
		}
	}
	return bestRow, nil
}

func TestGetWorkloadContextReturnsEnrichedResponse(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				// Base workload query
				"MATCH (w:Workload)": {
					"id":        "workload-1",
					"name":      "order-service",
					"kind":      "Deployment",
					"repo_id":   "repo-1",
					"repo_name": "order-service",
					"instances": []any{
						map[string]any{
							"instance_id":   "inst-1",
							"platform_name": "eks-prod",
							"platform_kind": "EKS",
							"environment":   "production",
						},
					},
				},
			},
			runByMatch: map[string][]map[string]any{
				// Dependencies query
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {
					{
						"type":          "DEPENDS_ON",
						"target_name":   "auth-service",
						"target_id":     "repo-2",
						"evidence_type": "terraform_module_source",
					},
				},
				// Infrastructure query
				"K8sResource OR": {
					{
						"type":      "K8sResource",
						"name":      "order-deployment",
						"kind":      "Deployment",
						"file_path": "k8s/deployment.yaml",
					},
				},
				// Entry points query
				"fn.name IN": {
					{
						"name":          "main",
						"relative_path": "cmd/server/main.go",
						"language":      "go",
					},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/workload-1/context", nil)
	req.SetPathValue("workload_id", "workload-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Base workload fields present
	if got, want := resp["id"], "workload-1"; got != want {
		t.Fatalf("id = %v, want %v", got, want)
	}
	if got, want := resp["name"], "order-service"; got != want {
		t.Fatalf("name = %v, want %v", got, want)
	}
	if got, want := resp["repo_id"], "repo-1"; got != want {
		t.Fatalf("repo_id = %v, want %v", got, want)
	}

	// Instances present
	instances, ok := resp["instances"].([]any)
	if !ok {
		t.Fatalf("instances type = %T, want []any", resp["instances"])
	}
	if len(instances) != 1 {
		t.Fatalf("len(instances) = %d, want 1", len(instances))
	}

	// Dependencies from repo enrichment
	deps, ok := resp["dependencies"].([]any)
	if !ok {
		t.Fatalf("dependencies type = %T, want []any", resp["dependencies"])
	}
	if len(deps) != 1 {
		t.Fatalf("len(dependencies) = %d, want 1", len(deps))
	}
	dep0, ok := deps[0].(map[string]any)
	if !ok {
		t.Fatalf("dependencies[0] type = %T", deps[0])
	}
	if got, want := dep0["type"], "DEPENDS_ON"; got != want {
		t.Fatalf("dependencies[0].type = %v, want %v", got, want)
	}

	// Infrastructure from repo enrichment
	infra, ok := resp["infrastructure"].([]any)
	if !ok {
		t.Fatalf("infrastructure type = %T, want []any", resp["infrastructure"])
	}
	if len(infra) != 1 {
		t.Fatalf("len(infrastructure) = %d, want 1", len(infra))
	}

	if _, exists := resp["entry_points"]; exists {
		t.Fatalf("entry_points = %#v, want omitted for workload context", resp["entry_points"])
	}
}

func TestFetchWorkloadContextLogsStageTimings(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := &EntityHandler{
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (w:Workload)": {
					"id":   "workload-1",
					"name": "checkout-service",
					"kind": "service",
				},
			},
			runByMatch: map[string][]map[string]any{
				"DEFINES": {
					{
						"repo_id":   "repo-1",
						"repo_name": "checkout-service",
					},
				},
				"INSTANCE_OF":                         {},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
			},
		},
	}

	got, err := handler.fetchWorkloadContext(
		t.Context(),
		"w.id = $workload_id",
		map[string]any{"workload_id": "workload-1"},
	)
	if err != nil {
		t.Fatalf("fetchWorkloadContext() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("fetchWorkloadContext() = nil, want context")
	}

	logText := logs.String()
	for _, want := range []string{
		`"event_name":"service_query.stage_started"`,
		`"event_name":"service_query.stage_completed"`,
		`"operation":"workload_context"`,
		`"stage":"workload_lookup"`,
		`"stage":"repository_lookup"`,
		`"duration_seconds"`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s; logs = %s", want, logText)
		}
	}
}

func TestGetWorkloadContextReturnsNotFoundForMissingWorkload(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{},
			runByMatch:       map[string][]map[string]any{},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/unknown/context", nil)
	req.SetPathValue("workload_id", "unknown")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestGetWorkloadContextSkipsEnrichmentWhenNoRepoID(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (w:Workload)": {
					"id":        "workload-orphan",
					"name":      "orphan-service",
					"kind":      "Deployment",
					"repo_id":   "",
					"repo_name": "",
					"instances": []any{},
				},
			},
			runByMatch: map[string][]map[string]any{},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/workload-orphan/context", nil)
	req.SetPathValue("workload_id", "workload-orphan")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// No enrichment sections when repo_id is empty
	if _, exists := resp["dependencies"]; exists {
		t.Fatal("dependencies should not be present when repo_id is empty")
	}
	if _, exists := resp["infrastructure"]; exists {
		t.Fatal("infrastructure should not be present when repo_id is empty")
	}
	if _, exists := resp["entry_points"]; exists {
		t.Fatal("entry_points should not be present when repo_id is empty")
	}
}

func TestGetServiceContextAcceptsQualifiedWorkloadID(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.id = $service_name": {
					"id":        "workload:service-edge-api",
					"name":      "service-edge-api",
					"kind":      "Deployment",
					"repo_id":   "repo-1",
					"repo_name": "service-edge-api",
					"instances": []any{
						map[string]any{
							"instance_id":   "inst-1",
							"platform_name": "eks-prod",
							"platform_kind": "EKS",
							"environment":   "production",
						},
					},
				},
			},
			runByMatch: map[string][]map[string]any{
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/workload:service-edge-api/context", nil)
	req.SetPathValue("service_name", "workload:service-edge-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got, want := resp["id"], "workload:service-edge-api"; got != want {
		t.Fatalf("id = %v, want %v", got, want)
	}
	if got, want := resp["name"], "service-edge-api"; got != want {
		t.Fatalf("name = %v, want %v", got, want)
	}
}

func TestGetServiceStoryAcceptsPlainServiceName(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": {
					"id":        "workload:service-edge-api",
					"name":      "service-edge-api",
					"kind":      "Deployment",
					"repo_id":   "repo-1",
					"repo_name": "service-edge-api",
					"instances": []any{
						map[string]any{
							"instance_id":   "inst-1",
							"platform_name": "eks-prod",
							"platform_kind": "EKS",
							"environment":   "production",
						},
					},
				},
			},
			runByMatch: map[string][]map[string]any{
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/service-edge-api/story", nil)
	req.SetPathValue("service_name", "service-edge-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got, want := resp["service_name"], "service-edge-api"; got != want {
		t.Fatalf("service_name = %v, want %v", got, want)
	}
	if got, ok := resp["story"].(string); !ok || !strings.Contains(got, "Workload service-edge-api") {
		t.Fatalf("story = %#v, want narrative for service-edge-api", resp["story"])
	}
}

func TestGetServiceStoryAcceptsQualifiedWorkloadIDAndNormalizesServiceName(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.id = $service_name": {
					"id":        "workload:service-edge-api",
					"name":      "service-edge-api",
					"kind":      "Deployment",
					"repo_id":   "repo-1",
					"repo_name": "service-edge-api",
					"instances": []any{
						map[string]any{
							"instance_id":   "inst-1",
							"platform_name": "eks-prod",
							"platform_kind": "EKS",
							"environment":   "production",
						},
					},
				},
			},
			runByMatch: map[string][]map[string]any{
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/workload:service-edge-api/story", nil)
	req.SetPathValue("service_name", "workload:service-edge-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got, want := resp["service_name"], "service-edge-api"; got != want {
		t.Fatalf("service_name = %v, want %v", got, want)
	}
	if got, ok := resp["story"].(string); !ok || !strings.Contains(got, "Workload service-edge-api") {
		t.Fatalf("story = %#v, want narrative for service-edge-api", resp["story"])
	}
}

func TestGetServiceContextOmitsRepoEntryPoints(t *testing.T) {
	t.Parallel()

	entryPointQueried := false
	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": {
					"id":        "workload:service-edge-api",
					"name":      "service-edge-api",
					"kind":      "Deployment",
					"repo_id":   "repo-1",
					"repo_name": "service-edge-api",
					"instances": []any{
						map[string]any{
							"instance_id":   "inst-1",
							"platform_name": "eks-prod",
							"platform_kind": "EKS",
							"environment":   "production",
						},
					},
				},
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (r:Repository)-[:DEFINES]->(w)"):
					return []map[string]any{{"repo_id": "repo-1", "repo_name": "service-edge-api"}}, nil
				case strings.Contains(cypher, "fn.name IN"):
					entryPointQueried = true
					return []map[string]any{{"name": "main", "relative_path": "cmd/server/main.go", "language": "go"}}, nil
				default:
					return nil, nil
				}
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/service-edge-api/context", nil)
	req.SetPathValue("service_name", "service-edge-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if _, exists := resp["entry_points"]; exists {
		t.Fatalf("entry_points = %#v, want omitted for service context", resp["entry_points"])
	}
	if entryPointQueried {
		t.Fatal("service context queried repo entry points even though enrichment omits them")
	}
}

func TestFetchWorkloadContextAnchorsFollowUpQueriesByResolvedWorkloadID(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": {
					"id":   "workload:service-edge-api",
					"name": "service-edge-api",
					"kind": "service",
				},
			},
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "w.name = $service_name OR w.id = $service_name") {
					t.Fatalf("follow-up cypher used broad service lookup after workload id was resolved: %s", cypher)
				}
				if strings.Contains(cypher, "MATCH (w:Workload)") && params["workload_id"] != "workload:service-edge-api" {
					got := params["workload_id"]
					t.Fatalf("params[workload_id] = %#v, want %#v", got, "workload:service-edge-api")
				}
				if strings.Contains(cypher, "MATCH (r:Repository)-[:DEFINES]->(w)") {
					return []map[string]any{{"repo_id": "repo-1", "repo_name": "service-edge-api"}}, nil
				}
				return nil, nil
			},
		},
	}

	ctx, err := handler.fetchWorkloadContext(
		context.Background(),
		serviceLookupWhereClause,
		map[string]any{"service_name": "service-edge-api"},
	)
	if err != nil {
		t.Fatalf("fetchWorkloadContext() error = %v, want nil", err)
	}
	if got, want := ctx["repo_id"], "repo-1"; got != want {
		t.Fatalf("ctx[repo_id] = %#v, want %#v", got, want)
	}
}

func TestGetServiceContextIncludesGraphDeploymentEvidenceWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": {
					"id":        "workload:checkout-service",
					"name":      "checkout-service",
					"kind":      "service",
					"repo_id":   "repo-service",
					"repo_name": "checkout-service",
					"instances": []any{},
				},
			},
			runByMatch: map[string][]map[string]any{
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
				"(r:Repository {id: $repo_id})-[source_rel:HAS_DEPLOYMENT_EVIDENCE]->": {
					{
						"direction":         "outgoing",
						"artifact_id":       "evidence-artifact:gha:1",
						"name":              ".github/workflows/deploy.yml",
						"domain":            "deployment",
						"path":              ".github/workflows/deploy.yml",
						"evidence_kind":     "GITHUB_ACTIONS_REUSABLE_WORKFLOW",
						"artifact_family":   "github_actions",
						"extractor":         "github_actions",
						"relationship_type": "DEPLOYS_FROM",
						"resolved_id":       "resolved-ci",
						"generation_id":     "gen-service",
						"confidence":        0.93,
						"matched_alias":     "shared-workflows",
						"matched_value":     "org/shared-workflows/.github/workflows/deploy.yml@v1",
						"evidence_source":   "resolver/cross-repo",
						"source_repo_id":    "repo-service",
						"source_repo_name":  "checkout-service",
						"target_repo_id":    "repo-workflows",
						"target_repo_name":  "shared-workflows",
					},
				},
				"EVIDENCES_REPOSITORY_RELATIONSHIP]->(r:Repository": {
					{
						"direction":         "incoming",
						"artifact_id":       "evidence-artifact:helm:1",
						"name":              "charts/checkout/values-prod.yaml",
						"domain":            "deployment",
						"path":              "charts/checkout/values-prod.yaml",
						"evidence_kind":     "HELM_VALUES_REFERENCE",
						"artifact_family":   "helm",
						"extractor":         "helm",
						"relationship_type": "DEPLOYS_FROM",
						"resolved_id":       "resolved-helm",
						"generation_id":     "gen-deploy",
						"confidence":        0.84,
						"environment":       "prod",
						"matched_alias":     "checkout-service",
						"matched_value":     "registry.example.test/checkout-service",
						"evidence_source":   "resolver/cross-repo",
						"source_repo_id":    "repo-deploy",
						"source_repo_name":  "deployment-configs",
						"target_repo_id":    "repo-service",
						"target_repo_name":  "checkout-service",
					},
				},
				"EXPOSES_ENDPOINT]->(endpoint:Endpoint)": {
					{
						"endpoint_id":     "endpoint:checkout:health",
						"path":            "/health",
						"methods":         []any{"get"},
						"source_kinds":    []any{"framework:express"},
						"source_paths":    []any{"src/server.ts"},
						"evidence_source": "workload_materialization",
						"workload_id":     "workload:checkout-service",
						"workload_name":   "checkout-service",
					},
				},
				"RETURN count(endpoint) AS endpoint_count": {
					{"endpoint_count": 1},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/checkout-service/context", nil)
	req.SetPathValue("service_name", "checkout-service")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	evidence, ok := resp["deployment_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_evidence type = %T, want map[string]any", resp["deployment_evidence"])
	}
	if got, want := evidence["truth_basis"], "graph"; got != want {
		t.Fatalf("deployment_evidence.truth_basis = %#v, want %#v", got, want)
	}
	if got, want := evidence["artifact_count"], float64(2); got != want {
		t.Fatalf("deployment_evidence.artifact_count = %#v, want %#v", got, want)
	}
	for _, want := range []string{"github_actions", "helm"} {
		if !containsStringAny(evidence["artifact_families"].([]any), want) {
			t.Fatalf("artifact_families missing %q: %#v", want, evidence["artifact_families"])
		}
	}

	apiSurface, ok := resp["api_surface"].(map[string]any)
	if !ok {
		t.Fatalf("api_surface type = %T, want map[string]any", resp["api_surface"])
	}
	if got, want := apiSurface["truth_basis"], "graph"; got != want {
		t.Fatalf("api_surface.truth_basis = %#v, want %#v", got, want)
	}
	if got, want := apiSurface["endpoint_count"], float64(1); got != want {
		t.Fatalf("api_surface.endpoint_count = %#v, want %#v", got, want)
	}
}

func TestBuildWorkloadStorySurfacesObservedServiceSignalsWithoutMaterializedInstances(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"name":      "sample-service-api",
		"kind":      "service",
		"repo_name": "sample-service-api",
		"instances": []map[string]any{},
		"observed_config_environments": []string{
			"production",
			"qa",
		},
		"hostnames": []map[string]any{
			{"hostname": "sample-service-api.qa.example.com"},
			{"hostname": "sample-service-api.production.example.com"},
		},
		"api_surface": map[string]any{
			"endpoint_count": 21,
			"spec_count":     1,
			"docs_routes":    []string{"/_specs"},
		},
	}

	story := buildWorkloadStory(ctx)
	for _, fragment := range []string{
		"No materialized workload instances found.",
		"Observed config environments: production, qa.",
		"Public entrypoints: sample-service-api.production.example.com, sample-service-api.qa.example.com.",
		"API surface exposes 21 endpoint(s) across 1 spec file(s).",
		"Docs routes: /_specs.",
	} {
		if !strings.Contains(story, fragment) {
			t.Fatalf("story = %q, want fragment %q", story, fragment)
		}
	}
}
