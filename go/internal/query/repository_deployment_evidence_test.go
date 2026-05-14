package query

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetRepositoryContextIncludesGraphDeploymentEvidence(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"INSTANCE_OF": {
					"id":               "repo-service",
					"name":             "checkout-service",
					"path":             "/repos/checkout-service",
					"local_path":       "/repos/checkout-service",
					"remote_url":       "https://github.com/acme/checkout-service",
					"repo_slug":        "acme/checkout-service",
					"has_remote":       true,
					"file_count":       int64(42),
					"workload_count":   int64(1),
					"platform_count":   int64(2),
					"dependency_count": int64(3),
				},
			},
			runByMatch: map[string][]map[string]any{
				"EVIDENCES_REPOSITORY_RELATIONSHIP]->(r:Repository": {
					{
						"direction":             "incoming",
						"artifact_id":           "evidence-artifact:helm:1",
						"name":                  "HELM_VALUES_REFERENCE:charts/checkout/values-prod.yaml",
						"domain":                "deployment",
						"path":                  "charts/checkout/values-prod.yaml",
						"evidence_kind":         "HELM_VALUES_REFERENCE",
						"artifact_family":       "helm",
						"extractor":             "helm",
						"relationship_type":     "DEPLOYS_FROM",
						"resolved_id":           "resolved-1",
						"generation_id":         "gen-1",
						"confidence":            0.91,
						"start_line":            int64(24),
						"end_line":              int64(28),
						"environment":           "prod",
						"runtime_platform_kind": "kubernetes",
						"matched_alias":         "checkout-service",
						"matched_value":         "registry.example.test/checkout-service",
						"evidence_source":       "resolver/cross-repo",
						"source_repo_id":        "repo-deploy",
						"source_repo_name":      "platform-deployments",
						"target_repo_id":        "repo-service",
						"target_repo_name":      "checkout-service",
					},
				},
				"(r:Repository {id: $repo_id})-[source_rel:HAS_DEPLOYMENT_EVIDENCE]->": {
					{
						"direction":         "outgoing",
						"artifact_id":       "evidence-artifact:gha:1",
						"name":              "GITHUB_ACTIONS_REUSABLE_WORKFLOW_REF:.github/workflows/deploy.yaml",
						"domain":            "deployment",
						"path":              ".github/workflows/deploy.yaml",
						"evidence_kind":     "GITHUB_ACTIONS_REUSABLE_WORKFLOW_REF",
						"artifact_family":   "github_actions",
						"extractor":         "github_actions",
						"relationship_type": "DEPLOYS_FROM",
						"resolved_id":       "resolved-2",
						"generation_id":     "gen-1",
						"confidence":        0.82,
						"start_line":        int64(12),
						"end_line":          int64(16),
						"matched_alias":     "deploy.yaml",
						"matched_value":     ".github/workflows/deploy.yaml",
						"evidence_source":   "resolver/cross-repo",
						"source_repo_id":    "repo-service",
						"source_repo_name":  "checkout-service",
						"target_repo_id":    "repo-workflows",
						"target_repo_name":  "shared-workflows",
					},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-service/context", nil)
	req.SetPathValue("repo_id", "repo-service")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	surface, ok := resp["deployment_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_evidence type = %T, want map[string]any", resp["deployment_evidence"])
	}
	if got, want := surface["truth_basis"], "graph"; got != want {
		t.Fatalf("deployment_evidence.truth_basis = %#v, want %#v", got, want)
	}
	if got, want := surface["artifact_count"], float64(2); got != want {
		t.Fatalf("deployment_evidence.artifact_count = %#v, want %#v", got, want)
	}
	evidenceIndex, ok := surface["evidence_index"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_evidence.evidence_index type = %T, want map[string]any", surface["evidence_index"])
	}
	if got, want := evidenceIndex["lookup_basis"], "resolved_id"; got != want {
		t.Fatalf("evidence_index.lookup_basis = %#v, want %#v", got, want)
	}
	relationshipTypes := evidenceIndex["relationship_types"].(map[string]any)
	deploysFrom := relationshipTypes["DEPLOYS_FROM"].(map[string]any)
	if got, want := deploysFrom["artifact_count"], float64(2); got != want {
		t.Fatalf("DEPLOYS_FROM.artifact_count = %#v, want %#v", got, want)
	}
	if !containsStringAny(deploysFrom["resolved_ids"].([]any), "resolved-1") ||
		!containsStringAny(deploysFrom["resolved_ids"].([]any), "resolved-2") {
		t.Fatalf("DEPLOYS_FROM.resolved_ids = %#v, want both resolved ids", deploysFrom["resolved_ids"])
	}
	artifactFamilies := evidenceIndex["artifact_families"].(map[string]any)
	helm := artifactFamilies["helm"].(map[string]any)
	if got, want := helm["artifact_count"], float64(1); got != want {
		t.Fatalf("helm.artifact_count = %#v, want %#v", got, want)
	}
	if !containsStringAny(helm["resolved_ids"].([]any), "resolved-1") {
		t.Fatalf("helm.resolved_ids = %#v, want resolved-1", helm["resolved_ids"])
	}

	for _, want := range []string{"helm", "github_actions"} {
		if !containsStringAny(surface["artifact_families"].([]any), want) {
			t.Fatalf("artifact_families missing %q: %#v", want, surface["artifact_families"])
		}
	}
	for _, want := range []string{"HELM_VALUES_REFERENCE", "GITHUB_ACTIONS_REUSABLE_WORKFLOW_REF"} {
		if !containsStringAny(surface["evidence_kinds"].([]any), want) {
			t.Fatalf("evidence_kinds missing %q: %#v", want, surface["evidence_kinds"])
		}
	}
	if !containsStringAny(surface["environments"].([]any), "prod") {
		t.Fatalf("environments missing prod: %#v", surface["environments"])
	}

	artifacts, ok := surface["artifacts"].([]any)
	if !ok || len(artifacts) != 2 {
		t.Fatalf("deployment_evidence.artifacts = %#v, want two artifacts", surface["artifacts"])
	}
	first, ok := artifacts[0].(map[string]any)
	if !ok {
		t.Fatalf("deployment_evidence.artifacts[0] type = %T, want map[string]any", artifacts[0])
	}
	for key, want := range map[string]any{
		"id":                    "evidence-artifact:gha:1",
		"direction":             "outgoing",
		"artifact_family":       "github_actions",
		"relationship_type":     "DEPLOYS_FROM",
		"resolved_id":           "resolved-2",
		"generation_id":         "gen-1",
		"source_repo_id":        "repo-service",
		"target_repo_id":        "repo-workflows",
		"postgres_lookup_basis": "resolved_id",
	} {
		if got := first[key]; got != want {
			t.Fatalf("artifact[0].%s = %#v, want %#v; artifact=%#v", key, got, want, first)
		}
	}
	sourceLocation := first["source_location"].(map[string]any)
	for key, want := range map[string]any{
		"repo_id":    "repo-service",
		"repo_name":  "checkout-service",
		"path":       ".github/workflows/deploy.yaml",
		"start_line": float64(12),
		"end_line":   float64(16),
	} {
		if got := sourceLocation[key]; got != want {
			t.Fatalf("artifact[0].source_location.%s = %#v, want %#v; location=%#v", key, got, want, sourceLocation)
		}
	}
}

