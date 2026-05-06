package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeRepoGraphReader dispatches Neo4j queries to per-query stubs based on
// Cypher content matching. This lets each test control the data for every
// query in getRepositoryContext independently.
type fakeRepoGraphReader struct {
	// runSingleByMatch maps a Cypher fragment to the result row.
	runSingleByMatch map[string]map[string]any
	// runByMatch maps a Cypher fragment to multiple result rows.
	runByMatch map[string][]map[string]any
	run        func(context.Context, string, map[string]any) ([]map[string]any, error)
	runSingle  func(context.Context, string, map[string]any) (map[string]any, error)
}

func (f fakeRepoGraphReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
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

func (f fakeRepoGraphReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
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
	if bestRow == nil && strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") && len(f.runSingleByMatch) == 1 {
		for _, row := range f.runSingleByMatch {
			return row, nil
		}
	}
	return bestRow, nil
}

func TestGetRepositoryContextUsesNarrowRepositoryLookupAndLogsStages(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	var runSingleCyphers []string
	reader := fakeRepoGraphReader{
		runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
			runSingleCyphers = append(runSingleCyphers, cypher)
			if got, want := params["repo_id"], "repo-observed"; got != want {
				t.Fatalf("repo_id param = %#v, want %#v", got, want)
			}
			return map[string]any{
				"id":         "repo-observed",
				"name":       "observed-service",
				"path":       "/repos/observed-service",
				"local_path": "/repos/observed-service",
				"has_remote": false,
			}, nil
		},
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if got, want := params["repo_id"], "repo-observed"; got != want {
				t.Fatalf("repo_id param = %#v, want %#v", got, want)
			}
			switch {
			case strings.Contains(cypher, "RETURN count(DISTINCT f) AS count"):
				return []map[string]any{{"count": int64(2)}}, nil
			case strings.Contains(cypher, "RETURN count(DISTINCT w) AS count"):
				return []map[string]any{{"count": int64(1)}}, nil
			case strings.Contains(cypher, "RETURN count(DISTINCT p) AS count"):
				return []map[string]any{{"count": int64(1)}}, nil
			case strings.Contains(cypher, "RETURN count(DISTINCT dep) AS count"):
				return []map[string]any{{"count": int64(0)}}, nil
			default:
				return nil, nil
			}
		},
	}
	handler := &RepositoryHandler{
		Neo4j:  reader,
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-observed/context", nil)
	req.SetPathValue("repo_id", "repo-observed")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if len(runSingleCyphers) != 1 {
		t.Fatalf("RunSingle calls = %d, want 1", len(runSingleCyphers))
	}
	baseLookup := runSingleCyphers[0]
	if !strings.Contains(baseLookup, "MATCH (r:Repository {id: $repo_id})") {
		t.Fatalf("base lookup = %s, want repository-id anchor", baseLookup)
	}
	if strings.Contains(baseLookup, "OPTIONAL MATCH") || strings.Contains(baseLookup, "INSTANCE_OF") {
		t.Fatalf("base lookup still uses broad aggregation shape:\n%s", baseLookup)
	}

	logText := logs.String()
	for _, want := range []string{
		`"event_name":"repository_query.stage_started"`,
		`"event_name":"repository_query.stage_completed"`,
		`"operation":"repository_context"`,
		`"stage":"repository_lookup"`,
		`"stage":"summary_counts"`,
		`"duration_seconds"`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s; logs = %s", want, logText)
		}
	}
}

