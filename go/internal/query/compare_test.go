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

type fakeCompareGraphReader struct {
	runSingle func(context.Context, string, map[string]any) (map[string]any, error)
	run       func(context.Context, string, map[string]any) ([]map[string]any, error)
}

func (f fakeCompareGraphReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	if f.runSingle != nil {
		return f.runSingle(ctx, cypher, params)
	}
	return nil, nil
}

func (f fakeCompareGraphReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if f.run != nil {
		return f.run(ctx, cypher, params)
	}
	return nil, nil
}

func TestCompareEnvironmentsReturnsPresentSnapshotsFromMaterializedInstances(t *testing.T) {
	t.Parallel()

	handler := &CompareHandler{
		Neo4j: fakeCompareGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload)"):
					return map[string]any{
						"id":      "workload:service-edge-api",
						"name":    "service-edge-api",
						"kind":    "service",
						"repo_id": "repo-service-edge-api",
					}, nil
				case strings.Contains(cypher, "MATCH (i:WorkloadInstance)"):
					switch params["environment"] {
					case "qa":
						return map[string]any{
							"id":          "instance:qa",
							"name":        "service-edge-api-qa",
							"kind":        "service",
							"environment": "qa",
							"workload_id": "workload:service-edge-api",
						}, nil
					case "prod":
						return map[string]any{
							"id":          "instance:prod",
							"name":        "service-edge-api-prod",
							"kind":        "service",
							"environment": "prod",
							"workload_id": "workload:service-edge-api",
						}, nil
					}
				}
				return nil, nil
			},
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "MATCH (i:WorkloadInstance)-[r:USES]->(c:CloudResource)") {
					return nil, nil
				}
				switch params["instance_id"] {
				case "instance:qa":
					return []map[string]any{
						{
							"id":          "cloud:queue-qa",
							"name":        "queue-qa",
							"environment": "qa",
							"kind":        "queue",
							"provider":    "aws",
							"confidence":  1.0,
							"reason":      "materialized_cloud_dependency",
						},
					}, nil
				case "instance:prod":
					return []map[string]any{
						{
							"id":          "cloud:queue-prod",
							"name":        "queue-prod",
							"environment": "prod",
							"kind":        "queue",
							"provider":    "aws",
							"confidence":  0.8,
							"reason":      "materialized_cloud_dependency",
						},
					}, nil
				}
				return nil, nil
			},
		},
	}

	resp := executeCompareEnvironmentsRequest(t, handler, `{"workload_id":"workload:service-edge-api","left":"qa","right":"prod"}`)

	left := requireMap(t, resp, "left")
	if got, want := left["status"], "present"; got != want {
		t.Fatalf("left.status = %#v, want %#v", got, want)
	}
	leftProvenance := requireMapSlice(t, left, "provenance")
	if len(leftProvenance) != 1 {
		t.Fatalf("len(left.provenance) = %d, want 1", len(leftProvenance))
	}
	if got, want := leftProvenance[0]["kind"], "materialized_workload_instance"; got != want {
		t.Fatalf("left.provenance[0].kind = %#v, want %#v", got, want)
	}

	changed := requireMap(t, resp, "changed")
	cloudResources := requireMapSlice(t, changed, "cloud_resources")
	if len(cloudResources) != 2 {
		t.Fatalf("len(changed.cloud_resources) = %d, want 2", len(cloudResources))
	}

	if got, want := resp["reason"], "Comparison based on materialized cloud resource differences"; got != want {
		t.Fatalf("reason = %#v, want %#v", got, want)
	}
	if got, want := resp["confidence"], float64(0.9); got != want {
		t.Fatalf("confidence = %#v, want %#v", got, want)
	}
}

