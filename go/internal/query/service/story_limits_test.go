// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestServiceStoryDossierUsesAggregateAPICountsAndSpecPaths(t *testing.T) {
	t.Parallel()

	ctx := testutil.SampleServiceDossierContext()
	ctx["api_surface"] = map[string]any{
		"endpoint_count": 73,
		"method_count":   9,
		"spec_paths":     []string{"openapi.yaml", "admin.yaml"},
		"endpoints":      []map[string]any{},
	}
	ctx["support_overview"] = buildServiceSupportOverview(ctx)

	got := BuildServiceStoryResponse("sample-service-api", ctx)
	apiSurface := querycontract.MapValue(got, "api_surface")
	if got, want := querycontract.IntVal(apiSurface, "endpoint_count"), 73; got != want {
		t.Fatalf("api_surface.endpoint_count = %d, want %d", got, want)
	}
	if got, want := querycontract.IntVal(apiSurface, "spec_count"), 2; got != want {
		t.Fatalf("api_surface.spec_count = %d, want len(spec_paths) %d", got, want)
	}
	supportOverview := querycontract.MapValue(got, "support_overview")
	if got, want := querycontract.IntVal(supportOverview, "spec_count"), 2; got != want {
		t.Fatalf("support_overview.spec_count = %d, want api_surface.spec_count %d", got, want)
	}
	limits := querycontract.MapValue(got, "result_limits")
	if got, want := querycontract.IntVal(limits, "endpoint_count"), 73; got != want {
		t.Fatalf("result_limits.endpoint_count = %d, want aggregate %d", got, want)
	}
}

func TestServiceStorySupportOverviewUsesAPISurfaceSpecPathCount(t *testing.T) {
	t.Parallel()

	ctx := testutil.SampleServiceDossierContext()
	ctx["api_surface"] = map[string]any{
		"endpoint_count": 73,
		"method_count":   9,
		"spec_paths":     []string{"openapi.yaml", "admin.yaml"},
		"endpoints":      []map[string]any{},
	}
	ctx["support_overview"] = buildServiceSupportOverview(ctx)

	got := BuildServiceStoryResponse("sample-service-api", ctx)
	apiSurface := querycontract.MapValue(got, "api_surface")
	if got, want := querycontract.IntVal(apiSurface, "spec_count"), 2; got != want {
		t.Fatalf("api_surface.spec_count = %d, want len(spec_paths) %d", got, want)
	}
	supportOverview := querycontract.MapValue(got, "support_overview")
	if got, want := querycontract.IntVal(supportOverview, "spec_count"), querycontract.IntVal(apiSurface, "spec_count"); got != want {
		t.Fatalf("support_overview.spec_count = %d, want api_surface.spec_count %d", got, want)
	}
}

func TestServiceStoryDossierBoundsRawPayloadsAndNestedAPISurface(t *testing.T) {
	t.Parallel()

	ctx := testutil.SampleServiceDossierContext()
	ctx["api_surface"] = map[string]any{
		"endpoint_count": 60,
		"method_count":   1,
		"endpoints":      numberedRows("path", 60),
	}
	ctx["hostnames"] = numberedRows("hostname", 60)
	ctx["deployment_evidence"] = map[string]any{
		"artifacts":       numberedRows("resolved_id", 60),
		"delivery_paths":  numberedRows("path", 60),
		"artifact_count":  60,
		"tool_families":   []string{"argocd"},
		"evidence_family": "deployment",
	}

	got := BuildServiceStoryResponse("sample-service-api", ctx)
	if got := len(querycontract.MapSliceValue(got, "hostnames")); got != serviceStoryItemLimit {
		t.Fatalf("top-level hostnames length = %d, want cap %d", got, serviceStoryItemLimit)
	}
	deploymentEvidence := querycontract.MapValue(got, "deployment_evidence")
	if got := len(querycontract.MapSliceValue(deploymentEvidence, "artifacts")); got != serviceStoryItemLimit {
		t.Fatalf("deployment_evidence.artifacts length = %d, want cap %d", got, serviceStoryItemLimit)
	}
	// #8798: the endpoint rows ship once, at the top level; the overview copy
	// keeps its counts and names where the rows are.
	if got := len(querycontract.MapSliceValue(querycontract.MapValue(got, "api_surface"), "endpoints")); got != serviceStoryItemLimit {
		t.Fatalf("api_surface.endpoints length = %d, want cap %d", got, serviceStoryItemLimit)
	}
	overviewAPI := querycontract.MapValue(querycontract.MapValue(got, "deployment_overview"), "api_surface")
	if _, has := overviewAPI["endpoints"]; has {
		t.Fatalf("deployment_overview.api_surface.endpoints present = %#v, want the rows shipped once at api_surface", overviewAPI)
	}
	if got := querycontract.StringVal(overviewAPI, "endpoints_shipped_at"); got != "api_surface.endpoints" {
		t.Fatalf("deployment_overview.api_surface.endpoints_shipped_at = %q, want api_surface.endpoints", got)
	}
	if got, want := querycontract.IntVal(overviewAPI, "endpoint_count"), 60; got != want {
		t.Fatalf("deployment_overview.api_surface.endpoint_count = %d, want %d (counts stay)", got, want)
	}
	rawLimits := querycontract.MapValue(got, "raw_context_limits")
	if !querycontract.BoolVal(querycontract.MapValue(rawLimits, "hostnames"), "truncated") {
		t.Fatalf("raw_context_limits.hostnames.truncated = false, want true")
	}
}