func TestGetRepositoryStoryUsesNarrowRepositoryLookupAndLogsStages(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	var runSingleCyphers []string
	reader := fakeRepoGraphReader{
		runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
			runSingleCyphers = append(runSingleCyphers, cypher)
			if got, want := params["repo_id"], "repo-story"; got != want {
				t.Fatalf("repo_id param = %#v, want %#v", got, want)
			}
			return map[string]any{
				"id":         "repo-story",
				"name":       "story-service",
				"path":       "/repos/story-service",
				"local_path": "/repos/story-service",
				"has_remote": false,
			}, nil
		},
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if got, want := params["repo_id"], "repo-story"; got != want {
				t.Fatalf("repo_id param = %#v, want %#v", got, want)
			}
			switch {
			case strings.Contains(cypher, "RETURN count(DISTINCT f) AS count"):
				return []map[string]any{{"count": int64(7)}}, nil
			case strings.Contains(cypher, "RETURN f.language AS language, count(DISTINCT f) AS file_count"):
				return []map[string]any{{"language": "go", "file_count": int64(7)}}, nil
			case strings.Contains(cypher, "RETURN w.name AS workload_name"):
				return []map[string]any{{"workload_name": "story-service"}}, nil
			case strings.Contains(cypher, "RETURN p.type AS platform_type"):
				return []map[string]any{{"platform_type": "ecs"}}, nil
			case strings.Contains(cypher, "RETURN count(DISTINCT dep) AS count"):
				return []map[string]any{{"count": int64(1)}}, nil
			default:
				return nil, nil
			}
		},
	}
	handler := &RepositoryHandler{
		Neo4j:  reader,
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-story/story", nil)
	req.SetPathValue("repo_id", "repo-story")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if len(runSingleCyphers) != 1 {
		t.Fatalf("RunSingle calls = %d, want 1", len(runSingleCyphers))
	}
	baseLookup := runSingleCyphers[0]
	if !strings.Contains(baseLookup, "MATCH (r:Repository {id: $repo_id})") {
		t.Fatalf("base lookup = %s, want repository-id anchor", baseLookup)
	}
	if strings.Contains(baseLookup, "OPTIONAL MATCH") || strings.Contains(baseLookup, "INSTANCE_OF") {
		t.Fatalf("base lookup still uses broad aggregation shape:\n%s", baseLookup)
	}

	logText := logs.String()
	for _, want := range []string{
		`"event_name":"repository_query.stage_started"`,
		`"event_name":"repository_query.stage_completed"`,
		`"operation":"repository_story"`,
		`"stage":"repository_lookup"`,
		`"stage":"graph_summary"`,
		`"duration_seconds"`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s; logs = %s", want, logText)
		}
	}
}

