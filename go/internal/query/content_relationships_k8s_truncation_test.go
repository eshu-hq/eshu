// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"testing"
)

// boundedK8sFakeContentStore models ListRepoEntitiesByType exactly like the
// production SQL: it applies `entity_type = $2` first, then `LIMIT $3`
// (mirroring content_reader_by_type.go), preserving rows slice order (the
// tests below control ordering directly, standing in for
// `ORDER BY relative_path, start_line, entity_id`). Unlike
// truncationFakeContentStore (which models the OLD untyped-limit regime),
// this fake exists purely to prove fetchK8sResourceCandidates' own +1/limit
// truncation-disclosure slicing (#5343 review P2). If failOnCall is set, the
// fake fails the test outright -- used to prove the selectorless fast path
// never reaches the candidate fetch at all.
type boundedK8sFakeContentStore struct {
	fakePortContentStore
	rows       []EntityContent
	t          *testing.T
	failOnCall bool
}

func (f boundedK8sFakeContentStore) ListRepoEntitiesByType(_ context.Context, _, entityType string, limit int) ([]EntityContent, error) {
	if f.failOnCall {
		f.t.Fatalf("ListRepoEntitiesByType called, want no call (selectorless fast path must return before the fetch)")
	}
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

// k8sResourceFillerEntities returns `count` K8sResource Deployment rows in
// namespace "other-ns" -- distinct from the "prod" namespace the truncation
// tests below use for the real Service/Deployment pair -- so they occupy the
// typed candidate scan without ever matching SELECTS themselves.
func k8sResourceFillerEntities(count int) []EntityContent {
	filler := make([]EntityContent, 0, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("filler-deploy-%05d", i)
		filler = append(filler, EntityContent{
			EntityID:     name,
			RepoID:       "repo-1",
			RelativePath: "deploy/" + name + ".yaml",
			EntityType:   "K8sResource",
			EntityName:   name,
			Metadata: map[string]any{
				"kind":                "Deployment",
				"namespace":           "other-ns",
				"qualified_name":      "other-ns/Deployment/" + name,
				"pod_template_labels": "app=" + name,
			},
		})
	}
	return filler
}

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

	relationships, ok, truncated, err := buildOutgoingK8sSelectRelationships(context.Background(), reader, service, nil)
	if err != nil {
		t.Fatalf("buildOutgoingK8sSelectRelationships() error = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("buildOutgoingK8sSelectRelationships() ok = false, want true")
	}
	if truncated {
		t.Fatalf("buildOutgoingK8sSelectRelationships() truncated = true, want false (candidate count is within the limit)")
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

	relationships, ok, truncated, err := buildIncomingK8sSelectRelationships(context.Background(), reader, deployment, nil)
	if err != nil {
		t.Fatalf("buildIncomingK8sSelectRelationships() error = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("buildIncomingK8sSelectRelationships() ok = false, want true")
	}
	if truncated {
		t.Fatalf("buildIncomingK8sSelectRelationships() truncated = true, want false (candidate count is within the limit)")
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

// TestBuildOutgoingK8sSelectRelationshipsTruncatesAtLimit is the FIX 2
// truncation-disclosure regression for the outgoing (Service -> Deployment)
// builder: repositorySemanticEntityLimit non-matching K8sResource Deployment
// rows fill the typed candidate scan, followed by ONE real matching
// Deployment as the (limit+1)-th row. fetchK8sResourceCandidates requests
// limit+1, sees repositorySemanticEntityLimit+1 rows come back, and must (a)
// set truncated=true and (b) drop the overflow row before matching runs --
// so the real Deployment, having landed past the cutoff, must NOT appear in
// the results. This proves both the disclosure flag and the actual capping
// behavior, not just the flag in isolation.
func TestBuildOutgoingK8sSelectRelationshipsTruncatesAtLimit(t *testing.T) {
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

	reader := boundedK8sFakeContentStore{rows: candidates}
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

	relationships, ok, truncated, err := buildOutgoingK8sSelectRelationships(context.Background(), reader, service, nil)
	if err != nil {
		t.Fatalf("buildOutgoingK8sSelectRelationships() error = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("buildOutgoingK8sSelectRelationships() ok = false, want true")
	}
	if !truncated {
		t.Fatalf("buildOutgoingK8sSelectRelationships() truncated = false, want true (%d candidates exceed the %d limit)",
			len(candidates), repositorySemanticEntityLimit)
	}
	if len(relationships) != 0 {
		t.Fatalf("len(relationships) = %d, want 0 (the matching deployment landed past the cutoff and must be dropped): %#v",
			len(relationships), relationships)
	}
}

// TestBuildIncomingK8sSelectRelationshipsTruncatesAtLimit is the symmetric
// incoming (Deployment -> Service) counterpart to
// TestBuildOutgoingK8sSelectRelationshipsTruncatesAtLimit.
func TestBuildIncomingK8sSelectRelationshipsTruncatesAtLimit(t *testing.T) {
	t.Parallel()

	candidates := make([]EntityContent, 0, repositorySemanticEntityLimit+1)
	for i := 0; i < repositorySemanticEntityLimit; i++ {
		name := fmt.Sprintf("filler-service-%05d", i)
		candidates = append(candidates, EntityContent{
			EntityID:     name,
			RepoID:       "repo-1",
			RelativePath: "deploy/" + name + ".yaml",
			EntityType:   "K8sResource",
			EntityName:   name,
			Metadata: map[string]any{
				"kind":           "Service",
				"namespace":      "other-ns",
				"qualified_name": "other-ns/Service/" + name,
				"selector":       "app=" + name,
			},
		})
	}
	candidates = append(candidates, EntityContent{
		EntityID:     "service-overflow",
		RepoID:       "repo-1",
		RelativePath: "deploy/zzz-overflow.yaml",
		EntityType:   "K8sResource",
		EntityName:   "overflow-service",
		Metadata: map[string]any{
			"kind":           "Service",
			"namespace":      "prod",
			"qualified_name": "prod/Service/overflow-service",
			"selector":       "app=frontend",
		},
	})

	reader := boundedK8sFakeContentStore{rows: candidates}
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

	relationships, ok, truncated, err := buildIncomingK8sSelectRelationships(context.Background(), reader, deployment, nil)
	if err != nil {
		t.Fatalf("buildIncomingK8sSelectRelationships() error = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("buildIncomingK8sSelectRelationships() ok = false, want true")
	}
	if !truncated {
		t.Fatalf("buildIncomingK8sSelectRelationships() truncated = false, want true (%d candidates exceed the %d limit)",
			len(candidates), repositorySemanticEntityLimit)
	}
	if len(relationships) != 0 {
		t.Fatalf("len(relationships) = %d, want 0 (the matching service landed past the cutoff and must be dropped): %#v",
			len(relationships), relationships)
	}
}

// TestBuildOutgoingK8sSelectRelationshipsNotTruncatedBelowLimit proves the
// truncated flag stays false when the true K8sResource candidate count is
// under the limit -- a repo with exactly repositorySemanticEntityLimit
// K8sResource rows (not one more) must not be falsely flagged as truncated.
func TestBuildOutgoingK8sSelectRelationshipsNotTruncatedBelowLimit(t *testing.T) {
	t.Parallel()

	candidates := k8sResourceFillerEntities(repositorySemanticEntityLimit - 1)
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
	if len(candidates) != repositorySemanticEntityLimit {
		t.Fatalf("test setup: len(candidates) = %d, want exactly %d", len(candidates), repositorySemanticEntityLimit)
	}

	reader := boundedK8sFakeContentStore{rows: candidates}
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

	relationships, ok, truncated, err := buildOutgoingK8sSelectRelationships(context.Background(), reader, service, nil)
	if err != nil {
		t.Fatalf("buildOutgoingK8sSelectRelationships() error = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("buildOutgoingK8sSelectRelationships() ok = false, want true")
	}
	if truncated {
		t.Fatalf("buildOutgoingK8sSelectRelationships() truncated = true, want false (candidate count == limit exactly, not over it)")
	}
	if len(relationships) != 1 {
		t.Fatalf("len(relationships) = %d, want 1 (the matching deployment is within the limit): %#v", len(relationships), relationships)
	}
}

// TestBuildOutgoingK8sSelectRelationshipsSelectorlessFastPathNeverTruncates
// proves the genuinely-selectorless Service fast path (selectorPresent &&
// selector == "") returns before the candidate fetch ever runs, so truncated
// stays false and the fake fails the test if the fetch is reached at all --
// there is no candidate scan to truncate on this path.
func TestBuildOutgoingK8sSelectRelationshipsSelectorlessFastPathNeverTruncates(t *testing.T) {
	t.Parallel()

	reader := boundedK8sFakeContentStore{t: t, failOnCall: true}
	service := EntityContent{
		EntityID:     "service-1",
		RepoID:       "repo-1",
		RelativePath: "deploy/service.yaml",
		EntityType:   "K8sResource",
		EntityName:   "external",
		Metadata: map[string]any{
			"kind":           "Service",
			"namespace":      "prod",
			"qualified_name": "prod/Service/external",
			"selector":       "",
		},
	}

	relationships, ok, truncated, err := buildOutgoingK8sSelectRelationships(context.Background(), reader, service, nil)
	if err != nil {
		t.Fatalf("buildOutgoingK8sSelectRelationships() error = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("buildOutgoingK8sSelectRelationships() ok = false, want true")
	}
	if truncated {
		t.Fatalf("buildOutgoingK8sSelectRelationships() truncated = true, want false (selectorless fast path never fetches candidates)")
	}
	if len(relationships) != 0 {
		t.Fatalf("len(relationships) = %d, want 0 (selectorless Service selects nothing)", len(relationships))
	}
}