func TestServiceStoryDossierDedupesRelationshipPreviewsAndIncludesDependents(t *testing.T) {
	t.Parallel()

	ctx := testutil.SampleServiceDossierContext()
	ctx["dependents"] = []map[string]any{
		{"repository": "consumer-a", "repo_id": "repo-consumer-a", "relationship_types": []string{"CALLS"}},
	}
	ctx["deployment_evidence"] = map[string]any{
		"artifacts": []map[string]any{
			{"direction": "incoming", "relationship_type": "DEPLOYS_FROM", "resolved_id": "resolved-same", "confidence": 0.7, "source_repo_id": "repo-deploy", "source_repo_name": "deploy", "target_repo_id": "repo-service", "target_repo_name": "service"},
			{"direction": "incoming", "relationship_type": "DEPLOYS_FROM", "resolved_id": "resolved-same", "confidence": 0.9, "source_repo_id": "repo-deploy", "source_repo_name": "deploy", "target_repo_id": "repo-service", "target_repo_name": "service"},
		},
	}

	got := BuildServiceStoryResponse("sample-service-api", ctx)
	upstream := querycontract.MapSliceValue(got, "upstream_dependencies")
	if countRowsWithValue(upstream, "resolved_id", "resolved-same") != 1 {
		t.Fatalf("upstream_dependencies = %#v, want one row for resolved-same", upstream)
	}
	edges := querycontract.MapSliceValue(querycontract.MapValue(got, "evidence_graph"), "edges")
	if countRowsWithValue(edges, "resolved_id", "resolved-same") != 1 {
		t.Fatalf("evidence_graph.edges = %#v, want one edge for resolved-same", edges)
	}
	nodes := querycontract.MapSliceValue(querycontract.MapValue(got, "evidence_graph"), "nodes")
	if countRowsWithValue(nodes, "id", "repo-consumer-a") != 1 {
		t.Fatalf("evidence_graph.nodes = %#v, want graph dependent repo node", nodes)
	}
}

func TestServiceStoryResultLimitsMatchIndependentDownstreamCaps(t *testing.T) {
	t.Parallel()

	ctx := testutil.SampleServiceDossierContext()
	ctx["dependents"] = numberedRows("repo_id", 30)
	ctx["consumer_repositories"] = numberedRows("repo_id", 30)

	got := BuildServiceStoryResponse("sample-service-api", ctx)
	downstream := querycontract.MapValue(got, "downstream_consumers")
	if querycontract.BoolVal(downstream, "truncated") {
		t.Fatalf("downstream_consumers.truncated = true, want false for two independently uncapped lists")
	}
	limits := querycontract.MapValue(got, "result_limits")
	if querycontract.BoolVal(limits, "truncated") {
		t.Fatalf("result_limits.truncated = true, want false when no section cap is exceeded")
	}
}

func TestServiceStoryDossierSurfacesUncorrelatedCloudResourceTruncation(t *testing.T) {
	t.Parallel()

	// over-limit case: truncation flag must appear in dossier response
	ctx := testutil.SampleServiceDossierContext()
	ctx["uncorrelated_cloud_resources"] = numberedRows("id", serviceStoryItemLimit+1)
	ctx["uncorrelated_cloud_resources_truncated"] = true

	got := BuildServiceStoryResponse("orders-api", ctx)
	if !querycontract.BoolVal(got, "uncorrelated_cloud_resources_truncated") {
		t.Fatalf("uncorrelated_cloud_resources_truncated = false in dossier response, want true when truncation occurred")
	}
	// rows must still be capped
	if n := len(querycontract.MapSliceValue(got, "uncorrelated_cloud_resources")); n != serviceStoryItemLimit {
		t.Fatalf("len(uncorrelated_cloud_resources) = %d, want %d (capped)", n, serviceStoryItemLimit)
	}

	// under-limit case: flag must be absent
	ctx2 := testutil.SampleServiceDossierContext()
	ctx2["uncorrelated_cloud_resources"] = numberedRows("id", 3)
	// do NOT set truncated flag

	got2 := BuildServiceStoryResponse("orders-api", ctx2)
	if _, present := got2["uncorrelated_cloud_resources_truncated"]; present {
		t.Fatalf("uncorrelated_cloud_resources_truncated present in dossier response, want absent when no truncation")
	}
}