func TestGetRepositoryContextUsesContentCoverageForFileCountsAndLanguages(t *testing.T) {
	t.Parallel()

	reader := fakeRepoGraphReader{
		runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				t.Fatalf("RunSingle cypher = %q, want repository base lookup", cypher)
			}
			return map[string]any{
				"id":         "repo-coverage",
				"name":       "coverage-service",
				"path":       "/repos/coverage-service",
				"local_path": "/repos/coverage-service",
				"has_remote": false,
			}, nil
		},
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			for _, forbidden := range []string{
				"RETURN count(DISTINCT f) AS count",
				"RETURN count(DISTINCT w) AS count",
				"RETURN count(DISTINCT p) AS count",
				"RETURN count(DISTINCT dep) AS count",
				"f.language IS NOT NULL",
			} {
				if strings.Contains(cypher, forbidden) {
					t.Fatalf("cypher = %q, want read-model summary instead of graph summary fanout", cypher)
				}
			}
			return nil, nil
		},
	}
	handler := &RepositoryHandler{
		Neo4j: reader,
		Content: fakePortContentStore{
			coverage: RepositoryContentCoverage{
				Available: true,
				FileCount: 456,
				Languages: []RepositoryLanguageCount{
					{Language: "go", FileCount: 300},
					{Language: "yaml", FileCount: 156},
				},
			},
			summary: repositoryReadModelSummary{
				Available:       true,
				WorkloadNames:   []string{"coverage-service"},
				PlatformCount:   1,
				DependencyCount: 2,
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-coverage/context", nil)
	req.SetPathValue("repo_id", "repo-coverage")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp["file_count"], float64(456); got != want {
		t.Fatalf("file_count = %#v, want %#v", got, want)
	}
	if got, want := resp["workload_count"], float64(1); got != want {
		t.Fatalf("workload_count = %#v, want %#v", got, want)
	}
	if got, want := resp["platform_count"], float64(1); got != want {
		t.Fatalf("platform_count = %#v, want %#v", got, want)
	}
	if got, want := resp["dependency_count"], float64(2); got != want {
		t.Fatalf("dependency_count = %#v, want %#v", got, want)
	}
	languages, ok := resp["languages"].([]any)
	if !ok || len(languages) != 2 {
		t.Fatalf("languages = %#v, want two content coverage rows", resp["languages"])
	}
	first, ok := languages[0].(map[string]any)
	if !ok {
		t.Fatalf("languages[0] = %#v, want map", languages[0])
	}
	if got, want := first["language"], "go"; got != want {
		t.Fatalf("languages[0].language = %#v, want %#v", got, want)
	}
}

func TestGetRepositoryStoryUsesContentCoverageForFileCountsAndLanguages(t *testing.T) {
	t.Parallel()

	reader := fakeRepoGraphReader{
		runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				t.Fatalf("RunSingle cypher = %q, want repository base lookup", cypher)
			}
			return map[string]any{
				"id":         "repo-story-coverage",
				"name":       "story-coverage-service",
				"path":       "/repos/story-coverage-service",
				"local_path": "/repos/story-coverage-service",
				"has_remote": false,
			}, nil
		},
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			for _, forbidden := range []string{
				"RETURN count(DISTINCT f) AS count",
				"RETURN f.language AS language",
				"RETURN w.name AS workload_name",
				"RETURN p.type AS platform_type",
				"RETURN count(DISTINCT dep) AS count",
			} {
				if strings.Contains(cypher, forbidden) {
					t.Fatalf("cypher = %q, want read-model summary instead of graph summary fanout", cypher)
				}
			}
			return nil, nil
		},
	}
	handler := &RepositoryHandler{
		Neo4j: reader,
		Content: fakePortContentStore{
			coverage: RepositoryContentCoverage{
				Available: true,
				FileCount: 789,
				Languages: []RepositoryLanguageCount{
					{Language: "typescript", FileCount: 500},
					{Language: "yaml", FileCount: 289},
				},
			},
			summary: repositoryReadModelSummary{
				Available:       true,
				WorkloadNames:   []string{"story-coverage-service"},
				PlatformTypes:   []string{"ecs"},
				PlatformCount:   1,
				DependencyCount: 2,
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-story-coverage/story", nil)
	req.SetPathValue("repo_id", "repo-story-coverage")
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
	for _, want := range []string{
		"Repository story-coverage-service contains 789 indexed files.",
		"Languages: typescript, yaml.",
		"Defines 1 workload(s): story-coverage-service.",
		"Runs on platform signal(s): ecs.",
	} {
		if !strings.Contains(story, want) {
			t.Fatalf("story = %q, want fragment %q", story, want)
		}
	}
}

func TestGetRepositoryContextReturnsEnrichedResponse(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				// Base repo query — must return repo + counts
				"INSTANCE_OF": {
					"id":               "repo-1",
					"name":             "order-service",
					"path":             "/repos/order-service",
					"local_path":       "/repos/order-service",
					"remote_url":       "https://github.com/org/order-service",
					"repo_slug":        "org/order-service",
					"has_remote":       true,
					"file_count":       int64(120),
					"workload_count":   int64(2),
					"platform_count":   int64(1),
					"dependency_count": int64(5),
				},
			},
			runByMatch: map[string][]map[string]any{
				// Entry points query
				"fn.name IN": {
					{
						"name":          "main",
						"relative_path": "cmd/server/main.go",
						"language":      "go",
					},
					{
						"name":          "handler",
						"relative_path": "internal/api/handler.go",
						"language":      "go",
					},
				},
				// Infrastructure entities query
				"K8sResource OR": {
					{
						"type":      "K8sResource",
						"name":      "order-deployment",
						"kind":      "Deployment",
						"file_path": "k8s/deployment.yaml",
					},
				},
				// Language distribution query
				"f.language IS NOT NULL": {
					{
						"language":   "go",
						"file_count": int64(80),
					},
					{
						"language":   "yaml",
						"file_count": int64(30),
					},
					{
						"language":   "dockerfile",
						"file_count": int64(10),
					},
				},
				// Cross-repo relationships (outgoing)
				"MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON]->(target:Repository)": {
					{
						"type":          "DEPENDS_ON",
						"target_name":   "auth-service",
						"target_id":     "repo-2",
						"evidence_type": "terraform_module_source",
					},
					{
						"type":          "USES_MODULE",
						"target_name":   "terraform-modules-shared",
						"target_id":     "repo-6",
						"evidence_type": "terraform_module_source",
					},
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "infra-configs",
						"target_id":     "repo-3",
						"evidence_type": "argocd_source",
					},
					{
						"type":          "PROVISIONS_DEPENDENCY_FOR",
						"target_name":   "platform-infra",
						"target_id":     "repo-5",
						"evidence_type": "terraform_source",
					},
					{
						"type":          "RUNS_ON",
						"target_name":   "eks-platform",
						"target_id":     "repo-7",
						"evidence_type": "terraform_runtime_family",
					},
				},
				// Consumer repositories (incoming)
				"MATCH (consumer:Repository)-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON]->(r:Repository {id: $repo_id})": {
					{
						"consumer_name": "checkout-service",
						"consumer_id":   "repo-4",
					},
				},
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

	// Verify base repository ref
	repo, ok := resp["repository"].(map[string]any)
	if !ok {
		t.Fatalf("resp[repository] type = %T, want map", resp["repository"])
	}
	if got, want := repo["id"], "repo-1"; got != want {
		t.Fatalf("repository.id = %v, want %v", got, want)
	}

	// Verify counts preserved
	if got, want := resp["file_count"], float64(120); got != want {
		t.Fatalf("file_count = %v, want %v", got, want)
	}
	if got, want := resp["workload_count"], float64(2); got != want {
		t.Fatalf("workload_count = %v, want %v", got, want)
	}
	if got, want := resp["dependency_count"], float64(5); got != want {
		t.Fatalf("dependency_count = %v, want %v", got, want)
	}

	// Verify entry points
	entryPoints, ok := resp["entry_points"].([]any)
	if !ok {
		t.Fatalf("entry_points type = %T, want []any", resp["entry_points"])
	}
	if len(entryPoints) != 2 {
		t.Fatalf("len(entry_points) = %d, want 2", len(entryPoints))
	}
	ep0, ok := entryPoints[0].(map[string]any)
	if !ok {
		t.Fatalf("entry_points[0] type = %T", entryPoints[0])
	}
	if got, want := ep0["name"], "main"; got != want {
		t.Fatalf("entry_points[0].name = %v, want %v", got, want)
	}

	// Verify infrastructure entities
	infraEntities, ok := resp["infrastructure"].([]any)
	if !ok {
		t.Fatalf("infrastructure type = %T, want []any", resp["infrastructure"])
	}
	if len(infraEntities) != 1 {
		t.Fatalf("len(infrastructure) = %d, want 1", len(infraEntities))
	}
	infra0, ok := infraEntities[0].(map[string]any)
	if !ok {
		t.Fatalf("infrastructure[0] type = %T", infraEntities[0])
	}
	if got, want := infra0["type"], "K8sResource"; got != want {
		t.Fatalf("infrastructure[0].type = %v, want %v", got, want)
	}

	// Verify language distribution
	languages, ok := resp["languages"].([]any)
	if !ok {
		t.Fatalf("languages type = %T, want []any", resp["languages"])
	}
	if len(languages) != 3 {
		t.Fatalf("len(languages) = %d, want 3", len(languages))
	}

	// Verify cross-repo relationships
	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("relationships type = %T, want []any", resp["relationships"])
	}
	if len(relationships) != 5 {
		t.Fatalf("len(relationships) = %d, want 5", len(relationships))
	}
	rel0, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("relationships[0] type = %T", relationships[0])
	}
	if got, want := rel0["type"], "DEPENDS_ON"; got != want {
		t.Fatalf("relationships[0].type = %v, want %v", got, want)
	}
	if got, want := rel0["target_name"], "auth-service"; got != want {
		t.Fatalf("relationships[0].target_name = %v, want %v", got, want)
	}

	relTypes := make(map[string]struct{}, len(relationships))
	for _, rel := range relationships {
		row, ok := rel.(map[string]any)
		if !ok {
			t.Fatalf("relationship type = %T, want map[string]any", rel)
		}
		relTypes[row["type"].(string)] = struct{}{}
	}
	for _, want := range []string{"DEPENDS_ON", "USES_MODULE", "DEPLOYS_FROM", "PROVISIONS_DEPENDENCY_FOR", "RUNS_ON"} {
		if _, ok := relTypes[want]; !ok {
			t.Fatalf("missing relationship type %q", want)
		}
	}

	// Verify consumer repositories
	consumers, ok := resp["consumers"].([]any)
	if !ok {
		t.Fatalf("consumers type = %T, want []any", resp["consumers"])
	}
	if len(consumers) != 1 {
		t.Fatalf("len(consumers) = %d, want 1", len(consumers))
	}
	con0, ok := consumers[0].(map[string]any)
	if !ok {
		t.Fatalf("consumers[0] type = %T", consumers[0])
	}
	if got, want := con0["name"], "checkout-service"; got != want {
		t.Fatalf("consumers[0].name = %v, want %v", got, want)
	}
}