func TestGetRepositoryContextUsesReadModelForDeploymentEvidence(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (r:Repository {id: $repo_id})": {
					"id":         "repo-service",
					"name":       "checkout-service",
					"path":       "/repos/checkout-service",
					"local_path": "/repos/checkout-service",
					"has_remote": false,
				},
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "EvidenceArtifact") {
					t.Fatalf("cypher = %q, want deployment evidence read model before graph fallback", cypher)
				}
				return nil, nil
			},
		},
		Content: fakePortContentStore{
			deploymentEvidence: repositoryDeploymentEvidenceReadModel{
				Available: true,
				Rows: []map[string]any{
					{
						"direction":             "incoming",
						"artifact_id":           "evidence-artifact:1",
						"name":                  "environments/prod/ecs.tf",
						"domain":                "deployment",
						"path":                  "environments/prod/ecs.tf",
						"evidence_kind":         "TERRAFORM_ECS_SERVICE",
						"artifact_family":       "terraform",
						"extractor":             "terraform-runtime-service-module",
						"relationship_type":     "PROVISIONS_DEPENDENCY_FOR",
						"resolved_id":           "resolved-1",
						"generation_id":         "gen-1",
						"confidence":            0.96,
						"environment":           "prod",
						"runtime_platform_kind": "ecs",
						"matched_alias":         "checkout-service",
						"matched_value":         "checkout-service",
						"evidence_source":       "resolver/cross-repo",
						"source_repo_id":        "repo-terraform",
						"source_repo_name":      "terraform-live",
						"target_repo_id":        "repo-service",
						"target_repo_name":      "checkout-service",
					},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-service/context", nil)
	req.SetPathValue("repo_id", "repo-service")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	surface := resp["deployment_evidence"].(map[string]any)
	artifacts := surface["artifacts"].([]any)
	first := artifacts[0].(map[string]any)
	if got, want := first["direction"], "incoming"; got != want {
		t.Fatalf("direction = %#v, want %#v", got, want)
	}
	if got, want := first["source_repo_id"], "repo-terraform"; got != want {
		t.Fatalf("source_repo_id = %#v, want %#v", got, want)
	}
	if got, want := first["target_repo_id"], "repo-service"; got != want {
		t.Fatalf("target_repo_id = %#v, want %#v", got, want)
	}
}

func TestGetRepositoryContextReturnsErrorWhenDeploymentEvidenceReadModelFails(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (r:Repository {id: $repo_id})": {
					"id":         "repo-service",
					"name":       "checkout-service",
					"path":       "/repos/checkout-service",
					"local_path": "/repos/checkout-service",
					"has_remote": false,
				},
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "EvidenceArtifact") {
					t.Fatalf("cypher = %q, want no graph fallback after read-model error", cypher)
				}
				return nil, nil
			},
		},
		Content: fakePortContentStore{
			deploymentEvidenceErr: errors.New("postgres read model unavailable"),
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-service/context", nil)
	req.SetPathValue("repo_id", "repo-service")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "postgres read model unavailable") {
		t.Fatalf("body = %s, want read-model error", body)
	}
}