func numberedRows(key string, count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for i := range count {
		rows = append(rows, map[string]any{
			key: fmt.Sprintf("%s-%03d", key, i),
		})
	}
	return rows
}

// TestServiceStoryDownstreamCountDedupesSharedRepository is the #10656
// regression: a repository that is both a graph dependent and a content
// consumer counts twice in downstream_entries (the sum) but once in
// downstream_repository_count (the distinct set), so the two numbers differ
// and neither overstates the distinct repository count.
func TestServiceStoryDownstreamCountDedupesSharedRepository(t *testing.T) {
	t.Parallel()

	ctx := testutil.SampleServiceDossierContext()
	ctx["dependents"] = []map[string]any{
		{"repository": "repo-a", "repo_id": "repo-a"},
		{"repository": "repo-b", "repo_id": "repo-b"},
	}
	ctx["consumer_repositories"] = []map[string]any{
		{"repository": "repo-b", "repo_id": "repo-b"},
		{"repository": "repo-c", "repo_id": "repo-c"},
	}

	got := BuildServiceStoryResponse("sample-service-api", ctx)
	limits := querycontract.MapValue(got, "result_limits")
	if got, want := querycontract.IntVal(limits, "downstream_entries"), 4; got != want {
		t.Fatalf("result_limits.downstream_entries = %d, want %d (repo-b counts in both lists)", got, want)
	}
	if got, want := querycontract.IntVal(limits, "downstream_repository_count"), 3; got != want {
		t.Fatalf("result_limits.downstream_repository_count = %d, want %d (repo-b deduped across lists)", got, want)
	}
	if got, want := querycontract.IntVal(limits, "downstream_count"), 4; got != want {
		t.Fatalf("result_limits.downstream_count = %d, want %d (deprecated alias of downstream_entries)", got, want)
	}
	if querycontract.BoolVal(limits, "lower_bound") {
		t.Fatalf("result_limits.lower_bound = true, want false (no read bound fired)")
	}
}

