package query

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBuildRepositorySemanticOverviewTracksFrameworkCounts(t *testing.T) {
	t.Parallel()

	overview := buildRepositorySemanticOverview([]EntityContent{
		{
			EntityID:   "component-1",
			RepoID:     "repo-1",
			EntityType: "Component",
			EntityName: "Shell",
			Language:   "tsx",
			Metadata: map[string]any{
				"framework": "react",
			},
		},
		{
			EntityID:   "component-2",
			RepoID:     "repo-1",
			EntityType: "Component",
			EntityName: "Settings",
			Language:   "tsx",
			Metadata: map[string]any{
				"framework": "react",
			},
		},
		{
			EntityID:   "module-1",
			RepoID:     "repo-1",
			EntityType: "Module",
			EntityName: "frontend",
			Language:   "typescript",
			Metadata: map[string]any{
				"framework": "vue",
			},
		},
	})

	frameworkCounts, ok := overview["framework_counts"].(map[string]int)
	if !ok {
		t.Fatalf("framework_counts type = %T, want map[string]int", overview["framework_counts"])
	}
	if got, want := frameworkCounts["react"], 2; got != want {
		t.Fatalf("framework_counts[react] = %d, want %d", got, want)
	}
	if got, want := frameworkCounts["vue"], 1; got != want {
		t.Fatalf("framework_counts[vue] = %d, want %d", got, want)
	}
}

func TestBuildRepositoryFrameworkSummaryCombinesSemanticAndFileSignals(t *testing.T) {
	t.Parallel()

	semanticOverview := map[string]any{
		"framework_counts": map[string]int{
			"react": 2,
		},
	}
	files := []FileContent{
		{
			RelativePath: "package.json",
			Content: `{
  "dependencies": {
    "express": "^4.19.0",
    "react": "^18.3.0"
  }
}`,
		},
		{
			RelativePath: "server/app.js",
			Content: `const Hapi = require("@hapi/hapi")
module.exports = { Hapi }`,
		},
	}

	summary := buildRepositoryFrameworkSummary(semanticOverview, files)
	if summary == nil {
		t.Fatal("buildRepositoryFrameworkSummary() = nil, want summary")
	}
	if got, want := summary["framework_count"], 3; got != want {
		t.Fatalf("framework_count = %#v, want %#v", got, want)
	}

	frameworks := mapSliceValue(summary, "frameworks")
	if len(frameworks) != 3 {
		t.Fatalf("len(frameworks) = %d, want 3", len(frameworks))
	}

	indexed := map[string]map[string]any{}
	for _, row := range frameworks {
		indexed[StringVal(row, "framework")] = row
	}

	if got, want := StringVal(indexed["react"], "confidence"), "high"; got != want {
		t.Fatalf("react confidence = %q, want %q", got, want)
	}
	if got, want := stringSliceValue(indexed["react"], "evidence_kinds"), []string{"package_dependency", "semantic_entity"}; !stringSlicesEqual(got, want) {
		t.Fatalf("react evidence_kinds = %#v, want %#v", got, want)
	}
	if got, want := StringVal(indexed["express"], "confidence"), "medium"; got != want {
		t.Fatalf("express confidence = %q, want %q", got, want)
	}
	if got, want := StringVal(indexed["hapi"], "confidence"), "medium"; got != want {
		t.Fatalf("hapi confidence = %q, want %q", got, want)
	}

	story := StringVal(summary, "story")
	for _, want := range []string{"react", "express", "hapi"} {
		if !strings.Contains(strings.ToLower(story), want) {
			t.Fatalf("story = %q, want mention of %q", story, want)
		}
	}
}

