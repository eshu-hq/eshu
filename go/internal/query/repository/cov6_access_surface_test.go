// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	contractrepository "github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
)

func cov6ScopedAccess() querycontract.RepositoryAccessFilter {
	return querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repository:a"},
		Allowed:              map[string]struct{}{"repository:a": {}},
	}
}

func TestCov6TruncateUTF8(t *testing.T) {
	t.Parallel()
	if got := truncateUTF8("hello", 10); got != "hello" {
		t.Fatalf("short string = %q, want hello", got)
	}
	if got := truncateUTF8("hello world", 5); got != "hello" {
		t.Fatalf("ascii cut = %q, want hello", got)
	}
	// "héllo": h(1) é(2) l l o. Cutting at 4 bytes lands on a boundary.
	if got := truncateUTF8("héllo", 4); got != "hél" {
		t.Fatalf("rune-boundary cut = %q, want hél", got)
	}
	// Cutting at 2 bytes splits é and must fall back to h.
	if got := truncateUTF8("héllo", 2); got != "h" {
		t.Fatalf("split-rune cut = %q, want h", got)
	}
	if got := truncateUTF8("héllo", 3); got != "hé" {
		t.Fatalf("exact-rune cut = %q, want hé", got)
	}
}

func TestCov6MergeRepositoryRelationships(t *testing.T) {
	t.Parallel()
	if got := mergeRepositoryDeployableUnitRelationships(nil, []map[string]any{{"a": 1}}); got != nil {
		t.Fatal("nil read model must stay nil")
	}
	base := &contractrepository.RepositoryRelationshipReadModel{Available: true}
	if got := mergeRepositoryDeployableUnitRelationships(base, nil); got != base {
		t.Fatal("empty supplemental must return the model unchanged")
	}
	row := func(direction, source, target string) map[string]any {
		return map[string]any{
			"direction": direction, "type": "DEPENDS_ON",
			"source_id": source, "target_id": target,
			"resolved_id": "x", "generation_id": "g1",
			"source_name": "src",
		}
	}
	model := &contractrepository.RepositoryRelationshipReadModel{Available: true}
	merged := mergeRepositoryDeployableUnitRelationships(model, []map[string]any{
		row("incoming", "repository:a", "repository:b"),
		row("incoming", "repository:a", "repository:b"),
		row("outgoing", "repository:b", "repository:c"),
		{"unkeyed": true},
	})
	if merged == nil || len(merged.Relationships) != 3 {
		t.Fatalf("merged relationships = %v, want 3 deduplicated rows", merged)
	}
	if len(merged.Consumers) != 1 || merged.Consumers[0]["id"] != "repository:a" {
		t.Fatalf("consumers = %v, want [repository:a]", merged.Consumers)
	}
	if !merged.Available {
		t.Fatal("merge must carry availability through")
	}
	// Keyless rows never dedupe; empty keys read back empty.
	if got := repositoryRelationshipRowKey(map[string]any{}); got != "" {
		t.Fatalf("empty row key = %q, want empty", got)
	}
	if got := repositoryConsumersFromRelationships([]map[string]any{
		{"direction": "outgoing", "source_id": "x"},
		{"direction": "incoming"},
	}); len(got) != 0 {
		t.Fatalf("non-consumer rows = %v, want none", got)
	}
}

func TestCov6CoverageGapAndCompleteness(t *testing.T) {
	t.Parallel()
	graphGap, contentGap := computeCoverageGapCounts(8, 100, 10, 120)
	if graphGap != 22 || contentGap != 0 {
		t.Fatalf("gaps = (%d,%d), want (22,0)", graphGap, contentGap)
	}
	graphGap, contentGap = computeCoverageGapCounts(10, 120, 8, 100)
	if graphGap != 0 || contentGap != 22 {
		t.Fatalf("reversed gaps = (%d,%d), want (0,22)", graphGap, contentGap)
	}
	for _, tc := range []struct {
		graph, content bool
		graphGap       int
		contentGap     int
		want           string
	}{
		{false, false, 0, 0, "unknown"},
		{false, true, 0, 0, "graph_unavailable"},
		{true, false, 0, 0, "content_unavailable"},
		{true, true, 0, 0, "complete"},
		{true, true, 3, 2, "graph_and_content_partial"},
		{true, true, 3, 0, "graph_partial"},
		{true, true, 0, 2, "content_partial"},
	} {
		if got := completenessStateForCoverage(tc.graph, tc.content, tc.graphGap, tc.contentGap); got != tc.want {
			t.Fatalf("completeness(%v,%v,%d,%d) = %q, want %q",
				tc.graph, tc.content, tc.graphGap, tc.contentGap, got, tc.want)
		}
	}
}

