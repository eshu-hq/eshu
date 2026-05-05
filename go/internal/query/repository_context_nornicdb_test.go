package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetRepositoryContextOverridesOptionalAggregationCountsWithScalarQueries(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"OPTIONAL MATCH": {
					"id":               "repo-1",
					"name":             "api-node-datax",
					"path":             "/repos/api-node-datax",
					"local_path":       "/repos/api-node-datax",
					"has_remote":       false,
					"file_count":       int64(0),
					"workload_count":   int64(0),
					"platform_count":   int64(0),
					"dependency_count": int64(0),
				},
			},
			runByMatch: map[string][]map[string]any{
				"RETURN count(DISTINCT f) AS count": {
					{"count": int64(3399)},
				},
				"RETURN count(DISTINCT w) AS count": {
					{"count": int64(1)},
				},
				"RETURN count(DISTINCT p) AS count": {
					{"count": int64(4)},
				},
				"RETURN count(DISTINCT dep) AS count": {
					{"count": int64(0)},
				},
				"fn.name IN":                          {},
				"K8sResource OR":                      {},
				"f.language IS NOT NULL":              {},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/context", nil)
	req.SetPathValue("repo_id", "repo-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	for key, want := range map[string]float64{
		"file_count":     3399,
		"workload_count": 1,
		"platform_count": 4,
	} {
		if got := resp[key]; got != want {
			t.Fatalf("%s = %#v, want %#v", key, got, want)
		}
	}
}

func TestGetRepositoryStoryOverridesBroadAggregationWithAnchoredReads(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"OPTIONAL MATCH": {
					"id":               "repo-1",
					"name":             "catalog-api",
					"path":             "/repos/catalog-api",
					"local_path":       "/repos/catalog-api",
					"has_remote":       false,
					"file_count":       nil,
					"languages":        []any{"DISTINCT f.language"},
					"workload_names":   nil,
					"platform_types":   nil,
					"dependency_count": int64(0),
				},
			},
			runByMatch: map[string][]map[string]any{
				"RETURN count(DISTINCT f) AS count": {
					{"count": int64(537)},
				},
				"RETURN f.language AS language, count(DISTINCT f) AS file_count": {
					{"language": "json", "file_count": int64(336)},
					{"language": "javascript", "file_count": int64(139)},
					{"language": "yaml", "file_count": int64(49)},
				},
				"RETURN w.name AS workload_name": {
					{"workload_name": "catalog-api"},
				},
				"RETURN p.type AS platform_type": {},
				"RETURN count(DISTINCT dep) AS count": {
					{"count": int64(2)},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/story", nil)
	req.SetPathValue("repo_id", "repo-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	story := StringVal(resp, "story")
	if strings.Contains(story, "DISTINCT f.language") {
		t.Fatalf("story = %q, want no leaked Cypher expression", story)
	}
	if !strings.Contains(story, "Repository catalog-api contains 537 indexed files.") {
		t.Fatalf("story = %q, want anchored file count", story)
	}
	if !strings.Contains(story, "Defines 1 workload(s): catalog-api.") {
		t.Fatalf("story = %q, want anchored workload names", story)
	}
	deploymentOverview, ok := resp["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", resp["deployment_overview"])
	}
	if got, want := deploymentOverview["workload_count"], float64(1); got != want {
		t.Fatalf("deployment_overview.workload_count = %#v, want %#v", got, want)
	}
	supportOverview, ok := resp["support_overview"].(map[string]any)
	if !ok {
		t.Fatalf("support_overview type = %T, want map[string]any", resp["support_overview"])
	}
	if got, want := supportOverview["dependency_count"], float64(2); got != want {
		t.Fatalf("support_overview.dependency_count = %#v, want %#v", got, want)
	}
}
