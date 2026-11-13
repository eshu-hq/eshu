// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"encoding/json"
	"strings"
	"testing"
)

// prose is a wide evidence string: bodies, rationales, and payloads at this
// scale are what push a populated repository story over the dispatch budget.
const repoStorySizeProse = "a wide evidence body describing how this row was established, " +
	"with enough detail to stand in for production prose, matched values, and payloads. "

// wideRow returns a map row whose dropped keys carry production-scale prose.
func repoStorySizeWideRow(identity map[string]any, dropped int) map[string]any {
	row := map[string]any{}
	for k, v := range identity {
		row[k] = v
	}
	for i := range dropped {
		row["dropped_prose_"+string(rune('a'+i%26))+string(rune('0'+i%10))] = strings.Repeat(repoStorySizeProse, 3)
	}
	return row
}

// repoStorySizeStrings returns n short identity strings at the documented
// string-row cap scale (repositoryStoryStringRowLimit = 500).
func repoStorySizeStrings(prefix string, n int) []string {
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, prefix+string(rune('a'+i%26))+string(rune('0'+i%10)))
	}
	return out
}

// repoStoryAtCapBody builds an at-cap repository story: 500 workload and
// platform strings, 5000-file-derived documentation signals, 200 wide
// relationship rows with their partition repeats, 100 wide deployment
// evidence artifacts, 100 wide content artifacts, and limit-scale
// documentation/support rows. Uncapped families use representative wide rows;
// every capped family sits at its documented cap.
func repoStoryAtCapBody() map[string]any {
	relationships := make([]map[string]any, 0, 200)
	for range 200 {
		relationships = append(relationships, repoStorySizeWideRow(map[string]any{
			"type": "DEPENDS_ON", "direction": "outgoing",
			"source_name": "edge-api", "source_id": "repo-edge",
			"target_name": "dep", "target_id": "repo-dep",
			"evidence_type": "go_mod_require", "resolved_id": "res-wide",
			"confidence": 0.8, "evidence_count": 5,
		}, 4))
	}
	relationshipOverview := map[string]any{
		"relationship_count": 200,
		"relationships":      relationships,
		"controller_driven":  relationships,
		"workflow_driven":    relationships,
		"story":              "Depends on many.",
	}
	evidenceArtifacts := make([]map[string]any, 0, 100)
	for i := range 100 {
		evidenceArtifacts = append(evidenceArtifacts, repoStorySizeWideRow(map[string]any{
			"id": "art-wide", "path": "charts/api", "evidence_kind": "helm_release",
			"artifact_family": "helm", "relationship_type": "DEPLOYS_FROM",
			"source_repo_id": "repo-edge", "target_repo_id": "repo-platform",
			"resolved_id": "res-wide",
		}, 6))
		_ = i
	}
	contentArtifacts := make([]map[string]any, 0, 100)
	for range 100 {
		contentArtifacts = append(contentArtifacts, repoStorySizeWideRow(map[string]any{
			"relative_path": "Dockerfile.svc", "artifact_type": "dockerfile",
			"artifact_name": "api-image", "service_name": "edge-api",
		}, 8))
	}
	deliveryPaths := make([]map[string]any, 0, 100)
	for range 100 {
		deliveryPaths = append(deliveryPaths, repoStorySizeWideRow(map[string]any{
			"path": "charts/api", "kind": "runtime_artifact", "artifact_type": "dockerfile",
		}, 6))
	}
	deliveryWorkflows := make([]map[string]any, 0, 50)
	for range 50 {
		deliveryWorkflows = append(deliveryWorkflows, repoStorySizeWideRow(map[string]any{
			"path": ".github/workflows/ci.yml", "controller_kind": "github_actions",
		}, 5))
	}
	findings := make([]map[string]any, 0, 10)
	for range 10 {
		findings = append(findings, repoStorySizeWideRow(map[string]any{
			"finding_id": "finding:runbook", "title": "Runbook",
		}, 4))
	}
	supportRows := make([]map[string]any, 0, 10)
	for range 10 {
		supportRows = append(supportRows, repoStorySizeWideRow(map[string]any{
			"link_basis": "incident_routing.pagerduty_service", "fact_id": "fact:1",
			"fact_kind": "pagerduty_service",
		}, 4))
	}
	frameworks := make([]map[string]any, 0, 20)
	for range 20 {
		frameworks = append(frameworks, map[string]any{
			"framework": "go", "confidence": "high", "signal_count": 200,
			"paths": repoStorySizeStrings("src/file", 200),
		})
	}
	return map[string]any{
		"repository": map[string]any{"id": "repo-edge"},
		"subject":    map[string]any{"type": "repository", "id": "repo-edge"},
		"story": "Repository edge-api contains 5000 indexed files. Defines 500 workload(s): " +
			strings.Join(repoStorySizeStrings("workload-", 500), ", ") + ".",
		"story_sections": []map[string]any{
			{"title": "codebase", "summary": "5000 indexed file(s) across 200 language family(s)"},
			{"title": "deployment", "summary": strings.Repeat("deployment signals observed across many workloads. ", 20)},
		},
		"coverage_summary":      map[string]any{"status": "ok"},
		"limitations":           []string{},
		"truncated":             false,
		"drilldowns":            map[string]any{"context_path": "/api/v0/repositories/repo-edge/context"},
		"relationship_overview": relationshipOverview,
		"infrastructure_overview": map[string]any{
			"families":              []string{"helm"},
			"relationship_overview": relationshipOverview,
			"deployment_evidence": map[string]any{
				"artifact_count": 100, "artifacts": evidenceArtifacts,
				"evidence_index": map[string]any{"by_family": map[string]any{"helm": 100}},
			},
			"deployment_artifacts": contentArtifacts,
		},
		"deployment_overview": map[string]any{
			"workload_count": 500, "workloads": repoStorySizeStrings("workload-", 500),
			"platform_count": 500, "platforms": repoStorySizeStrings("platform-", 500),
			"deployment_artifacts": contentArtifacts,
			"delivery_paths":       deliveryPaths,
			"delivery_workflows":   deliveryWorkflows,
			"topology_story":       repoStorySizeStrings("edge-api deploys via charts/api ", 100),
		},
		"ci_cd_evidence": map[string]any{
			"static_workflow_artifacts": map[string]any{
				"state": "present", "count": 50,
				"paths": repoStorySizeStrings(".github/workflows/ci-", 50),
			},
		},
		"documentation_overview": map[string]any{
			"repo_slug":           "acme/edge",
			"documentation_files": repoStorySizeStrings("docs/guide-", 200),
			"docs_routes":         repoStorySizeStrings("/docs/route-", 300),
			"target_documentation": map[string]any{
				"findings": findings, "finding_count": 10,
				"related_facts": []map[string]any{}, "related_fact_count": 0,
			},
		},
		"support_overview": map[string]any{
			"dependency_count": 200,
			"target_support": map[string]any{
				"evidence": supportRows, "evidence_count": 10,
				"ambiguous_evidence": []map[string]any{}, "ambiguous_count": 0,
			},
		},
		"framework_summary": map[string]any{"framework_count": 20, "frameworks": frameworks},
		"semantic_overview": map[string]any{"entity_count": 5000},
		"gitops_overview":   map[string]any{"enabled": true},
	}
}

