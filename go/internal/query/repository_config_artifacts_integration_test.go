package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetRepositoryStoryIncludesSharedConfigPathsFromRelatedRepos(t *testing.T) {
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
			rows: [][]driver.Value{},
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
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-helm", "deploy/kustomization.yaml", "abc123", "",
					"hash-k1", int64(5), "yaml", "yaml",
				},
				{
					"repo-helm", "deploy/policy.yaml", "abc123", "",
					"hash-k2", int64(20), "yaml", "yaml",
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
					"repo-helm", "deploy/kustomization.yaml", "abc123", `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - policy.yaml
`,
					"hash-k1", int64(5), "yaml", "yaml",
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
					"repo-helm", "deploy/policy.yaml", "abc123", `apiVersion: iam.aws.upbound.io/v1beta1
kind: RolePolicy
spec:
  policyDocument:
    Statement:
      - Resource:
          - arn:aws:ssm:us-east-1:123456789012:parameter/configd/payments/*
`,
					"hash-k2", int64(20), "yaml", "yaml",
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
					"repo-terraform", "deploy/kustomization.yaml", "def456", "",
					"hash-k3", int64(5), "yaml", "yaml",
				},
				{
					"repo-terraform", "deploy/policy.yaml", "def456", "",
					"hash-k4", int64(20), "yaml", "yaml",
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
					"repo-terraform", "deploy/kustomization.yaml", "def456", `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - policy.yaml
`,
					"hash-k3", int64(5), "yaml", "yaml",
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
					"repo-terraform", "deploy/policy.yaml", "def456", `apiVersion: iam.aws.upbound.io/v1beta1
kind: RolePolicy
spec:
  policyDocument:
    Statement:
      - Resource:
          - /configd/payments/*
`,
					"hash-k4", int64(20), "yaml", "yaml",
				},
			},
		},
	})

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"INSTANCE_OF": {
					"id":               "repo-app",
					"name":             "payments-api",
					"path":             "/repos/payments-api",
					"local_path":       "/repos/payments-api",
					"remote_url":       "https://github.com/acme/payments-api",
					"repo_slug":        "acme/payments-api",
					"has_remote":       true,
					"file_count":       int64(1),
					"workload_count":   int64(1),
					"platform_count":   int64(1),
					"dependency_count": int64(2),
					"languages":        []string{"go"},
					"workload_names":   []string{"payments-api"},
					"platform_types":   []string{"argocd_application"},
				},
			},
			runByMatch: map[string][]map[string]any{
				"MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON]->(related:Repository)": {
					{"repo_id": "repo-helm", "repo_name": "helm-charts"},
					{"repo_id": "repo-terraform", "repo_name": "terraform-stack-payments"},
				},
			},
		},
		Content: NewContentReader(db),
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-app/story", nil)
	req.SetPathValue("repo_id", "repo-app")
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
	sharedConfigPaths, ok := deploymentOverview["shared_config_paths"].([]any)
	if !ok || len(sharedConfigPaths) != 1 {
		t.Fatalf("shared_config_paths = %#v, want one grouped row", deploymentOverview["shared_config_paths"])
	}

	row, ok := sharedConfigPaths[0].(map[string]any)
	if !ok {
		t.Fatalf("shared_config_paths[0] type = %T, want map[string]any", sharedConfigPaths[0])
	}
	if got, want := row["path"], "/configd/payments/*"; got != want {
		t.Fatalf("shared_config_paths[0].path = %#v, want %#v", got, want)
	}
	sourceRepos, ok := row["source_repositories"].([]any)
	if !ok || len(sourceRepos) != 2 {
		t.Fatalf("source_repositories = %#v, want two repos", row["source_repositories"])
	}

	topologyStory, ok := deploymentOverview["topology_story"].([]any)
	if !ok || len(topologyStory) != 3 {
		t.Fatalf("topology_story = %#v, want config provenance plus shared-config summary", deploymentOverview["topology_story"])
	}
	if got, want := topologyStory[0], "Config provenance includes /configd/payments/* from helm-charts via kustomize_policy_document_resource in deploy/policy.yaml."; got != want {
		t.Fatalf("topology_story[0] = %#v, want %#v", got, want)
	}
	if got, want := topologyStory[1], "Config provenance includes /configd/payments/* from terraform-stack-payments via kustomize_policy_document_resource in deploy/policy.yaml."; got != want {
		t.Fatalf("topology_story[1] = %#v, want %#v", got, want)
	}
	if got, want := topologyStory[2], "Shared config families span /configd/payments/* across helm-charts, terraform-stack-payments."; got != want {
		t.Fatalf("topology_story[0] = %#v, want %#v", got, want)
	}
}