func TestCov6DeploymentEvidenceAccessFilter(t *testing.T) {
	t.Parallel()
	unscoped := querycontract.RepositoryAccessFilter{AllScopes: true}
	rows := []map[string]any{{"source_repo_id": "repository:x"}}
	if got := filterDeploymentEvidenceRowsForAccess(rows, "repository:a", unscoped); len(got) != 1 {
		t.Fatal("unscoped caller must see every row")
	}
	scoped := cov6ScopedAccess()
	// Cross-tenant non-anchor endpoint drops the whole row.
	dropped := map[string]any{"source_repo_id": "repository:a", "target_repo_id": "repository:evil"}
	if redactDeploymentEvidenceRowForAccess(dropped, "repository:a", scoped) {
		t.Fatal("cross-tenant row must be dropped")
	}
	// Anchor endpoints stay fully visible.
	owned := map[string]any{
		"source_repo_id": "repository:a", "source_name": "a",
		"target_repo_id": "repository:a", "target_name": "a",
	}
	if !redactDeploymentEvidenceRowForAccess(owned, "repository:a", scoped) {
		t.Fatal("fully-owned row must survive")
	}
	if owned["source_name"] != "a" {
		t.Fatal("owned endpoint identity must stay intact")
	}
	// Unverifiable endpoints are blanked deny-by-default, not dropped.
	blanked := map[string]any{
		"source_repo_id": "", "source_repo_name": "ghost", "source_repo_scope_id": "s1",
		"target_repo_id": "repository:a", "target_repo_name": "a",
		"path": "deploy.yaml",
	}
	if !redactDeploymentEvidenceRowForAccess(blanked, "repository:a", scoped) {
		t.Fatal("unverifiable-endpoint row must survive blanked")
	}
	if _, present := blanked["source_repo_name"]; present {
		t.Fatalf("blanked source identity = %v, want removed", blanked["source_repo_name"])
	}
	if blanked["path"] != "deploy.yaml" {
		t.Fatal("artifact evidence fields must survive blanking")
	}
	filtered := filterDeploymentEvidenceRowsForAccess([]map[string]any{dropped, owned}, "repository:a", scoped)
	if len(filtered) != 1 {
		t.Fatalf("filtered rows = %d, want 1", len(filtered))
	}
}

func TestCov6RelationshipAccessFilter(t *testing.T) {
	t.Parallel()
	unscoped := querycontract.RepositoryAccessFilter{AllScopes: true}
	rows := []map[string]any{{"target_id": "repository:evil"}}
	if got := filterRepoRelationshipTargetRowsForAccess(rows, "target_id", unscoped); len(got) != 1 {
		t.Fatal("unscoped caller must see every row")
	}
	scoped := cov6ScopedAccess()
	kept := filterRepoRelationshipTargetRowsForAccess([]map[string]any{
		{"target_id": "repository:a"},
		{"target_id": "repository:evil"},
	}, "target_id", scoped)
	if len(kept) != 1 {
		t.Fatalf("scoped target rows = %d, want 1", len(kept))
	}
	overview := FilterRepoRelationshipOverviewRowsForAccess([]map[string]any{
		{"source_id": "repository:a", "target_id": "repository:a"},
		{"source_id": "repository:a", "target_id": "repository:evil"},
		{"source_id": "", "target_id": "repository:a"},
	}, "repository:a", scoped)
	if len(overview) != 1 {
		t.Fatalf("scoped overview rows = %d, want 1 (anchor pair only)", len(overview))
	}
	if !repositoryRelationshipEndpointAllowed("repository:a", "repository:a", scoped) {
		t.Fatal("anchor endpoint must always be allowed")
	}
	if repositoryRelationshipEndpointAllowed("repository:evil", "repository:a", scoped) {
		t.Fatal("out-of-grant endpoint must be denied")
	}
	model := &contractrepository.RepositoryRelationshipReadModel{Available: true}
	if got := filterRepositoryRelationshipReadModelForAccess(nil, "repository:a", scoped); got != nil {
		t.Fatal("nil read model must stay nil")
	}
	if got := filterRepositoryRelationshipReadModelForAccess(model, "repository:a", unscoped); got != model {
		t.Fatal("unscoped read model must pass through unchanged")
	}
}