func TestGetRepositoryContextIncludesTerraformAndTerragruntInfrastructureFromContent(t *testing.T) {
	t.Parallel()

	fixtureContent := readAnsibleJenkinsAutomationFixture(t, "Jenkinsfile")
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"tf-module-1", "repo-1", "infra/main.tf", "TerraformModule", "eks",
					int64(1), int64(8), "hcl", "module \"eks\" {}", []byte(`{"source":"tfr:///terraform-aws-modules/eks/aws?version=19.0.0","deployment_name":"comprehensive-cluster"}`),
				},
				{
					"tg-config-1", "repo-1", "infra/terragrunt.hcl", "TerragruntConfig", "terragrunt",
					int64(1), int64(12), "hcl", "terraform { source = \"../modules/app\" }", []byte(`{"terraform_source":"../modules/app","includes":"root","inputs":"image_tag"}`),
				},
				{
					"tg-dep-1", "repo-1", "infra/terragrunt.hcl", "TerragruntDependency", "vpc",
					int64(5), int64(8), "hcl", "dependency \"vpc\" {}", []byte(`{"config_path":"../vpc"}`),
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
					"repo-1", "Jenkinsfile", "abc123", "",
					"hash-jenkins", int64(12), "groovy", "groovy",
				},
				{
					"repo-1", "Dockerfile", "abc123", "",
					"hash-docker", int64(30), "dockerfile", "dockerfile",
				},
				{
					"repo-1", ".github/workflows/deploy.yaml", "abc123", "name: deploy\non:\n  push:\n",
					"hash-gha", int64(40), "yaml", "github_actions_workflow",
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
					"name":             "infra-runtime",
					"path":             "/repos/infra-runtime",
					"local_path":       "/repos/infra-runtime",
					"remote_url":       "https://github.com/acme/infra-runtime.git",
					"repo_slug":        "acme/infra-runtime",
					"has_remote":       true,
					"file_count":       int64(12),
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
		t.Fatalf("json.Unmarshal: %v", err)
	}

	infraEntities, ok := resp["infrastructure"].([]any)
	if !ok {
		t.Fatalf("infrastructure type = %T, want []any", resp["infrastructure"])
	}
	if len(infraEntities) != 3 {
		t.Fatalf("len(infrastructure) = %d, want 3", len(infraEntities))
	}

	types := make(map[string]map[string]any, len(infraEntities))
	for _, item := range infraEntities {
		infra, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("infrastructure item type = %T, want map[string]any", item)
		}
		types[infra["type"].(string)] = infra
	}

	tfModule, ok := types["TerraformModule"]
	if !ok {
		t.Fatal("missing TerraformModule infrastructure entry")
	}
	if got, want := tfModule["source"], "tfr:///terraform-aws-modules/eks/aws?version=19.0.0"; got != want {
		t.Fatalf("TerraformModule.source = %#v, want %#v", got, want)
	}

	tgConfig, ok := types["TerragruntConfig"]
	if !ok {
		t.Fatal("missing TerragruntConfig infrastructure entry")
	}
	if got, want := tgConfig["terraform_source"], "../modules/app"; got != want {
		t.Fatalf("TerragruntConfig.terraform_source = %#v, want %#v", got, want)
	}

	tgDep, ok := types["TerragruntDependency"]
	if !ok {
		t.Fatal("missing TerragruntDependency infrastructure entry")
	}
	if got, want := tgDep["config_path"], "../vpc"; got != want {
		t.Fatalf("TerragruntDependency.config_path = %#v, want %#v", got, want)
	}

	overview, ok := resp["infrastructure_overview"].(map[string]any)
	if !ok {
		t.Fatalf("infrastructure_overview type = %T, want map[string]any", resp["infrastructure_overview"])
	}
	artifactCounts, ok := overview["artifact_family_counts"].(map[string]any)
	if !ok {
		t.Fatalf("artifact_family_counts type = %T, want map[string]any", overview["artifact_family_counts"])
	}
	if got, want := artifactCounts["docker"], float64(1); got != want {
		t.Fatalf("artifact_family_counts[docker] = %#v, want %#v", got, want)
	}
	if got, want := artifactCounts["github_actions"], float64(1); got != want {
		t.Fatalf("artifact_family_counts[github_actions] = %#v, want %#v", got, want)
	}

	deploymentArtifacts, ok := overview["deployment_artifacts"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_artifacts type = %T, want map[string]any", overview["deployment_artifacts"])
	}
	workflowArtifacts, ok := deploymentArtifacts["workflow_artifacts"].([]any)
	if !ok {
		t.Fatalf("workflow_artifacts type = %T, want []any", deploymentArtifacts["workflow_artifacts"])
	}
	if len(workflowArtifacts) != 1 {
		t.Fatalf("len(workflow_artifacts) = %d, want 1", len(workflowArtifacts))
	}
	workflowArtifact, ok := workflowArtifacts[0].(map[string]any)
	if !ok {
		t.Fatalf("workflow_artifacts[0] type = %T, want map[string]any", workflowArtifacts[0])
	}
	if got, want := workflowArtifact["relative_path"], ".github/workflows/deploy.yaml"; got != want {
		t.Fatalf("workflow_artifacts[0].relative_path = %#v, want %#v", got, want)
	}
	if got, want := workflowArtifact["workflow_name"], "deploy"; got != want {
		t.Fatalf("workflow_artifacts[0].workflow_name = %#v, want %#v", got, want)
	}

	controllerArtifacts, ok := deploymentArtifacts["controller_artifacts"].([]any)
	if !ok {
		t.Fatalf("controller_artifacts type = %T, want []any", deploymentArtifacts["controller_artifacts"])
	}
	if len(controllerArtifacts) != 1 {
		t.Fatalf("len(controller_artifacts) = %d, want 1", len(controllerArtifacts))
	}
	controllerArtifact, ok := controllerArtifacts[0].(map[string]any)
	if !ok {
		t.Fatalf("controller_artifacts[0] type = %T, want map[string]any", controllerArtifacts[0])
	}
	if got, want := controllerArtifact["path"], "Jenkinsfile"; got != want {
		t.Fatalf("controller_artifacts[0].path = %#v, want %#v", got, want)
	}
	if got, want := controllerArtifact["controller_kind"], "jenkins_pipeline"; got != want {
		t.Fatalf("controller_artifacts[0].controller_kind = %#v, want %#v", got, want)
	}
	pipelineCalls, ok := controllerArtifact["pipeline_calls"].([]any)
	if !ok || len(pipelineCalls) != 1 || pipelineCalls[0] != "pipelineDeploy" {
		t.Fatalf("controller_artifacts[0].pipeline_calls = %#v, want [pipelineDeploy]", controllerArtifact["pipeline_calls"])
	}
	shellCommands, ok := controllerArtifact["shell_commands"].([]any)
	if !ok || len(shellCommands) != 1 || shellCommands[0] != "./scripts/deploy.sh" {
		t.Fatalf("controller_artifacts[0].shell_commands = %#v, want [./scripts/deploy.sh]", controllerArtifact["shell_commands"])
	}

}