// TestRepoStoryEvidenceDetailAtCapHandlesFitsBudget is the acceptance gate
// for the whole fix: the at-cap handles body must fit the 256 KiB dispatch
// budget with margin. It records both marshaled sizes so the PR can quote
// them; a future family that erodes the margin fails here before MCP callers
// see an over-budget error instead of a story.
func TestRepoStoryEvidenceDetailAtCapHandlesFitsBudget(t *testing.T) {
	t.Parallel()

	const budget = 256 * 1024

	fullBody := repoStoryAtCapBody()
	fullBytes, err := json.Marshal(fullBody)
	if err != nil {
		t.Fatalf("marshal full: %v", err)
	}

	handlesBody := repoStoryAtCapBody()
	omissions := ApplyRepoStoryEvidenceDetail(handlesBody, ContextEvidenceDetailHandles)
	if len(omissions) == 0 {
		t.Fatal("at-cap omissions empty; the fixture must exercise every projected family")
	}
	handlesBytes, err := json.Marshal(handlesBody)
	if err != nil {
		t.Fatalf("marshal handles: %v", err)
	}

	t.Logf("repo story full body = %d bytes, handles body = %d bytes, budget = %d bytes", len(fullBytes), len(handlesBytes), budget)
	if len(handlesBytes) >= len(fullBytes) {
		t.Fatalf("handles body = %d bytes, want less than full body = %d", len(handlesBytes), len(fullBytes))
	}
	if len(handlesBytes) > budget {
		t.Fatalf("handles body = %d bytes, want <= %d budget", len(handlesBytes), budget)
	}
}