// TestServiceStoryDownstreamCountLowerBoundCases covers the #10656 edge
// cases: zero consumers, a fully grant-filtered consumer list (which arrives
// here as zero rows but still carries the read-bound signal), a consumer read
// past the 100 clamp, and a truncated dependents read beside an untruncated
// consumer read.
func TestServiceStoryDownstreamCountLowerBoundCases(t *testing.T) {
	t.Parallel()

	consumerRows := func(prefix string, n int) []map[string]any {
		rows := make([]map[string]any, 0, n)
		for i := range n {
			rows = append(rows, map[string]any{
				"repository": fmt.Sprintf("%s-%03d", prefix, i),
				"repo_id":    fmt.Sprintf("repo-%s-%03d", prefix, i),
			})
		}
		return rows
	}

	for _, tc := range []struct {
		name           string
		dependents     []map[string]any
		consumers      []map[string]any
		flags          map[string]any
		wantEntries    int
		wantRepos      int
		wantLowerBound bool
		wantListTool   bool
	}{
		{
			name:           "zero consumers counts dependents only",
			dependents:     []map[string]any{{"repository": "repo-a", "repo_id": "repo-a"}, {"repository": "repo-b", "repo_id": "repo-b"}},
			consumers:      nil,
			wantEntries:    2,
			wantRepos:      2,
			wantLowerBound: false,
			wantListTool:   false,
		},
		{
			name:           "all consumers grant-filtered still reports the read bound",
			dependents:     []map[string]any{{"repository": "repo-a", "repo_id": "repo-a"}, {"repository": "repo-b", "repo_id": "repo-b"}},
			consumers:      nil,
			flags:          map[string]any{"consumer_repositories_downstream_truncated": true},
			wantEntries:    2,
			wantRepos:      2,
			wantLowerBound: true,
			wantListTool:   true,
		},
		{
			name:           "more than 100 consumers caps at the read bound",
			consumers:      consumerRows("consumer", 100),
			flags:          map[string]any{"consumer_repositories_downstream_truncated": true},
			wantEntries:    100,
			wantRepos:      100,
			wantLowerBound: true,
			wantListTool:   true,
		},
		{
			name:           "truncated dependents beside untruncated consumers",
			dependents:     []map[string]any{{"repository": "repo-a", "repo_id": "repo-a"}},
			consumers:      []map[string]any{{"repository": "repo-b", "repo_id": "repo-b"}},
			flags:          map[string]any{"dependents_truncated": true},
			wantEntries:    2,
			wantRepos:      2,
			wantLowerBound: true,
			wantListTool:   true,
		},
		{
			name:           "overlap under a truncated read stays distinct and bounded",
			dependents:     []map[string]any{{"repository": "repo-a", "repo_id": "repo-a"}, {"repository": "repo-b", "repo_id": "repo-b"}},
			consumers:      []map[string]any{{"repository": "repo-b", "repo_id": "repo-b"}, {"repository": "repo-c", "repo_id": "repo-c"}},
			flags:          map[string]any{"dependents_truncated": true},
			wantEntries:    4,
			wantRepos:      3,
			wantLowerBound: true,
			wantListTool:   true,
		},
		{
			name:           "evidence-file truncation alone is not a downstream lower bound",
			dependents:     []map[string]any{{"repository": "repo-a", "repo_id": "repo-a"}},
			consumers:      []map[string]any{{"repository": "repo-b", "repo_id": "repo-b"}},
			flags:          map[string]any{"consumer_repositories_evidence_file_truncated": true},
			wantEntries:    2,
			wantRepos:      2,
			wantLowerBound: false,
			wantListTool:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := testutil.SampleServiceDossierContext()
			ctx["dependents"] = tc.dependents
			ctx["consumer_repositories"] = tc.consumers
			for key, value := range tc.flags {
				ctx[key] = value
			}

			got := BuildServiceStoryResponse("sample-service-api", ctx)
			limits := querycontract.MapValue(got, "result_limits")
			if got := querycontract.IntVal(limits, "downstream_entries"); got != tc.wantEntries {
				t.Fatalf("result_limits.downstream_entries = %d, want %d", got, tc.wantEntries)
			}
			if got := querycontract.IntVal(limits, "downstream_repository_count"); got != tc.wantRepos {
				t.Fatalf("result_limits.downstream_repository_count = %d, want %d", got, tc.wantRepos)
			}
			if got := querycontract.IntVal(limits, "downstream_count"); got != tc.wantEntries {
				t.Fatalf("result_limits.downstream_count = %d, want %d (deprecated alias of downstream_entries)", got, tc.wantEntries)
			}
			if got := querycontract.BoolVal(limits, "lower_bound"); got != tc.wantLowerBound {
				t.Fatalf("result_limits.lower_bound = %v, want %v", got, tc.wantLowerBound)
			}
			overview := buildServiceSupportOverview(ctx)
			gotTool := querycontract.StringVal(overview, "consumer_list_tool") != ""
			if gotTool != tc.wantListTool {
				t.Fatalf("support_overview.consumer_list_tool present = %v, want %v", gotTool, tc.wantListTool)
			}
		})
	}
}

// TestServiceSupportOverviewNamesConsumerListDrilldown pins the #10656
// context pointer: the support overview (shipped by both the story and the
// service context routes) names the paged consumer list tool and path only
// when a downstream read bound fired, because only then is there a known
// remainder past the rendered rows.
func TestServiceSupportOverviewNamesConsumerListDrilldown(t *testing.T) {
	t.Parallel()

	ctx := testutil.SampleServiceDossierContext()
	if overview := buildServiceSupportOverview(ctx); querycontract.StringVal(overview, "consumer_list_tool") != "" {
		t.Fatalf("support_overview.consumer_list_tool = %q, want absent (no read bound fired)", querycontract.StringVal(overview, "consumer_list_tool"))
	}

	ctx["consumer_repositories_downstream_truncated"] = true
	overview := buildServiceSupportOverview(ctx)
	if got, want := querycontract.StringVal(overview, "consumer_list_tool"), "list_service_downstream_consumers"; got != want {
		t.Fatalf("support_overview.consumer_list_tool = %q, want %q", got, want)
	}
	if got, want := querycontract.StringVal(overview, "consumer_list_path"), "/api/v0/services/sample-service-api/downstream-consumers"; got != want {
		t.Fatalf("support_overview.consumer_list_path = %q, want %q", got, want)
	}
}

func countRowsWithValue(rows []map[string]any, key string, want string) int {
	count := 0
	for _, row := range rows {
		if querycontract.StringVal(row, key) == want {
			count++
		}
	}
	return count
}