func TestCompareEnvironmentsBoundsResourceReadsAndReportsTruncation(t *testing.T) {
	t.Parallel()

	handler := &CompareHandler{
		Neo4j: fakeCompareGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload)"):
					return map[string]any{
						"id":      "workload:service-edge-api",
						"name":    "service-edge-api",
						"kind":    "service",
						"repo_id": "repo-service-edge-api",
					}, nil
				case strings.Contains(cypher, "MATCH (i:WorkloadInstance)"):
					env := params["environment"]
					return map[string]any{
						"id":          "instance:" + env.(string),
						"name":        "service-edge-api-" + env.(string),
						"kind":        "service",
						"environment": env,
						"workload_id": "workload:service-edge-api",
					}, nil
				}
				return nil, nil
			},
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("resource cypher = %q, want LIMIT $limit", cypher)
				}
				if !strings.Contains(cypher, "ORDER BY c.name, c.id") {
					t.Fatalf("resource cypher = %q, want deterministic resource ordering", cypher)
				}
				if got, want := params["limit"], 2; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{"id": "cloud:" + params["instance_id"].(string) + ":a", "name": "queue-a", "kind": "queue", "provider": "aws", "confidence": 1.0},
					{"id": "cloud:" + params["instance_id"].(string) + ":b", "name": "queue-b", "kind": "queue", "provider": "aws", "confidence": 1.0},
				}, nil
			},
		},
	}

	resp := executeCompareEnvironmentsRequest(t, handler, `{"workload_id":"workload:service-edge-api","left":"qa","right":"prod","limit":1}`)

	if got, want := resp["limit"], float64(1); got != want {
		t.Fatalf("limit = %#v, want %#v", got, want)
	}
	if got, want := resp["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	coverage := requireMap(t, resp, "coverage")
	if got, want := coverage["left_truncated"], true; got != want {
		t.Fatalf("coverage.left_truncated = %#v, want %#v", got, want)
	}
	if got, want := coverage["right_truncated"], true; got != want {
		t.Fatalf("coverage.right_truncated = %#v, want %#v", got, want)
	}
	left := requireMap(t, resp, "left")
	leftResources := requireMapSlice(t, left, "cloud_resources")
	if len(leftResources) != 1 {
		t.Fatalf("len(left.cloud_resources) = %d, want 1", len(leftResources))
	}
}

func TestCompareEnvironmentsReturnsInferredSnapshotsFromServiceEvidence(t *testing.T) {
	t.Parallel()

	handler := &CompareHandler{
		Neo4j: fakeCompareGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload)"):
					return map[string]any{
						"id":      "workload:service-edge-api",
						"name":    "service-edge-api",
						"kind":    "service",
						"repo_id": "repo-service-edge-api",
					}, nil
				case strings.Contains(cypher, "MATCH (i:WorkloadInstance)"):
					return nil, nil
				}
				return nil, nil
			},
		},
		Content: &stubCompareEvidenceReader{
			files: []FileContent{
				{RepoID: "repo-service-edge-api", RelativePath: "deploy/qa/service-edge-api.yaml"},
				{RepoID: "repo-service-edge-api", RelativePath: "deploy/prod/service-edge-api.yaml"},
			},
			fileContent: map[string]string{
				"deploy/qa/service-edge-api.yaml":   "spec:\n  rules:\n    - host: service-edge-api.qa.example.test\n",
				"deploy/prod/service-edge-api.yaml": "spec:\n  rules:\n    - host: service-edge-api.prod.example.test\n",
			},
		},
	}

	resp := executeCompareEnvironmentsRequest(t, handler, `{"workload_id":"workload:service-edge-api","left":"qa","right":"prod"}`)

	left := requireMap(t, resp, "left")
	if got, want := left["status"], "inferred"; got != want {
		t.Fatalf("left.status = %#v, want %#v", got, want)
	}
	if got, want := left["reason"], "environment inferred from service evidence; no materialized workload instance found"; got != want {
		t.Fatalf("left.reason = %#v, want %#v", got, want)
	}
	leftProvenance := requireMapSlice(t, left, "provenance")
	if len(leftProvenance) != 2 {
		t.Fatalf("len(left.provenance) = %d, want 2", len(leftProvenance))
	}
	if got, want := leftProvenance[0]["kind"], "service_environment_evidence"; got != want {
		t.Fatalf("left.provenance[0].kind = %#v, want %#v", got, want)
	}

	right := requireMap(t, resp, "right")
	if got, want := right["status"], "inferred"; got != want {
		t.Fatalf("right.status = %#v, want %#v", got, want)
	}

	changed := requireMap(t, resp, "changed")
	cloudResources := requireMapSlice(t, changed, "cloud_resources")
	if len(cloudResources) != 0 {
		t.Fatalf("len(changed.cloud_resources) = %d, want 0", len(cloudResources))
	}

	if got, want := resp["reason"], "Comparison limited to inferred environment evidence; cloud resource differences require materialized workload instances"; got != want {
		t.Fatalf("reason = %#v, want %#v", got, want)
	}
	if got, want := resp["confidence"], float64(0.35); got != want {
		t.Fatalf("confidence = %#v, want %#v", got, want)
	}
}