func TestGetRepositoryContextReturnsNotFoundForMissingRepo(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{},
			runByMatch:       map[string][]map[string]any{},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/unknown/context", nil)
	req.SetPathValue("repo_id", "unknown")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestGetRepositoryContextPartitionsControllerWorkflowAndIaCRelationships(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"INSTANCE_OF": {
					"id":               "repo-2",
					"name":             "platform-service",
					"path":             "/repos/platform-service",
					"local_path":       "/repos/platform-service",
					"remote_url":       "https://github.com/acme/platform-service",
					"repo_slug":        "acme/platform-service",
					"has_remote":       true,
					"file_count":       int64(18),
					"workload_count":   int64(1),
					"platform_count":   int64(1),
					"dependency_count": int64(5),
				},
			},
			runByMatch: map[string][]map[string]any{
				"MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON]->(target:Repository)": {
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "infra-configs",
						"target_id":     "repo-3",
						"evidence_type": "argocd_application_source",
					},
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "ci-workflows",
						"target_id":     "repo-4",
						"evidence_type": "github_actions_reusable_workflow_ref",
					},
					{
						"type":          "DISCOVERS_CONFIG_IN",
						"target_name":   "controller-pipelines",
						"target_id":     "repo-5",
						"evidence_type": "jenkins_shared_library",
					},
					{
						"type":          "DEPENDS_ON",
						"target_name":   "ansible-ops",
						"target_id":     "repo-6",
						"evidence_type": "ansible_role_reference",
					},
					{
						"type":          "DEPENDS_ON",
						"target_name":   "terraform-modules",
						"target_id":     "repo-7",
						"evidence_type": "terraform_module_source",
					},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-2/context", nil)
	req.SetPathValue("repo_id", "repo-2")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	overview, ok := resp["relationship_overview"].(map[string]any)
	if !ok {
		t.Fatalf("relationship_overview type = %T, want map[string]any", resp["relationship_overview"])
	}
	if got, want := overview["relationship_count"], float64(5); got != want {
		t.Fatalf("relationship_overview.relationship_count = %#v, want %#v", got, want)
	}

	controllerDriven, ok := overview["controller_driven"].([]any)
	if !ok {
		t.Fatalf("controller_driven type = %T, want []any", overview["controller_driven"])
	}
	if len(controllerDriven) != 3 {
		t.Fatalf("len(controller_driven) = %d, want 3", len(controllerDriven))
	}
	controllerEvidence := map[string]struct{}{}
	for _, item := range controllerDriven {
		row, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("controller_driven item type = %T, want map[string]any", item)
		}
		controllerEvidence[StringVal(row, "evidence_type")] = struct{}{}
	}
	for _, want := range []string{"argocd_application_source", "jenkins_shared_library", "ansible_role_reference"} {
		if _, ok := controllerEvidence[want]; !ok {
			t.Fatalf("controller_driven missing evidence_type %q", want)
		}
	}

	workflowDriven, ok := overview["workflow_driven"].([]any)
	if !ok {
		t.Fatalf("workflow_driven type = %T, want []any", overview["workflow_driven"])
	}
	if len(workflowDriven) != 1 {
		t.Fatalf("len(workflow_driven) = %d, want 1", len(workflowDriven))
	}
	workflowRow, ok := workflowDriven[0].(map[string]any)
	if !ok {
		t.Fatalf("workflow_driven[0] type = %T, want map[string]any", workflowDriven[0])
	}
	if got, want := workflowRow["evidence_type"], "github_actions_reusable_workflow_ref"; got != want {
		t.Fatalf("workflow_driven[0].evidence_type = %#v, want %#v", got, want)
	}

	iacDriven, ok := overview["iac_driven"].([]any)
	if !ok {
		t.Fatalf("iac_driven type = %T, want []any", overview["iac_driven"])
	}
	if len(iacDriven) != 1 {
		t.Fatalf("len(iac_driven) = %d, want 1", len(iacDriven))
	}
	iacRow, ok := iacDriven[0].(map[string]any)
	if !ok {
		t.Fatalf("iac_driven[0] type = %T, want map[string]any", iacDriven[0])
	}
	if got, want := iacRow["evidence_type"], "terraform_module_source"; got != want {
		t.Fatalf("iac_driven[0].evidence_type = %#v, want %#v", got, want)
	}

	story, ok := overview["story"].(string)
	if !ok {
		t.Fatalf("story type = %T, want string", overview["story"])
	}
	lowerStory := strings.ToLower(story)
	for _, want := range []string{"controller-driven", "workflow-driven", "iac-driven", "argocd_application_source", "github_actions_reusable_workflow_ref"} {
		if !strings.Contains(lowerStory, strings.ToLower(want)) {
			t.Fatalf("story = %q, want evidence lane %q", story, want)
		}
	}
}

func TestGetRepositoryContextHandlesEmptyEnrichmentGracefully(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"count(DISTINCT f) as file_count": {
					"id":               "repo-empty",
					"name":             "empty-repo",
					"path":             "/repos/empty-repo",
					"local_path":       "/repos/empty-repo",
					"has_remote":       false,
					"file_count":       int64(0),
					"workload_count":   int64(0),
					"platform_count":   int64(0),
					"dependency_count": int64(0),
				},
			},
			runByMatch: map[string][]map[string]any{},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-empty/context", nil)
	req.SetPathValue("repo_id", "repo-empty")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// All enrichment sections should be empty slices, not nil
	for _, key := range []string{"entry_points", "infrastructure", "languages", "relationships", "consumers"} {
		val, ok := resp[key].([]any)
		if !ok {
			t.Fatalf("resp[%s] type = %T, want []any", key, resp[key])
		}
		if len(val) != 0 {
			t.Fatalf("len(resp[%s]) = %d, want 0", key, len(val))
		}
	}
}
