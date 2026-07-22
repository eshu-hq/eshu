// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"testing"
)

type recordingK8sContentStore struct {
	fakePortContentStore
	rows       []EntityContent
	queryLimit int
}

type recordingDeploymentSourceGitOpsContentStore struct {
	fakePortContentStore
	rows       []EntityContent
	queryLimit int
}

func (s *recordingDeploymentSourceGitOpsContentStore) ListRepoEntities(
	_ context.Context,
	_ string,
	limit int,
) ([]EntityContent, error) {
	s.queryLimit = limit
	return append([]EntityContent(nil), s.rows...), nil
}

func (s *recordingK8sContentStore) SearchEntitiesByName(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	limit int,
) ([]EntityContent, error) {
	s.queryLimit = limit
	return append([]EntityContent(nil), s.rows...), nil
}

func TestFetchK8sResourceResultUsesSentinelAndReportsTruncation(t *testing.T) {
	t.Parallel()

	rows := make([]EntityContent, 0, serviceStoryItemLimit+1)
	for index := range serviceStoryItemLimit + 1 {
		rows = append(rows, EntityContent{
			EntityID:     fmt.Sprintf("k8s-%02d", index),
			RepoID:       "repo-service",
			RelativePath: fmt.Sprintf("deploy/%02d.yaml", index),
			EntityType:   "K8sResource",
			EntityName:   "payments-api",
			Metadata: map[string]any{
				"kind":             "Deployment",
				"qualified_name":   fmt.Sprintf("production/Deployment/payments-api-%02d", index),
				"container_images": []any{fmt.Sprintf("registry.example/payments:%02d", index)},
			},
		})
	}
	store := &recordingK8sContentStore{rows: rows}
	handler := &ImpactHandler{Content: store}

	result, err := handler.fetchK8sResourceResult(t.Context(), "repo-service", "payments-api")
	if err != nil {
		t.Fatalf("fetchK8sResourceResult() error = %v", err)
	}
	if got, want := store.queryLimit, serviceStoryItemLimit+1; got != want {
		t.Fatalf("SearchEntitiesByName limit = %d, want sentinel limit %d", got, want)
	}
	if got, want := len(result.rows), serviceStoryItemLimit; got != want {
		t.Fatalf("len(fetchK8sResourceResult().rows) = %d, want %d", got, want)
	}
	if got, want := len(result.imageRefs), serviceStoryItemLimit; got != want {
		t.Fatalf("len(fetchK8sResourceResult().imageRefs) = %d, want images from returned rows only (%d)", got, want)
	}
	if got, want := IntVal(result.limits, "limit"), serviceStoryItemLimit; got != want {
		t.Fatalf("k8s_resource_limits.limit = %d, want %d", got, want)
	}
	if got, want := IntVal(result.limits, "query_sentinel_limit"), serviceStoryItemLimit+1; got != want {
		t.Fatalf("k8s_resource_limits.query_sentinel_limit = %d, want %d", got, want)
	}
	if got, want := IntVal(result.limits, "returned_count"), serviceStoryItemLimit; got != want {
		t.Fatalf("k8s_resource_limits.returned_count = %d, want %d", got, want)
	}
	if got, want := IntVal(result.limits, "observed_count"), serviceStoryItemLimit+1; got != want {
		t.Fatalf("k8s_resource_limits.observed_count = %d, want lower bound %d", got, want)
	}
	if !BoolVal(result.limits, "observed_count_is_lower_bound") {
		t.Fatalf("k8s_resource_limits = %#v, want observed_count_is_lower_bound", result.limits)
	}
	if !BoolVal(result.limits, "truncated") {
		t.Fatalf("k8s_resource_limits = %#v, want truncated", result.limits)
	}
}

func TestFetchK8sResourceResultReportsLowerBoundWhenSubstringRowsFillProbe(t *testing.T) {
	t.Parallel()

	rows := make([]EntityContent, 0, serviceStoryItemLimit+1)
	rows = append(rows, EntityContent{
		EntityID:     "k8s-exact",
		RepoID:       "repo-service",
		RelativePath: "deploy/exact.yaml",
		EntityType:   "K8sResource",
		EntityName:   "payments-api",
	})
	for index := 1; index < serviceStoryItemLimit+1; index++ {
		rows = append(rows, EntityContent{
			EntityID:     fmt.Sprintf("k8s-substring-%02d", index),
			RepoID:       "repo-service",
			RelativePath: fmt.Sprintf("deploy/substring-%02d.yaml", index),
			EntityType:   "K8sResource",
			EntityName:   fmt.Sprintf("payments-api-worker-%02d", index),
		})
	}
	handler := &ImpactHandler{Content: &recordingK8sContentStore{rows: rows}}

	result, err := handler.fetchK8sResourceResult(t.Context(), "repo-service", "payments-api")
	if err != nil {
		t.Fatalf("fetchK8sResourceResult() error = %v", err)
	}
	if got, want := len(result.rows), 1; got != want {
		t.Fatalf("len(fetchK8sResourceResult().rows) = %d, want exact-name row count %d", got, want)
	}
	if got, want := IntVal(result.limits, "observed_count"), 1; got != want {
		t.Fatalf("k8s_resource_limits.observed_count = %d, want observed exact-name count %d", got, want)
	}
	if !BoolVal(result.limits, "observed_count_is_lower_bound") || !BoolVal(result.limits, "truncated") {
		t.Fatalf("k8s_resource_limits = %#v, full substring probe must disclose incomplete exact-name search", result.limits)
	}
}