func TestCompareEnvironmentsKeepsMixedPresentAndInferredStatesHonest(t *testing.T) {
	t.Parallel()

	handler := &CompareHandler{
		Neo4j: fakeCompareGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload)"):
					return map[string]any{
						"id":      "workload:service-edge-api",
						"name":    "service-edge-api",
						"kind":    "service",
						"repo_id": "repo-service-edge-api",
					}, nil
				case strings.Contains(cypher, "MATCH (i:WorkloadInstance)"):
					if params["environment"] == "qa" {
						return map[string]any{
							"id":          "instance:qa",
							"name":        "service-edge-api-qa",
							"kind":        "service",
							"environment": "qa",
							"workload_id": "workload:service-edge-api",
						}, nil
					}
					return nil, nil
				}
				return nil, nil
			},
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "MATCH (i:WorkloadInstance)-[r:USES]->(c:CloudResource)") && params["instance_id"] == "instance:qa" {
					return []map[string]any{
						{
							"id":          "cloud:queue-qa",
							"name":        "queue-qa",
							"environment": "qa",
							"kind":        "queue",
							"provider":    "aws",
							"confidence":  1.0,
							"reason":      "materialized_cloud_dependency",
						},
					}, nil
				}
				return nil, nil
			},
		},
		Content: &stubCompareEvidenceReader{
			files: []FileContent{
				{RepoID: "repo-service-edge-api", RelativePath: "deploy/prod/service-edge-api.yaml"},
			},
			fileContent: map[string]string{
				"deploy/prod/service-edge-api.yaml": "spec:\n  rules:\n    - host: service-edge-api.prod.example.test\n",
			},
		},
	}

	resp := executeCompareEnvironmentsRequest(t, handler, `{"workload_id":"workload:service-edge-api","left":"qa","right":"prod"}`)

	left := requireMap(t, resp, "left")
	if got, want := left["status"], "present"; got != want {
		t.Fatalf("left.status = %#v, want %#v", got, want)
	}
	right := requireMap(t, resp, "right")
	if got, want := right["status"], "inferred"; got != want {
		t.Fatalf("right.status = %#v, want %#v", got, want)
	}

	changed := requireMap(t, resp, "changed")
	cloudResources := requireMapSlice(t, changed, "cloud_resources")
	if len(cloudResources) != 0 {
		t.Fatalf("len(changed.cloud_resources) = %d, want 0", len(cloudResources))
	}
	if got, want := resp["reason"], "Comparison limited to inferred environment evidence; cloud resource differences require materialized workload instances"; got != want {
		t.Fatalf("reason = %#v, want %#v", got, want)
	}
}

func TestCompareEnvironmentsReturnsExplicitUnsupportedWhenEvidenceIsTrulyAbsent(t *testing.T) {
	t.Parallel()

	handler := &CompareHandler{
		Neo4j: fakeCompareGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload)"):
					return map[string]any{
						"id":      "workload:service-edge-api",
						"name":    "service-edge-api",
						"kind":    "service",
						"repo_id": "repo-service-edge-api",
					}, nil
				case strings.Contains(cypher, "MATCH (i:WorkloadInstance)"):
					return nil, nil
				}
				return nil, nil
			},
		},
	}

	resp := executeCompareEnvironmentsRequest(t, handler, `{"workload_id":"workload:service-edge-api","left":"qa","right":"prod"}`)

	left := requireMap(t, resp, "left")
	if got, want := left["status"], "unsupported"; got != want {
		t.Fatalf("left.status = %#v, want %#v", got, want)
	}
	if got, want := left["reason"], "no materialized workload instance or inferable service evidence found for environment"; got != want {
		t.Fatalf("left.reason = %#v, want %#v", got, want)
	}
	if got := requireMapSlice(t, left, "provenance"); len(got) != 0 {
		t.Fatalf("len(left.provenance) = %d, want 0", len(got))
	}

	if got, want := resp["confidence"], float64(0); got != want {
		t.Fatalf("confidence = %#v, want %#v", got, want)
	}
	if got, want := resp["reason"], "Comparison unsupported: one or both environments lack materialized instances and inferable environment evidence"; got != want {
		t.Fatalf("reason = %#v, want %#v", got, want)
	}
}

func executeCompareEnvironmentsRequest(t *testing.T, handler *CompareHandler, body string) map[string]any {
	t.Helper()

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/compare/environments", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return resp
}

func requireMap(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()

	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("%s type = %T, want map[string]any", key, parent[key])
	}
	return value
}

func requireMapSlice(t *testing.T, parent map[string]any, key string) []map[string]any {
	t.Helper()

	rows := compareMapSlice(parent, key)
	if rows == nil {
		t.Fatalf("%s type = %T, want []map[string]any", key, parent[key])
	}
	return rows
}

type stubCompareEvidenceReader struct {
	files       []FileContent
	fileContent map[string]string
}

func (s *stubCompareEvidenceReader) ListRepoFiles(context.Context, string, int) ([]FileContent, error) {
	return append([]FileContent(nil), s.files...), nil
}

func (s *stubCompareEvidenceReader) GetFileContent(_ context.Context, repoID, relativePath string) (*FileContent, error) {
	content, ok := s.fileContent[relativePath]
	if !ok {
		return nil, nil
	}
	return &FileContent{
		RepoID:       repoID,
		RelativePath: relativePath,
		Content:      content,
	}, nil
}
