package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetRepositoryContextIncludesJenkinsControllerArtifacts(t *testing.T) {
	t.Parallel()

	fixtureContent := readAnsibleJenkinsAutomationFixture(t, "Jenkinsfile")
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
					"repo-1", "Jenkinsfile", "abc123", "",
					"hash-jenkins", int64(12), "groovy", "groovy",
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
					"repo-1", "Jenkinsfile", "abc123", fixtureContent,
					"hash-jenkins", int64(12), "groovy", "groovy",
				},
			},
		},
	})

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"INSTANCE_OF": {
					"id":               "repo-1",
					"name":             "controller-service",
					"path":             "/repos/controller-service",
					"local_path":       "/repos/controller-service",
					"remote_url":       "https://github.com/org/controller-service",
					"repo_slug":        "org/controller-service",
					"has_remote":       true,
					"file_count":       int64(1),
					"workload_count":   int64(0),
					"platform_count":   int64(0),
					"dependency_count": int64(0),
				},
			},
			runByMatch: map[string][]map[string]any{},
		},
		Content: NewContentReader(db),
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
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	deploymentArtifacts, ok := resp["deployment_artifacts"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_artifacts type = %T, want map[string]any", resp["deployment_artifacts"])
	}
	controllerArtifacts, ok := deploymentArtifacts["controller_artifacts"].([]any)
	if !ok {
		t.Fatalf("controller_artifacts type = %T, want []any", deploymentArtifacts["controller_artifacts"])
	}
	if len(controllerArtifacts) != 1 {
		t.Fatalf("len(controller_artifacts) = %d, want 1", len(controllerArtifacts))
	}

	row, ok := controllerArtifacts[0].(map[string]any)
	if !ok {
		t.Fatalf("controller_artifacts[0] type = %T, want map[string]any", controllerArtifacts[0])
	}
	if got, want := row["controller_kind"], "jenkins_pipeline"; got != want {
		t.Fatalf("controller_artifacts[0].controller_kind = %#v, want %#v", got, want)
	}
	if got, want := row["path"], "Jenkinsfile"; got != want {
		t.Fatalf("controller_artifacts[0].path = %#v, want %#v", got, want)
	}
	pipelineCalls, ok := row["pipeline_calls"].([]any)
	if !ok || len(pipelineCalls) != 1 || pipelineCalls[0] != "pipelineDeploy" {
		t.Fatalf("controller_artifacts[0].pipeline_calls = %#v, want [pipelineDeploy]", row["pipeline_calls"])
	}
	sharedLibraries, ok := row["shared_libraries"].([]any)
	if !ok || len(sharedLibraries) != 1 || sharedLibraries[0] != "controller-pipelines" {
		t.Fatalf("controller_artifacts[0].shared_libraries = %#v, want [controller-pipelines]", row["shared_libraries"])
	}
	shellCommands, ok := row["shell_commands"].([]any)
	if !ok || len(shellCommands) != 1 || shellCommands[0] != "./scripts/deploy.sh" {
		t.Fatalf("controller_artifacts[0].shell_commands = %#v, want [./scripts/deploy.sh]", row["shell_commands"])
	}
}
