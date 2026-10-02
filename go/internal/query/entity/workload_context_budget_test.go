// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"fmt"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

const (
	// budgetEndpointTotal is the endpoint-edge count of the populated ops-qa
	// services that stayed over the MCP budget after #7169 (72-78 edges); the
	// graph read returns the first 50 rows of them.
	budgetEndpointTotal = 78
	// budgetArtifactsPerDirection fills the deployment-evidence read to its
	// per-direction cap, so the context carries the worst case of 100 rows.
	budgetArtifactsPerDirection = 50
)

// budgetEndpointRows returns count API endpoint rows with every column the
// graph read selects populated at a realistic width.
func budgetEndpointRows(count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, map[string]any{
			"endpoint_id":     fmt.Sprintf("endpoint:repo-1:/api/v2/organizations/{organizationId}/projects/{projectId}/listings/{listingId}/media/%03d", i),
			"path":            fmt.Sprintf("/api/v2/organizations/{organizationId}/projects/{projectId}/listings/{listingId}/media/%03d", i),
			"methods":         []any{"GET", "POST", "PUT", "DELETE"},
			"operation_ids":   []any{fmt.Sprintf("getListingMedia%03d", i), fmt.Sprintf("createListingMedia%03d", i)},
			"source_kinds":    []any{"openapi", "framework:fastapi"},
			"source_paths":    []any{"services/listings/api/openapi/listings.v2.yaml", "services/listings/app/routers/media.py"},
			"spec_versions":   []any{"3.0.3"},
			"api_versions":    []any{"v2"},
			"evidence_source": "parser/openapi",
			"workload_id":     "workload-1",
			"workload_name":   "svc",
		})
	}
	return rows
}

// budgetArtifactRows returns count deployment-evidence rows for one direction
// with every selected column populated at a realistic width.
func budgetArtifactRows(direction string, count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, map[string]any{
			"direction":              direction,
			"artifact_id":            fmt.Sprintf("evidence-artifact:%s:%03d:3f9a1c2e-7b4d-4e8a-9c61-2d5b8e0f7a13", direction, i),
			"name":                   fmt.Sprintf("values-production-%03d.yaml", i),
			"domain":                 "deployment",
			"path":                   fmt.Sprintf("deploy/kustomize/overlays/production/service-%03d/values-production.yaml", i),
			"evidence_kind":          "ARGOCD_APPLICATION_SOURCE",
			"artifact_family":        "argocd",
			"extractor":              "argocd_application_source_extractor",
			"relationship_type":      "DEPLOYS_FROM",
			"resolved_id":            fmt.Sprintf("resolved:%s:%03d:8a1d6c40-52be-47f3-bb09-6e3c0d9a4f21", direction, i),
			"generation_id":          "generation:5b7e2f90-1c3a-4d68-a0e4-93f1c7d2b8a6",
			"confidence":             0.92,
			"environment":            "production",
			"runtime_platform_kind":  "kubernetes",
			"matched_alias":          "svc-listings-service",
			"matched_value":          "https://git.example.test/platform/svc-listings-service.git",
			"evidence_source":        "resolver/cross-repo",
			"start_line":             12 + i,
			"end_line":               40 + i,
			"commit_sha":             "9f2c1ab47d3e50886b21c4fd79a0e3315c6d8e42",
			"ref_value":              "refs/heads/main",
			"ref_pinned":             true,
			"source_repo_id":         "repo-1",
			"source_repo_name":       "svc",
			"source_repo_remote_url": "https://git.example.test/platform/svc.git",
			"source_repo_scope_id":   "scope:git-repository:platform/svc",
			"target_repo_id":         fmt.Sprintf("repo-target-%03d", i),
			"target_repo_name":       fmt.Sprintf("platform-deploy-config-%03d", i),
			"target_repo_remote_url": fmt.Sprintf("https://git.example.test/platform/platform-deploy-config-%03d.git", i),
			"target_repo_scope_id":   fmt.Sprintf("scope:git-repository:platform/platform-deploy-config-%03d", i),
		})
	}
	return rows
}