func TestContentReaderRepositoryDeploymentEvidenceHydratesPreviewArtifacts(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: contentReaderDeploymentEvidenceColumns(),
			rows: [][]driver.Value{
				{
					"incoming", "resolved-1", "gen-1", "repo-terraform", "terraform-live",
					"repo-service", "checkout-service", "PROVISIONS_DEPENDENCY_FOR", float64(0.96),
					[]byte(`{"evidence_preview":[{"kind":"TERRAFORM_ECS_SERVICE","confidence":0.96,"details":{"path":"environments/prod/ecs.tf","extractor":"terraform-runtime-service-module","matched_alias":"checkout-service","matched_value":"checkout-service","runtime_platform_kind":"ecs"}}]}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	got, err := reader.repositoryDeploymentEvidence(t.Context(), "repo-service")
	if err != nil {
		t.Fatalf("repositoryDeploymentEvidence() error = %v, want nil", err)
	}
	if !got.Available {
		t.Fatal("repositoryDeploymentEvidence().Available = false, want true")
	}
	if len(got.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(got.Rows))
	}
	row := got.Rows[0]
	for key, want := range map[string]any{
		"direction":             "incoming",
		"source_repo_id":        "repo-terraform",
		"source_repo_name":      "terraform-live",
		"target_repo_id":        "repo-service",
		"target_repo_name":      "checkout-service",
		"environment":           "prod",
		"runtime_platform_kind": "ecs",
	} {
		if got := row[key]; got != want {
			t.Fatalf("Rows[0].%s = %#v, want %#v", key, got, want)
		}
	}
}

func TestQueryRepoDeploymentEvidenceIncomingUsesArtifactFirstBoundary(t *testing.T) {
	t.Parallel()

	reader := &recordingDeploymentEvidenceGraphReader{}
	if _, err := queryRepoDeploymentEvidence(context.Background(), reader, nil, map[string]any{"repo_id": "repo-service"}); err != nil {
		t.Fatalf("queryRepoDeploymentEvidence() error = %v, want nil", err)
	}

	if len(reader.cypherCalls) != 2 {
		t.Fatalf("len(cypherCalls) = %d, want 2", len(reader.cypherCalls))
	}
	incoming := reader.cypherCalls[1]
	for _, want := range []string{
		"MATCH (artifact:EvidenceArtifact)-[:EVIDENCES_REPOSITORY_RELATIONSHIP]->(r:Repository {id: $repo_id})",
		"WITH artifact, r",
		"MATCH (source:Repository)-[:HAS_DEPLOYMENT_EVIDENCE]->(artifact)",
	} {
		if !strings.Contains(incoming, want) {
			t.Fatalf("incoming query missing %q:\n%s", want, incoming)
		}
	}
	oldShape := "MATCH (source:Repository)-[:HAS_DEPLOYMENT_EVIDENCE]->(artifact:EvidenceArtifact)-[:EVIDENCES_REPOSITORY_RELATIONSHIP]->(r:Repository {id: $repo_id})"
	if strings.Contains(incoming, oldShape) {
		t.Fatalf("incoming query still uses source-first NornicDB-slow shape:\n%s", incoming)
	}
}

type recordingDeploymentEvidenceGraphReader struct {
	cypherCalls []string
	params      []map[string]any
}

func (r *recordingDeploymentEvidenceGraphReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.cypherCalls = append(r.cypherCalls, cypher)
	r.params = append(r.params, params)
	return nil, nil
}

func (r *recordingDeploymentEvidenceGraphReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}