func TestEnrichRepositoryStoryResponseWithEvidenceAddsNarrativeOverviews(t *testing.T) {
	t.Parallel()

	response := buildRepositoryStoryResponse(
		RepoRef{
			ID:        "repository:sample-app",
			Name:      "sample-app",
			LocalPath: "/workspace/sample-app",
			RemoteURL: "https://example.test/sample-app.git",
			RepoSlug:  "example/sample-app",
			HasRemote: true,
		},
		24,
		[]string{"javascript", "yaml"},
		[]string{"sample-app"},
		[]string{"docker_compose"},
		3,
		map[string]any{
			"families": []string{"docker", "github_actions"},
			"deployment_artifacts": map[string]any{
				"deployment_artifacts": []map[string]any{
					{
						"relative_path": "docker-compose.yaml",
						"artifact_type": "docker_compose",
						"service_name":  "api",
						"signals":       []string{"ports", "volumes"},
					},
				},
			},
		},
		map[string]any{
			"framework_counts": map[string]int{
				"react": 1,
			},
		},
	)

	files := []FileContent{
		{
			RelativePath: "README.md",
			Content:      "# Sample App\n",
		},
		{
			RelativePath: "docs/runbook.md",
			Content:      "# Runbook\n",
		},
		{
			RelativePath: "catalog-info.yaml",
			Content:      "apiVersion: backstage.io/v1alpha1\nkind: API\n",
		},
		{
			RelativePath: "package.json",
			Content: `{
  "dependencies": {
    "react": "^18.3.0"
  }
}`,
		},
		{
			RelativePath: "server/init/plugins/spec.js",
			Content: `server.route({
  method: "GET",
  path: "/_specs"
})`,
		},
		{
			RelativePath: "specs/index.yaml",
			Content: `openapi: 3.0.3
info:
  version: v1
servers:
  - url: https://sample-app.qa.example.test
paths:
  /widgets:
    get:
      operationId: listWidgets
  /_specs:
    get:
      operationId: docsIndex
`,
		},
	}

	enrichRepositoryStoryResponseWithEvidence(response, mapValue(response, "semantic_overview"), files)

	frameworkSummary := mapValue(response, "framework_summary")
	if frameworkSummary == nil {
		t.Fatal("framework_summary missing, want repository framework evidence")
	}
	if got, want := frameworkSummary["framework_count"], 1; got != want {
		t.Fatalf("framework_summary.framework_count = %#v, want %#v", got, want)
	}

	documentationOverview := mapValue(response, "documentation_overview")
	if documentationOverview == nil {
		t.Fatal("documentation_overview missing, want enriched documentation summary")
	}
	if got, want := documentationOverview["documentation_file_count"], 3; got != want {
		t.Fatalf("documentation_file_count = %#v, want %#v", got, want)
	}
	if got, want := documentationOverview["api_spec_count"], 1; got != want {
		t.Fatalf("api_spec_count = %#v, want %#v", got, want)
	}
	if got, want := documentationOverview["docs_route_count"], 1; got != want {
		t.Fatalf("docs_route_count = %#v, want %#v", got, want)
	}

	deploymentOverview := mapValue(response, "deployment_overview")
	if deploymentOverview == nil {
		t.Fatal("deployment_overview missing, want deployment overview")
	}
	topologySummary := StringVal(deploymentOverview, "topology_summary")
	if topologySummary == "" {
		t.Fatal("topology_summary is empty, want compact topology narrative")
	}
	if !strings.Contains(topologySummary, "docker_compose service api") {
		t.Fatalf("topology_summary = %q, want docker compose runtime evidence", topologySummary)
	}

	supportOverview := mapValue(response, "support_overview")
	if supportOverview == nil {
		t.Fatal("support_overview missing, want enriched support summary")
	}
	if got, want := supportOverview["framework_count"], 1; got != want {
		t.Fatalf("support_overview.framework_count = %#v, want %#v", got, want)
	}
	if got, want := supportOverview["documentation_file_count"], 3; got != want {
		t.Fatalf("support_overview.documentation_file_count = %#v, want %#v", got, want)
	}
	if got, want := supportOverview["api_spec_count"], 1; got != want {
		t.Fatalf("support_overview.api_spec_count = %#v, want %#v", got, want)
	}

	story := StringVal(response, "story")
	for _, want := range []string{"Framework signals", "Documentation signals", "Runtime artifacts include docker_compose service api"} {
		if !strings.Contains(story, want) {
			t.Fatalf("story = %q, want %q", story, want)
		}
	}
}

func TestHydrateRepositoryNarrativeFilesLoadsOnlyCandidateFiles(t *testing.T) {
	t.Parallel()

	db := openRepositoryNarrativeTestDB(t, []repositoryNarrativeQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-1", "package.json", "sha-1", `{"dependencies":{"react":"^18.3.0"}}`,
					"hash-1", int64(10), "json", "",
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
					"repo-1", "README.md", "sha-2", "# Sample\n",
					"hash-2", int64(2), "markdown", "",
				},
			},
		},
	})

	files := []FileContent{
		{RepoID: "repo-1", RelativePath: "package.json"},
		{RepoID: "repo-1", RelativePath: "README.md"},
		{RepoID: "repo-1", RelativePath: "Dockerfile"},
	}

	hydrated, err := hydrateRepositoryNarrativeFiles(context.Background(), NewContentReader(db), "repo-1", files)
	if err != nil {
		t.Fatalf("hydrateRepositoryNarrativeFiles() error = %v, want nil", err)
	}
	if len(hydrated) != 2 {
		t.Fatalf("len(hydrated) = %d, want 2", len(hydrated))
	}
	if got, want := hydrated[0].RelativePath, "README.md"; got != want {
		t.Fatalf("hydrated[0].RelativePath = %q, want %q", got, want)
	}
	if got, want := hydrated[1].RelativePath, "package.json"; got != want {
		t.Fatalf("hydrated[1].RelativePath = %q, want %q", got, want)
	}
}

func stringSlicesEqual(got []string, want []string) bool {
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

type repositoryNarrativeQueryResult struct {
	columns []string
	rows    [][]driver.Value
	err     error
}

func openRepositoryNarrativeTestDB(t *testing.T, results []repositoryNarrativeQueryResult) *sql.DB {
	t.Helper()

	name := fmt.Sprintf("repository-narrative-test-%d", atomic.AddUint64(&repositoryNarrativeDriverSeq, 1))
	sql.Register(name, &repositoryNarrativeDriver{results: results})

	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

var repositoryNarrativeDriverSeq uint64

type repositoryNarrativeDriver struct {
	results []repositoryNarrativeQueryResult
}

func (d *repositoryNarrativeDriver) Open(string) (driver.Conn, error) {
	return &repositoryNarrativeConn{results: append([]repositoryNarrativeQueryResult(nil), d.results...)}, nil
}

type repositoryNarrativeConn struct {
	results []repositoryNarrativeQueryResult
}

func (c *repositoryNarrativeConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("Prepare not implemented")
}

func (c *repositoryNarrativeConn) Close() error {
	return nil
}

func (c *repositoryNarrativeConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("Begin not implemented")
}

func (c *repositoryNarrativeConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	if len(c.results) == 0 {
		return nil, fmt.Errorf("unexpected query")
	}
	result := c.results[0]
	c.results = c.results[1:]
	if result.err != nil {
		return nil, result.err
	}
	return &repositoryNarrativeRows{columns: result.columns, rows: result.rows}, nil
}

type repositoryNarrativeRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *repositoryNarrativeRows) Columns() []string {
	return r.columns
}

func (r *repositoryNarrativeRows) Close() error {
	return nil
}

func (r *repositoryNarrativeRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}