func TestCov6BuildGraphAPISurface(t *testing.T) {
	t.Parallel()
	if got := frameworkNamesFromSourceKinds([]string{"framework:fastapi", "openapi", "framework: ", "framework:fastapi"}); len(got) != 2 {
		t.Fatalf("framework names = %v, want [fastapi fastapi]", got)
	}
	rows := []map[string]any{
		{
			"endpoint_id": "e1", "path": "/a", "methods": []any{"GET"},
			"operation_ids": []any{"op1"}, "source_kinds": []any{"framework:fastapi"},
			"source_paths": []any{"api.py"}, "spec_versions": []any{"3.0"},
			"api_versions": []any{"v1"}, "evidence_source": "graph",
			"workload_id": "w1", "workload_name": "web",
		},
		{"endpoint_id": "e2", "path": "/b"},
	}
	if got := countFrameworkEndpointRows(rows); got != 1 {
		t.Fatalf("framework rows = %d, want 1", got)
	}
	surface := buildGraphAPISurface(rows, 5)
	if surface["endpoint_count"] != 5 || surface["method_count"] != 1 || surface["operation_id_count"] != 1 {
		t.Fatalf("surface counts = %v, want 5/1/1", surface)
	}
	if surface["detail_truncated"] != true {
		t.Fatalf("surface truncated = %v, want true (5 > 2)", surface["detail_truncated"])
	}
	if surface["frameworks"] == nil || surface["framework_route_count"] != 1 {
		t.Fatalf("surface frameworks = %v, want fastapi with count 1", surface)
	}
	plain := buildGraphAPISurface([]map[string]any{{"endpoint_id": "e9"}}, 1)
	if _, hasFrameworks := plain["frameworks"]; hasFrameworks {
		t.Fatalf("frameworkless surface must omit frameworks, got %v", plain)
	}
}

func TestCov6SourceToolBreakdownAndCatalogLimit(t *testing.T) {
	t.Parallel()
	if got := BuildSourceToolBreakdownFromRows(nil); got != nil {
		t.Fatalf("empty breakdown = %v, want nil", got)
	}
	breakdown := BuildSourceToolBreakdownFromRows([]map[string]any{
		{"source_tool": "go", "edge_count": 4},
		{"source_tool": "", "edge_count": 9},
		{"source_tool": "npm", "edge_count": 2},
	})
	if len(breakdown) != 2 || breakdown["go"] != 4 || breakdown["npm"] != 2 {
		t.Fatalf("breakdown = %v, want map[go:4 npm:2]", breakdown)
	}
	if got := BuildSourceToolBreakdownFromRows([]map[string]any{{"source_tool": ""}}); got != nil {
		t.Fatalf("blank-only breakdown = %v, want nil", got)
	}
	req := httptest.NewRequest("GET", "/catalog?limit=5", nil)
	if got := catalogLimitFromRequest(req); got != 5 {
		t.Fatalf("catalog limit = %d, want 5", got)
	}
	bad := httptest.NewRequest("GET", "/catalog?limit=0", nil)
	if got := catalogLimitFromRequest(bad); got != defaultCatalogLimit {
		t.Fatalf("zero limit = %d, want default %d", got, defaultCatalogLimit)
	}
	huge := httptest.NewRequest("GET", "/catalog?limit=999999", nil)
	if got := catalogLimitFromRequest(huge); got != maxCatalogLimit {
		t.Fatalf("huge limit = %d, want max %d", got, maxCatalogLimit)
	}
}