func TestFetchDeploymentSourceGitOpsReportsRepositoryProbeLowerBound(t *testing.T) {
	t.Parallel()

	rows := make([]EntityContent, repositorySemanticEntityLimit+1)
	store := &recordingDeploymentSourceGitOpsContentStore{rows: rows}
	handler := &ImpactHandler{Content: store}

	_, _, _, lowerBound, err := handler.fetchDeploymentSourceGitOps(
		t.Context(),
		"payments-api",
		"",
		[]map[string]any{{"repo_id": "repository:deploy"}},
	)
	if err != nil {
		t.Fatalf("fetchDeploymentSourceGitOps() error = %v", err)
	}
	if got, want := store.queryLimit, repositorySemanticEntityLimit+1; got != want {
		t.Fatalf("ListRepoEntities limit = %d, want sentinel limit %d", got, want)
	}
	if !lowerBound {
		t.Fatal("fetchDeploymentSourceGitOps() lowerBound = false, want true after sentinel saturation")
	}
}

func TestBoundedK8sResourceResultCapsMergedGitOpsRows(t *testing.T) {
	t.Parallel()

	contentRows := make([]map[string]any, 0, serviceStoryItemLimit-1)
	for index := range serviceStoryItemLimit - 1 {
		contentRows = append(contentRows, map[string]any{
			"entity_id":     fmt.Sprintf("content-%02d", index),
			"repo_id":       "repo-service",
			"relative_path": fmt.Sprintf("deploy/%02d.yaml", index),
		})
	}
	gitOpsRows := []map[string]any{
		{"entity_id": "gitops-a", "repo_id": "repo-deploy", "relative_path": "apps/a.yaml"},
		{"entity_id": "gitops-b", "repo_id": "repo-deploy", "relative_path": "apps/b.yaml"},
	}

	result := boundedK8sResourceResult(contentRows, false, gitOpsRows, false, false)
	if got, want := len(result.rows), serviceStoryItemLimit; got != want {
		t.Fatalf("len(boundedK8sResourceResult().rows) = %d, want %d", got, want)
	}
	if got, want := IntVal(result.limits, "observed_count"), serviceStoryItemLimit+1; got != want {
		t.Fatalf("k8s_resource_limits.observed_count = %d, want %d", got, want)
	}
	if !BoolVal(result.limits, "truncated") {
		t.Fatalf("k8s_resource_limits = %#v, want merged-source truncation", result.limits)
	}
	if BoolVal(result.limits, "observed_count_is_lower_bound") {
		t.Fatalf("k8s_resource_limits = %#v, merged rows were fully observed", result.limits)
	}
	if got, want := IntVal(result.limits, "content_observed_count"), serviceStoryItemLimit-1; got != want {
		t.Fatalf("k8s_resource_limits.content_observed_count = %d, want %d", got, want)
	}
	if got, want := IntVal(result.limits, "deployment_source_observed_count"), len(gitOpsRows); got != want {
		t.Fatalf("k8s_resource_limits.deployment_source_observed_count = %d, want %d", got, want)
	}
}

func TestBoundedK8sResourceResultPropagatesDeploymentSourceLowerBound(t *testing.T) {
	t.Parallel()

	result := boundedK8sResourceResult(nil, false, []map[string]any{{
		"entity_id":     "gitops-a",
		"repo_id":       "repo-deploy",
		"relative_path": "apps/a.yaml",
	}}, true, false)
	if !BoolVal(result.limits, "observed_count_is_lower_bound") || !BoolVal(result.limits, "truncated") {
		t.Fatalf("k8s_resource_limits = %#v, deployment-source saturation must fail completeness closed", result.limits)
	}
	if !BoolVal(result.limits, "deployment_source_observed_count_is_lower_bound") {
		t.Fatalf("k8s_resource_limits = %#v, want deployment-source lower-bound disclosure", result.limits)
	}
}

func TestBuildDeploymentTraceResponseIncludesK8sResourceLimits(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		"limit":                         serviceStoryItemLimit,
		"query_sentinel_limit":          serviceStoryItemLimit + 1,
		"returned_count":                serviceStoryItemLimit,
		"observed_count":                serviceStoryItemLimit + 1,
		"observed_count_is_lower_bound": true,
		"truncated":                     true,
	}
	got := buildDeploymentTraceResponse("payments-api", map[string]any{
		"id":                  "workload:payments-api",
		"name":                "payments-api",
		"k8s_resource_limits": want,
	})
	if gotLimits := mapValue(got, "k8s_resource_limits"); len(gotLimits) == 0 {
		t.Fatal("buildDeploymentTraceResponse() omitted k8s_resource_limits")
	}
}
