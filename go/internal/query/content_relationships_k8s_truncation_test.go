// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"testing"
)

// truncationFakeContentStore proves the P2-1 remediation for #5343: it models
// the exact truncation regime a repo-wide ListRepoEntities(limit) creates for
// a rare entity type. orderedCandidates is the full pre-filter candidate set
// in the same relative_path/start_line order Postgres would return. Both
// methods slice from that single ordered list, but at a different point in
// the pipeline -- ListRepoEntities truncates BEFORE any entity_type
// filtering (mirroring the old repo-wide SQL scan), while
// ListRepoEntitiesByType filters to entityType FIRST and only then applies
// limit (mirroring the new typed SQL predicate). That difference is the
// entire theory under test.
type truncationFakeContentStore struct {
	fakePortContentStore
	orderedCandidates []EntityContent
}

func (t truncationFakeContentStore) ListRepoEntities(_ context.Context, _ string, limit int) ([]EntityContent, error) {
	if limit > 0 && limit < len(t.orderedCandidates) {
		return append([]EntityContent(nil), t.orderedCandidates[:limit]...), nil
	}
	return append([]EntityContent(nil), t.orderedCandidates...), nil
}

func (t truncationFakeContentStore) ListRepoEntitiesByType(_ context.Context, _, entityType string, limit int) ([]EntityContent, error) {
	filtered := make([]EntityContent, 0, len(t.orderedCandidates))
	for _, entity := range t.orderedCandidates {
		if entity.EntityType != entityType {
			continue
		}
		filtered = append(filtered, entity)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

// nonK8sFillerEntities returns `count` non-K8sResource EntityContent rows that
// sort ahead of any K8sResource row appended after them, filling the fetch
// horizon so a repo-wide LIMIT truncates before reaching the real candidate.
func nonK8sFillerEntities(count int) []EntityContent {
	filler := make([]EntityContent, 0, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("filler-%05d", i)
		filler = append(filler, EntityContent{
			EntityID:     name,
			RepoID:       "repo-1",
			RelativePath: "src/" + name + ".go",
			EntityType:   "Function",
			EntityName:   name,
		})
	}
	return filler
}

// TestBuildContentRelationshipSetK8sServiceSelectsRecoversDeploymentPastTruncationHorizon
// is the P2-1 regression: repositorySemanticEntityLimit (5000) non-K8sResource
// rows sort ahead of the one matching Deployment. The old
// ListRepoEntities(repositorySemanticEntityLimit) full-scan truncates BEFORE
// the K8sResource filter ever runs, so the Deployment never enters the
// candidate set and the SELECTS edge is silently dropped -- this is the
// latent false negative #5343's code review flagged. This test FAILS if the
// outgoing builder calls ListRepoEntities instead of ListRepoEntitiesByType.
func TestBuildContentRelationshipSetK8sServiceSelectsRecoversDeploymentPastTruncationHorizon(t *testing.T) {
	t.Parallel()

	candidates := nonK8sFillerEntities(repositorySemanticEntityLimit)
	candidates = append(candidates, EntityContent{
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
	})

	reader := truncationFakeContentStore{orderedCandidates: candidates}
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

	relationships, ok, err := buildOutgoingK8sSelectRelationships(context.Background(), reader, service)
	if err != nil {
		t.Fatalf("buildOutgoingK8sSelectRelationships() error = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("buildOutgoingK8sSelectRelationships() ok = false, want true")
	}
	if len(relationships) != 1 {
		t.Fatalf("len(relationships) = %d, want 1 (deployment-1 must survive the %d-row fetch horizon): %#v",
			len(relationships), repositorySemanticEntityLimit, relationships)
	}
	if got, want := relationships[0]["target_id"], "deployment-1"; got != want {
		t.Fatalf("relationships[0][target_id] = %#v, want %#v", got, want)
	}
	if got, want := relationships[0]["reason"], "k8s_service_selector_match"; got != want {
		t.Fatalf("relationships[0][reason] = %#v, want %#v", got, want)
	}
}

// TestBuildContentRelationshipSetK8sDeploymentRecoversIncomingServicePastTruncationHorizon
// is the symmetric incoming-side counterpart: it proves
// buildIncomingK8sSelectRelationships was switched to ListRepoEntitiesByType
// too. If only the outgoing builder were switched, a repo with more than
// repositorySemanticEntityLimit entities would silently produce an
// asymmetric edge (visible outgoing from the Service, missing incoming on
// the Deployment) -- exactly the drift the P2-1 fix must not introduce. This
// test FAILS if the incoming builder calls ListRepoEntities instead of
// ListRepoEntitiesByType.
func TestBuildContentRelationshipSetK8sDeploymentRecoversIncomingServicePastTruncationHorizon(t *testing.T) {
	t.Parallel()

	candidates := nonK8sFillerEntities(repositorySemanticEntityLimit)
	candidates = append(candidates, EntityContent{
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
	})

	reader := truncationFakeContentStore{orderedCandidates: candidates}
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

	relationships, ok, err := buildIncomingK8sSelectRelationships(context.Background(), reader, deployment)
	if err != nil {
		t.Fatalf("buildIncomingK8sSelectRelationships() error = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("buildIncomingK8sSelectRelationships() ok = false, want true")
	}
	if len(relationships) != 1 {
		t.Fatalf("len(relationships) = %d, want 1 (service-1 must survive the %d-row fetch horizon): %#v",
			len(relationships), repositorySemanticEntityLimit, relationships)
	}
	if got, want := relationships[0]["source_id"], "service-1"; got != want {
		t.Fatalf("relationships[0][source_id] = %#v, want %#v", got, want)
	}
	if got, want := relationships[0]["reason"], "k8s_service_selector_match"; got != want {
		t.Fatalf("relationships[0][reason] = %#v, want %#v", got, want)
	}
}
