// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestBuildRepositorySemanticOverviewTracksFrameworkCounts(t *testing.T) {
	t.Parallel()

	overview := buildRepositorySemanticOverview([]querycontract.EntityContent{
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
	files := []querycontract.FileContent{
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

	frameworks := querycontract.MapSliceValue(summary, "frameworks")
	if len(frameworks) != 3 {
		t.Fatalf("len(frameworks) = %d, want 3", len(frameworks))
	}

	indexed := map[string]map[string]any{}
	for _, row := range frameworks {
		indexed[querycontract.StringVal(row, "framework")] = row
	}

	if got, want := querycontract.StringVal(indexed["react"], "confidence"), "high"; got != want {
		t.Fatalf("react confidence = %q, want %q", got, want)
	}
	if got, want := querycontract.StringSliceValue(indexed["react"], "evidence_kinds"), []string{"package_dependency", "semantic_entity"}; !slices.Equal(got, want) {
		t.Fatalf("react evidence_kinds = %#v, want %#v", got, want)
	}
	if got, want := querycontract.StringVal(indexed["express"], "confidence"), "medium"; got != want {
		t.Fatalf("express confidence = %q, want %q", got, want)
	}
	if got, want := querycontract.StringVal(indexed["hapi"], "confidence"), "medium"; got != want {
		t.Fatalf("hapi confidence = %q, want %q", got, want)
	}

	story := querycontract.StringVal(summary, "story")
	for _, want := range []string{"react", "express", "hapi"} {
		if !strings.Contains(strings.ToLower(story), want) {
			t.Fatalf("story = %q, want mention of %q", story, want)
		}
	}
}

func TestEnrichRepositoryStoryResponseWithEvidenceAddsNarrativeOverviews(t *testing.T) {
	t.Parallel()

	response := buildRepositoryStoryResponse(
		querycontract.RepoRef{
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

	files := []querycontract.FileContent{
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

	enrichRepositoryStoryResponseWithEvidence(response, querycontract.MapValue(response, "semantic_overview"), files)

	frameworkSummary := querycontract.MapValue(response, "framework_summary")
	if frameworkSummary == nil {
		t.Fatal("framework_summary missing, want repository framework evidence")
	}
	if got, want := frameworkSummary["framework_count"], 1; got != want {
		t.Fatalf("framework_summary.framework_count = %#v, want %#v", got, want)
	}

	documentationOverview := querycontract.MapValue(response, "documentation_overview")
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

	deploymentOverview := querycontract.MapValue(response, "deployment_overview")
	if deploymentOverview == nil {
		t.Fatal("deployment_overview missing, want deployment overview")
	}
	topologySummary := querycontract.StringVal(deploymentOverview, "topology_summary")
	if topologySummary == "" {
		t.Fatal("topology_summary is empty, want compact topology narrative")
	}
	if !strings.Contains(topologySummary, "docker_compose service api") {
		t.Fatalf("topology_summary = %q, want docker compose runtime evidence", topologySummary)
	}

	supportOverview := querycontract.MapValue(response, "support_overview")
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

	story := querycontract.StringVal(response, "story")
	for _, want := range []string{"Framework signals", "Documentation signals", "Runtime artifacts include docker_compose service api"} {
		if !strings.Contains(story, want) {
			t.Fatalf("story = %q, want %q", story, want)
		}
	}
}

func TestHydrateRepositoryNarrativeFilesLoadsOnlyCandidateFiles(t *testing.T) {
	t.Parallel()

	// The hydration read goes through the content-store port, so the port
	// double serves the same two files the SQL stub used to return; Dockerfile
	// has no content row under either double and stays unhydrated.
	store := querytestutil.FakePortContentStore{RepoFiles: []querycontract.FileContent{
		{RepoID: "repo-1", RelativePath: "package.json", Content: `{"dependencies":{"react":"^18.3.0"}}`},
		{RepoID: "repo-1", RelativePath: "README.md", Content: "# Sample\n"},
	}}

	files := []querycontract.FileContent{
		{RepoID: "repo-1", RelativePath: "package.json"},
		{RepoID: "repo-1", RelativePath: "README.md"},
		{RepoID: "repo-1", RelativePath: "Dockerfile"},
	}

	hydrated, err := hydrateRepositoryNarrativeFiles(context.Background(), store, "repo-1", files)
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
