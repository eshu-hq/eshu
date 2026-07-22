// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"
)

// entityContextFakeContentStore backs a single GetEntityContent lookup plus
// a controllable ListRepoEntitiesByType, so getEntityContextFromContent (the
// response-assembly layer entity_context_content.go) can be exercised
// end-to-end through buildContentRelationshipSet without a real Postgres
// content store.
type entityContextFakeContentStore struct {
	fakePortContentStore
	entity *EntityContent
	rows   []EntityContent
}

func (f entityContextFakeContentStore) GetEntityContent(_ context.Context, entityID string) (*EntityContent, error) {
	if f.entity != nil && f.entity.EntityID == entityID {
		found := *f.entity
		return &found, nil
	}
	return nil, nil
}

func (f entityContextFakeContentStore) ListRepoEntitiesByType(_ context.Context, _, entityType string, limit int) ([]EntityContent, error) {
	filtered := make([]EntityContent, 0, len(f.rows))
	for _, entity := range f.rows {
		if entity.EntityType != entityType {
			continue
		}
		filtered = append(filtered, entity)
	}
	if limit > 0 && limit < len(filtered) {
		return filtered[:limit], nil
	}
	return filtered, nil
}

// TestGetEntityContextFromContentDisclosesK8sSelectTruncation is the FIX 2
// response-assembly regression: when the k8s SELECTS candidate scan
// truncates (see TestBuildOutgoingK8sSelectRelationshipsTruncatesAtLimit for
// the builder-level proof), getEntityContextFromContent must surface that on
// the response as relationships_complete=false plus the machine-readable
// reason, reusing the existing impact-query complete/coverage vocabulary
// rather than inventing new words.
func TestGetEntityContextFromContentDisclosesK8sSelectTruncation(t *testing.T) {
	t.Parallel()

	candidates := k8sResourceFillerEntities(repositorySemanticEntityLimit)
	candidates = append(candidates, EntityContent{
		EntityID:     "deployment-overflow",
		RepoID:       "repo-1",
		RelativePath: "deploy/zzz-overflow.yaml",
		EntityType:   "K8sResource",
		EntityName:   "overflow-deploy",
		Metadata: map[string]any{
			"kind":                "Deployment",
			"namespace":           "prod",
			"qualified_name":      "prod/Deployment/overflow-deploy",
			"pod_template_labels": "app=frontend",
		},
	})

	service := EntityContent{
		EntityID:     "service-1",
		RepoID:       "repo-1",
		RelativePath: "deploy/service.yaml",
		EntityType:   "K8sResource",
		EntityName:   "web",
		Metadata: map[string]any{
			"kind":           "Service",
			"namespace":      "prod",
			"qualified_name": "prod/Service/web",
			"selector":       "app=frontend",
		},
	}

	handler := &EntityHandler{Content: entityContextFakeContentStore{entity: &service, rows: candidates}}
	response, err := handler.getEntityContextFromContent(context.Background(), "service-1")
	if err != nil {
		t.Fatalf("getEntityContextFromContent() error = %v, want nil", err)
	}
	if response == nil {
		t.Fatalf("getEntityContextFromContent() response = nil, want non-nil")
	}
	if got, want := response["relationships_complete"], false; got != want {
		t.Fatalf("response[relationships_complete] = %#v, want %#v", got, want)
	}
	if got, want := response["relationships_truncation_reason"], k8sSelectCandidateScanTruncationReason; got != want {
		t.Fatalf("response[relationships_truncation_reason] = %#v, want %#v", got, want)
	}
}

// TestGetEntityContextFromContentOmitsTruncationFieldsBelowLimit proves the
// disclosure fields are emitted ONLY when truncation actually occurs -- a
// repo whose K8sResource candidate count is within the limit must get a
// response with neither key present, so every existing cassette and the
// B-12 20-repo snapshot stay byte-identical.
func TestGetEntityContextFromContentOmitsTruncationFieldsBelowLimit(t *testing.T) {
	t.Parallel()

	service := EntityContent{
		EntityID:     "service-1",
		RepoID:       "repo-1",
		RelativePath: "deploy/service.yaml",
		EntityType:   "K8sResource",
		EntityName:   "web",
		Metadata: map[string]any{
			"kind":           "Service",
			"namespace":      "prod",
			"qualified_name": "prod/Service/web",
			"selector":       "app=frontend",
		},
	}
	deployment := EntityContent{
		EntityID:     "deployment-1",
		RepoID:       "repo-1",
		RelativePath: "deploy/frontend.yaml",
		EntityType:   "K8sResource",
		EntityName:   "frontend-deploy",
		Metadata: map[string]any{
			"kind":                "Deployment",
			"namespace":           "prod",
			"qualified_name":      "prod/Deployment/frontend-deploy",
			"pod_template_labels": "app=frontend",
		},
	}

	handler := &EntityHandler{Content: entityContextFakeContentStore{entity: &service, rows: []EntityContent{deployment}}}
	response, err := handler.getEntityContextFromContent(context.Background(), "service-1")
	if err != nil {
		t.Fatalf("getEntityContextFromContent() error = %v, want nil", err)
	}
	if _, ok := response["relationships_complete"]; ok {
		t.Fatalf("response[relationships_complete] present = %#v, want absent (not truncated)", response["relationships_complete"])
	}
	if _, ok := response["relationships_truncation_reason"]; ok {
		t.Fatalf("response[relationships_truncation_reason] present = %#v, want absent (not truncated)", response["relationships_truncation_reason"])
	}
}

func TestGetEntityContextFromContentDisclosesTruncatedWorkflowSource(t *testing.T) {
	t.Parallel()

	workflow := EntityContent{
		EntityID:     "workflow-1",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/ci.yml",
		EntityType:   "File",
		EntityName:   "ci",
		Metadata: map[string]any{
			"source_cache_truncated":      true,
			"source_cache_original_bytes": 40000,
			"source_cache_limit_bytes":    32768,
		},
	}

	handler := &EntityHandler{Content: entityContextFakeContentStore{entity: &workflow}}
	response, err := handler.getEntityContextFromContent(context.Background(), workflow.EntityID)
	if err != nil {
		t.Fatalf("getEntityContextFromContent() error = %v, want nil", err)
	}
	if got, want := response["relationships_complete"], false; got != want {
		t.Fatalf("relationships_complete = %#v, want %#v", got, want)
	}
	if got, want := response["relationships_truncation_reason"], githubActionsSourceCacheTruncationReason; got != want {
		t.Fatalf("relationships_truncation_reason = %#v, want %#v", got, want)
	}
	if got := contextPartialReasons(response); len(got) != 1 || got[0] != githubActionsSourceCacheTruncationReason {
		t.Fatalf("partial_reasons = %#v, want [%q]", got, githubActionsSourceCacheTruncationReason)
	}
	limits := entityContextResultLimits(response, workflow.EntityID)
	if truncated, _ := limits["truncated"].(bool); !truncated {
		t.Fatal("result_limits.truncated = false, want true for incomplete workflow relationship truth")
	}
}