// populatedContextHandler builds a populated service through the real
// enrichment path: the 671-hostname outlier (50 hostnames, 50 entrypoints and
// 50 network paths after the #7169 cap), 78 endpoint edges (50 rows returned),
// and a deployment-evidence read filled to its cap in both directions, which
// is the graph fallback path. The Postgres read model caps artifacts at 50 in
// total.
func populatedContextHandler() *Handler {
	h := outlierHostnameHandler()
	reader := h.Neo4j.(graph.FakeWorkloadGraphReader)
	reader.RunByMatch["RETURN count(endpoint) AS endpoint_count"] = []map[string]any{{"endpoint_count": budgetEndpointTotal}}
	reader.RunByMatch["RETURN endpoint.id AS endpoint_id"] = budgetEndpointRows(querycontract.ContextStoryItemLimit)
	reader.RunByMatch["RETURN 'outgoing' AS direction"] = budgetArtifactRows("outgoing", budgetArtifactsPerDirection)
	reader.RunByMatch["RETURN 'incoming' AS direction"] = budgetArtifactRows("incoming", budgetArtifactsPerDirection)
	h.Neo4j = reader
	return h
}

// TestGetWorkloadContextCapsEvidenceRowsOnPopulatedService drives the real
// GetWorkloadContext handler over a populated service (#7129). The overview's
// second copy of the endpoint rows must go, and the artifact rows must be cut
// to the row limit with the row count read, an explicit truncation marker, and
// the evidence_index handles that still address every cut row. The row caps
// alone do not fit this fixture in the MCP budget; evidence_detail handles
// does, and TestContextRoutesHandlesModeFitsWorstCaseBudget proves that.
func TestGetWorkloadContextCapsEvidenceRowsOnPopulatedService(t *testing.T) {
	t.Parallel()

	data, wireBytes := getOutlierJSON(t, populatedContextHandler(), "/api/v0/workloads/workload-1/context", "workload_id", "workload-1")

	t.Logf("populated context est2x with the row caps only = %d bytes (budget %d)", wireBytes, mcpResponseByteBudget)

	apiSurface := querycontract.MapValue(data, "api_surface")
	if got := len(querycontract.MapSliceValue(apiSurface, "endpoints")); got != querycontract.ContextStoryItemLimit {
		t.Fatalf("api_surface.endpoints len = %d, want %d (shipped once, at the top level)", got, querycontract.ContextStoryItemLimit)
	}
	overviewSurface := querycontract.MapValue(querycontract.MapValue(data, "deployment_overview"), "api_surface")
	if _, has := overviewSurface["endpoints"]; has {
		t.Fatal("deployment_overview.api_surface.endpoints is present; the rows ship once at the top level")
	}
	if got, want := querycontract.IntVal(overviewSurface, "endpoint_count"), budgetEndpointTotal; got != want {
		t.Fatalf("deployment_overview.api_surface.endpoint_count = %d, want %d (counts stay)", got, want)
	}

	evidence := querycontract.MapValue(data, "deployment_evidence")
	if got := len(querycontract.MapSliceValue(evidence, "artifacts")); got != querycontract.ContextStoryItemLimit {
		t.Fatalf("deployment_evidence.artifacts len = %d, want %d", got, querycontract.ContextStoryItemLimit)
	}
	if got, want := querycontract.IntVal(evidence, "artifact_count"), 2*budgetArtifactsPerDirection; got != want {
		t.Fatalf("deployment_evidence.artifact_count = %d, want %d (total before the cut)", got, want)
	}
	if !querycontract.BoolVal(evidence, "artifacts_truncated") {
		t.Fatal("deployment_evidence.artifacts_truncated = false next to a cut list, want true")
	}
	byFamily := querycontract.MapValue(querycontract.MapValue(evidence, "evidence_index"), "artifact_families")
	argocd := querycontract.MapValue(byFamily, "argocd")
	if got, want := len(querycontract.StringSliceVal(argocd, "resolved_ids")), 2*budgetArtifactsPerDirection; got != want {
		t.Fatalf("evidence_index handles = %d resolved_ids, want %d (every cut row stays addressable)", got, want)
	}

	if !slices.Contains(stringSlice(t, data["partial_reasons"]), "deployment_evidence_artifacts_truncated") {
		t.Fatalf("partial_reasons = %v, want deployment_evidence_artifacts_truncated", data["partial_reasons"])
	}
	limits := querycontract.MapValue(data, "result_limits")
	if got, want := querycontract.IntVal(limits, "artifact_count"), 2*budgetArtifactsPerDirection; got != want {
		t.Fatalf("result_limits.artifact_count = %d, want %d", got, want)
	}
	if !querycontract.BoolVal(limits, "truncated") {
		t.Fatal("result_limits.truncated = false next to a cut artifact list, want true")
	}
}
