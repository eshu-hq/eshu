package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetRepositoryContextIncludesDockerComposeDeploymentArtifacts(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-deploy", "docker-compose.yaml", "abc123", "",
					"hash-compose", int64(20), "yaml", "docker_compose",
				},
			},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-deploy", "docker-compose.yaml", "abc123", `services:
  api:
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/healthz"]
    ports:
      - "8080:8080"
    environment:
      LOG_LEVEL: info
`,
					"hash-compose", int64(20), "yaml", "docker_compose",
				},
			},
		},
	})

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"INSTANCE_OF": {
					"id":               "repo-deploy",
					"name":             "compose-deploy",
					"path":             "/repos/compose-deploy",
					"local_path":       "/repos/compose-deploy",
					"remote_url":       "https://github.com/acme/compose-deploy",
					"repo_slug":        "acme/compose-deploy",
					"has_remote":       true,
					"file_count":       int64(1),
					"workload_count":   int64(0),
					"platform_count":   int64(0),
					"dependency_count": int64(0),
				},
			},
		},
		Content: NewContentReader(db),
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-deploy/context", nil)
	req.SetPathValue("repo_id", "repo-deploy")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	deploymentArtifacts, ok := resp["deployment_artifacts"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_artifacts type = %T, want map[string]any", resp["deployment_artifacts"])
	}
	artifacts, ok := deploymentArtifacts["deployment_artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("deployment_artifacts.deployment_artifacts = %#v, want one row", deploymentArtifacts["deployment_artifacts"])
	}
	row, ok := artifacts[0].(map[string]any)
	if !ok {
		t.Fatalf("artifact row type = %T, want map[string]any", artifacts[0])
	}
	if got, want := row["service_name"], "api"; got != want {
		t.Fatalf("artifact.service_name = %#v, want %#v", got, want)
	}
}

func TestGetRepositoryStoryIncludesDeploymentArtifactsFromDockerCompose(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-deploy", "docker-compose.yaml", "abc123", "",
					"hash-compose", int64(20), "yaml", "docker_compose",
				},
			},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-deploy", "docker-compose.yaml", "abc123", "",
					"hash-compose", int64(20), "yaml", "docker_compose",
				},
			},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-deploy", "docker-compose.yaml", "abc123", `services:
  api:
    ports:
      - "8080:8080"
    volumes:
      - ./data:/var/lib/app
`,
					"hash-compose", int64(20), "yaml", "docker_compose",
				},
			},
		},
	})

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"INSTANCE_OF": {
					"id":               "repo-deploy",
					"name":             "compose-deploy",
					"path":             "/repos/compose-deploy",
					"local_path":       "/repos/compose-deploy",
					"remote_url":       "https://github.com/acme/compose-deploy",
					"repo_slug":        "acme/compose-deploy",
					"has_remote":       true,
					"file_count":       int64(1),
					"workload_count":   int64(0),
					"platform_count":   int64(0),
					"dependency_count": int64(0),
					"languages":        []string{"yaml"},
					"workload_names":   []string{},
					"platform_types":   []string{},
				},
			},
		},
		Content: NewContentReader(db),
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-deploy/story", nil)
	req.SetPathValue("repo_id", "repo-deploy")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	deploymentOverview, ok := resp["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", resp["deployment_overview"])
	}
	deploymentArtifacts, ok := deploymentOverview["deployment_artifacts"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_artifacts type = %T, want map[string]any", deploymentOverview["deployment_artifacts"])
	}
	artifacts, ok := deploymentArtifacts["deployment_artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("deployment_artifacts.deployment_artifacts = %#v, want one row", deploymentArtifacts["deployment_artifacts"])
	}

	deliveryPaths, ok := deploymentOverview["delivery_paths"].([]any)
	if !ok || len(deliveryPaths) != 1 {
		t.Fatalf("delivery_paths = %#v, want one runtime row", deploymentOverview["delivery_paths"])
	}
	deliveryRow, ok := deliveryPaths[0].(map[string]any)
	if !ok {
		t.Fatalf("delivery_paths[0] type = %T, want map[string]any", deliveryPaths[0])
	}
	if got, want := deliveryRow["path"], "docker-compose.yaml"; got != want {
		t.Fatalf("delivery_paths[0].path = %#v, want %#v", got, want)
	}
	if got, want := deliveryRow["artifact_type"], "docker_compose"; got != want {
		t.Fatalf("delivery_paths[0].artifact_type = %#v, want %#v", got, want)
	}
	if got, want := deliveryRow["service_name"], "api"; got != want {
		t.Fatalf("delivery_paths[0].service_name = %#v, want %#v", got, want)
	}

	directStory, ok := deploymentOverview["direct_story"].([]any)
	if !ok || len(directStory) != 1 {
		t.Fatalf("direct_story = %#v, want one runtime line", deploymentOverview["direct_story"])
	}
	if got, want := directStory[0], "Runtime artifacts include docker_compose service api in docker-compose.yaml (ports, volumes)."; got != want {
		t.Fatalf("direct_story[0] = %#v, want %#v", got, want)
	}
}
