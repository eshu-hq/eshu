// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// These tests exercise the #5363 anchored directed match: the impact-trace
// k8s SELECTS surfaced pool is widened so a differently-named Service that
// genuinely selector-matches the traced Deployment is discovered, while every
// unmatched candidate stays off the wire and the #5343 tri-state
// false-positive-masking guards are preserved exactly.

// k8sSelectWideningStore is a ContentStore double whose name-anchored
// SearchEntitiesByName returns exact-name K8sResource rows from entities (the
// surfaced pool), while the embedded fakePortContentStore serves the narrow
// candidate scan and the by-ID hydration from the same entities set. One
// fixture set therefore drives all three fetches consistently.
type k8sSelectWideningStore struct {
	fakePortContentStore
}

func newK8sSelectWideningStore(entities []EntityContent) k8sSelectWideningStore {
	return k8sSelectWideningStore{fakePortContentStore: fakePortContentStore{entities: entities}}
}

func (s k8sSelectWideningStore) SearchEntitiesByName(_ context.Context, repoID, entityType, name string, limit int) ([]EntityContent, error) {
	out := make([]EntityContent, 0)
	for _, entity := range s.entities {
		if repoID != "" && entity.RepoID != "" && entity.RepoID != repoID {
			continue
		}
		if entityType != "" && entity.EntityType != entityType {
			continue
		}
		if entity.EntityName != name {
			continue
		}
		out = append(out, entity)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// k8sEntity builds a K8sResource EntityContent in repo-1. metaStrings entries
// are added to the metadata map verbatim, so omitting "selector" leaves the key
// absent (the tri-state "unknown" case), whereas including it with "" is the
// distinct "known empty" case.
func k8sEntity(id, name, path, kind, namespace string, metaStrings map[string]string) EntityContent {
	metadata := map[string]any{
		"kind":           kind,
		"namespace":      namespace,
		"qualified_name": namespace + "/" + kind + "/" + name,
	}
	for key, value := range metaStrings {
		metadata[key] = value
	}
	return EntityContent{
		EntityID:     id,
		EntityType:   "K8sResource",
		EntityName:   name,
		RepoID:       "repo-1",
		RelativePath: path,
		Metadata:     metadata,
	}
}

func surfacedEntityIDs(rows []map[string]any) map[string]struct{} {
	ids := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		ids[StringVal(row, "entity_id")] = struct{}{}
	}
	return ids
}

func hasSelectsEdge(relationships []map[string]any, sourceID, targetID, reason string) bool {
	for _, rel := range relationships {
		if StringVal(rel, "type") != "SELECTS" {
			continue
		}
		if StringVal(rel, "source_id") == sourceID &&
			StringVal(rel, "target_id") == targetID &&
			StringVal(rel, "reason") == reason {
			return true
		}
	}
	return false
}

// Test 1 -- under-linking regression (failing-then-green). A Deployment "web"
// and a differently-named Service "web-svc" whose selector is a subset of the
// Deployment's pod-template labels, same namespace: the Service must surface in
// k8s_resources AND a SELECTS edge must be produced. This FAILS on main, where
// the name-anchored fetch never returns the differently-named Service.
func TestImpactTraceK8sSelectWideningUnderLinkingRegression(t *testing.T) {
	t.Parallel()

	deployment := k8sEntity("dep-web", "web", "deploy/web.yaml", "Deployment", "prod", map[string]string{
		"pod_template_labels": "app=web,tier=api",
	})
	service := k8sEntity("svc-web", "web-svc", "svc/web.yaml", "Service", "prod", map[string]string{
		"selector": "app=web",
	})

	handler := &ImpactHandler{Content: newK8sSelectWideningStore([]EntityContent{deployment, service})}
	result, err := handler.fetchK8sResourceResult(context.Background(), "repo-1", "web")
	if err != nil {
		t.Fatalf("fetchK8sResourceResult() error = %v", err)
	}

	surfaced := surfacedEntityIDs(result.rows)
	if _, ok := surfaced["svc-web"]; !ok {
		t.Fatalf("selector-matching Service web-svc not surfaced; rows = %#v", result.rows)
	}
	if _, ok := surfaced["dep-web"]; !ok {
		t.Fatalf("anchored Deployment web not surfaced; rows = %#v", result.rows)
	}

	relationships := buildK8sRelationships(result.rows)
	if !hasSelectsEdge(relationships, "svc-web", "dep-web", k8sSelectReasonSelectorMatch) {
		t.Fatalf("missing SELECTS edge web-svc -> web (selector match); relationships = %#v", relationships)
	}
	if got, want := BoolVal(result.limits, "k8s_relationships_complete"), true; got != want {
		t.Fatalf("k8s_relationships_complete = %v, want %v", got, want)
	}
}

// Test 2 -- #5343 false-positive non-regression. A same-named Service with a
// known, non-matching selector produces NO edge and does NOT slip in via the
// widened pool: the authoritative selector governs, the name fallback is
// unreachable.
func TestImpactTraceK8sSelectWideningRespectsNonMatchingSelector(t *testing.T) {
	t.Parallel()

	deployment := k8sEntity("dep-web", "web", "deploy/web.yaml", "Deployment", "prod", map[string]string{
		"pod_template_labels": "app=web,tier=api",
	})
	// Same name as the workload, but a selector that is NOT a subset: the
	// #5343 masking guard means no name-fallback rescue.
	service := k8sEntity("svc-web", "web", "svc/web.yaml", "Service", "prod", map[string]string{
		"selector": "app=other",
	})

	handler := &ImpactHandler{Content: newK8sSelectWideningStore([]EntityContent{deployment, service})}
	result, err := handler.fetchK8sResourceResult(context.Background(), "repo-1", "web")
	if err != nil {
		t.Fatalf("fetchK8sResourceResult() error = %v", err)
	}

	relationships := buildK8sRelationships(result.rows)
	for _, rel := range relationships {
		if StringVal(rel, "type") == "SELECTS" {
			t.Fatalf("unexpected SELECTS edge for non-matching selector: %#v", rel)
		}
	}
}

// Test 3 -- namespace strictness. Selector matches but the namespaces differ:
// no edge, and the candidate never surfaces.
func TestImpactTraceK8sSelectWideningEnforcesNamespaceEquality(t *testing.T) {
	t.Parallel()

	deployment := k8sEntity("dep-web", "web", "deploy/web.yaml", "Deployment", "prod", map[string]string{
		"pod_template_labels": "app=web,tier=api",
	})
	service := k8sEntity("svc-web", "web-svc", "svc/web.yaml", "Service", "staging", map[string]string{
		"selector": "app=web",
	})

	handler := &ImpactHandler{Content: newK8sSelectWideningStore([]EntityContent{deployment, service})}
	result, err := handler.fetchK8sResourceResult(context.Background(), "repo-1", "web")
	if err != nil {
		t.Fatalf("fetchK8sResourceResult() error = %v", err)
	}

	if _, ok := surfacedEntityIDs(result.rows)["svc-web"]; ok {
		t.Fatalf("cross-namespace Service must not surface; rows = %#v", result.rows)
	}
	relationships := buildK8sRelationships(result.rows)
	if hasSelectsEdge(relationships, "svc-web", "dep-web", k8sSelectReasonSelectorMatch) {
		t.Fatalf("cross-namespace SELECTS edge produced; relationships = %#v", relationships)
	}
}

// Test 4 -- pool purity at scale. 5000 candidate Services, exactly 2 of which
// selector-match: only those 2 (plus the anchored Deployment) join
// k8s_resources, and no unmatched candidate appears ANYWHERE in the marshaled
// surface.
func TestImpactTraceK8sSelectWideningPoolPurity(t *testing.T) {
	t.Parallel()

	deployment := k8sEntity("dep-web", "web", "deploy/web.yaml", "Deployment", "prod", map[string]string{
		"pod_template_labels": "app=web,tier=api",
	})
	entities := []EntityContent{deployment}
	matchIDs := map[string]struct{}{"svc-match-1": {}, "svc-match-2": {}}
	for i := range 5000 {
		id := fmt.Sprintf("svc-noise-%04d", i)
		selector := fmt.Sprintf("app=noise-%04d", i)
		name := fmt.Sprintf("noise-svc-%04d", i)
		if i == 1000 {
			id, name, selector = "svc-match-1", "frontend", "app=web"
		}
		if i == 4000 {
			id, name, selector = "svc-match-2", "gateway", "app=web,tier=api"
		}
		entities = append(entities, k8sEntity(id, name, fmt.Sprintf("svc/%s.yaml", id), "Service", "prod", map[string]string{
			"selector": selector,
		}))
	}

	handler := &ImpactHandler{Content: newK8sSelectWideningStore(entities)}
	result, err := handler.fetchK8sResourceResult(context.Background(), "repo-1", "web")
	if err != nil {
		t.Fatalf("fetchK8sResourceResult() error = %v", err)
	}

	surfaced := surfacedEntityIDs(result.rows)
	if len(surfaced) != 3 {
		t.Fatalf("surfaced %d rows, want exactly 3 (deployment + 2 matches); ids = %v", len(surfaced), surfaced)
	}
	for id := range matchIDs {
		if _, ok := surfaced[id]; !ok {
			t.Fatalf("matched Service %s not surfaced; ids = %v", id, surfaced)
		}
	}

	marshaled, err := json.Marshal(result.rows)
	if err != nil {
		t.Fatalf("marshal rows: %v", err)
	}
	if strings.Contains(string(marshaled), "svc-noise-") || strings.Contains(string(marshaled), "noise-svc-") {
		t.Fatalf("unmatched candidate leaked into the surfaced pool: %s", marshaled)
	}

	relationships := buildK8sRelationships(result.rows)
	selectsCount := 0
	for _, rel := range relationships {
		if StringVal(rel, "type") == "SELECTS" {
			selectsCount++
		}
	}
	if selectsCount != 2 {
		t.Fatalf("SELECTS edge count = %d, want 2; relationships = %#v", selectsCount, relationships)
	}
}

// Test 5a -- tri-state widening safety: a candidate whose selector key is
// ABSENT must never be pulled into the widened pool via the name+namespace
// fallback, because widening candidates are differently named by construction.
func TestImpactTraceK8sSelectWideningSelectorAbsentNeverWidens(t *testing.T) {
	t.Parallel()

	deployment := k8sEntity("dep-web", "web", "deploy/web.yaml", "Deployment", "prod", map[string]string{
		"pod_template_labels": "app=web,tier=api",
	})
	// selector key absent (vintage row) AND a different name: no authoritative
	// selector, and the name fallback cannot fire across different names.
	service := k8sEntity("svc-web", "web-svc", "svc/web.yaml", "Service", "prod", nil)

	handler := &ImpactHandler{Content: newK8sSelectWideningStore([]EntityContent{deployment, service})}
	result, err := handler.fetchK8sResourceResult(context.Background(), "repo-1", "web")
	if err != nil {
		t.Fatalf("fetchK8sResourceResult() error = %v", err)
	}
	if _, ok := surfacedEntityIDs(result.rows)["svc-web"]; ok {
		t.Fatalf("selector-absent, differently-named Service must not widen in; rows = %#v", result.rows)
	}
}

// Test 5b -- mixed-vintage drop: the anchored Deployment predates
// pod_template_labels capture (key absent), so a selector-present Service is
// dropped with no edge and no surface, even though the selector is otherwise
// eligible.
func TestImpactTraceK8sSelectWideningMixedVintageDrops(t *testing.T) {
	t.Parallel()

	// Deployment carries no pod_template_labels key (vintage).
	deployment := k8sEntity("dep-web", "web", "deploy/web.yaml", "Deployment", "prod", nil)
	service := k8sEntity("svc-web", "web-svc", "svc/web.yaml", "Service", "prod", map[string]string{
		"selector": "app=web",
	})

	handler := &ImpactHandler{Content: newK8sSelectWideningStore([]EntityContent{deployment, service})}
	result, err := handler.fetchK8sResourceResult(context.Background(), "repo-1", "web")
	if err != nil {
		t.Fatalf("fetchK8sResourceResult() error = %v", err)
	}
	if _, ok := surfacedEntityIDs(result.rows)["svc-web"]; ok {
		t.Fatalf("mixed-vintage candidate must not surface; rows = %#v", result.rows)
	}
	relationships := buildK8sRelationships(result.rows)
	for _, rel := range relationships {
		if StringVal(rel, "type") == "SELECTS" {
			t.Fatalf("mixed-vintage produced a SELECTS edge: %#v", rel)
		}
	}
}

// Test 6 -- frozen sub-surface. A repo with no new selector match surfaces
// exactly the name-anchored rows: the surfaced pool, image_refs, and the
// pre-existing limit-map values are unchanged, and the new completeness keys
// report a complete (non-truncated) scan.
func TestImpactTraceK8sSelectWideningFrozenSubSurfaceOnNoMatch(t *testing.T) {
	t.Parallel()

	deployment := k8sEntity("dep-web", "web", "deploy/web.yaml", "Deployment", "prod", map[string]string{
		"pod_template_labels": "app=web,tier=api",
		"container_images":    "", // no images
	})
	// A Service that does not match (different selector) and is differently
	// named: it must not perturb the surfaced pool at all.
	service := k8sEntity("svc-web", "web-svc", "svc/web.yaml", "Service", "prod", map[string]string{
		"selector": "app=elsewhere",
	})

	handler := &ImpactHandler{Content: newK8sSelectWideningStore([]EntityContent{deployment, service})}
	result, err := handler.fetchK8sResourceResult(context.Background(), "repo-1", "web")
	if err != nil {
		t.Fatalf("fetchK8sResourceResult() error = %v", err)
	}

	surfaced := surfacedEntityIDs(result.rows)
	if len(surfaced) != 1 {
		t.Fatalf("no-match repo surfaced %d rows, want 1 (anchored Deployment only); ids = %v", len(surfaced), surfaced)
	}
	if _, ok := surfaced["dep-web"]; !ok {
		t.Fatalf("anchored Deployment missing; rows = %#v", result.rows)
	}
	if len(result.imageRefs) != 0 {
		t.Fatalf("image_refs = %#v, want empty (frozen)", result.imageRefs)
	}
	// Pre-existing limit-map values are unchanged.
	if got, want := IntVal(result.limits, "limit"), serviceStoryItemLimit; got != want {
		t.Fatalf("limit = %d, want %d", got, want)
	}
	if got, want := IntVal(result.limits, "returned_count"), 1; got != want {
		t.Fatalf("returned_count = %d, want %d", got, want)
	}
	if BoolVal(result.limits, "truncated") {
		t.Fatalf("truncated = true, want false on a fully observed no-match repo")
	}
	if BoolVal(result.limits, "observed_count_is_lower_bound") {
		t.Fatalf("observed_count_is_lower_bound = true, want false")
	}
	// New completeness keys: always present, and complete here.
	if got, want := BoolVal(result.limits, "k8s_relationships_complete"), true; got != want {
		t.Fatalf("k8s_relationships_complete = %v, want %v", got, want)
	}
	if got, want := IntVal(result.limits, "k8s_select_candidate_sentinel_limit"), repositorySemanticEntityLimit+1; got != want {
		t.Fatalf("k8s_select_candidate_sentinel_limit = %d, want %d", got, want)
	}
	if _, present := result.limits["k8s_relationships_incomplete_reason"]; present {
		t.Fatalf("k8s_relationships_incomplete_reason present on a complete scan; limits = %#v", result.limits)
	}
}
