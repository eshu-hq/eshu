// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"
)

// fetchK8sResourcesContentStore is a minimal ContentStore double whose
// SearchEntitiesByName returns a fixed row set, letting tests drive the real
// fetchK8sResources row-construction logic (as opposed to hand-building the
// map[string]any rows that logic produces).
type fetchK8sResourcesContentStore struct {
	fakePortContentStore
	byName []EntityContent
}

func (f fetchK8sResourcesContentStore) SearchEntitiesByName(
	context.Context, string, string, string, int,
) ([]EntityContent, error) {
	return f.byName, nil
}

// TestFetchK8sResourcesPreservesSelectorPodTemplateLabelsNamespaceTriState
// drives the real ImpactHandler.fetchK8sResources (not buildK8sRelationships
// with hand-made maps) and proves it carries the selector/pod_template_labels
// keys through to the returned resource row IFF the source content entity's
// metadata carries them, and never defaults an absent key to "". This is the
// comma-ok tri-state k8sSelectMatch depends on (see
// content_relationships_k8s_match.go): a defaulted "" would be
// indistinguishable from a genuinely empty/selectorless value.
func TestFetchK8sResourcesPreservesSelectorPodTemplateLabelsNamespaceTriState(t *testing.T) {
	t.Parallel()

	withLabels := EntityContent{
		EntityID:     "deployment-with-labels",
		EntityName:   "demo",
		RepoID:       "repo-1",
		RelativePath: "deploy/demo.yaml",
		Metadata: map[string]any{
			"kind":                "Deployment",
			"namespace":           "prod",
			"qualified_name":      "prod/Deployment/demo",
			"selector":            "app=frontend",
			"pod_template_labels": "app=frontend,tier=web",
		},
	}
	vintage := EntityContent{
		EntityID:     "deployment-vintage",
		EntityName:   "demo",
		RepoID:       "repo-1",
		RelativePath: "deploy/vintage.yaml",
		Metadata: map[string]any{
			"kind":           "Deployment",
			"namespace":      "prod",
			"qualified_name": "prod/Deployment/demo",
		},
	}

	h := &ImpactHandler{
		Content: fetchK8sResourcesContentStore{byName: []EntityContent{withLabels, vintage}},
	}

	resources, _, err := h.fetchK8sResources(context.Background(), "repo-1", "demo")
	if err != nil {
		t.Fatalf("fetchK8sResources() error = %v, want nil", err)
	}
	if len(resources) != 2 {
		t.Fatalf("len(resources) = %d, want 2: %#v", len(resources), resources)
	}

	byID := make(map[string]map[string]any, len(resources))
	for _, resource := range resources {
		byID[StringVal(resource, "entity_id")] = resource
	}

	withLabelsResource, ok := byID["deployment-with-labels"]
	if !ok {
		t.Fatalf("missing deployment-with-labels resource: %#v", resources)
	}
	if _, present := withLabelsResource["selector"]; !present {
		t.Fatalf("deployment-with-labels resource missing selector key, want present: %#v", withLabelsResource)
	}
	if got, want := withLabelsResource["selector"], "app=frontend"; got != want {
		t.Fatalf("selector = %#v, want %#v", got, want)
	}
	if _, present := withLabelsResource["pod_template_labels"]; !present {
		t.Fatalf("deployment-with-labels resource missing pod_template_labels key, want present: %#v", withLabelsResource)
	}
	if got, want := withLabelsResource["pod_template_labels"], "app=frontend,tier=web"; got != want {
		t.Fatalf("pod_template_labels = %#v, want %#v", got, want)
	}
	if got, want := withLabelsResource["namespace"], "prod"; got != want {
		t.Fatalf("namespace = %#v, want %#v", got, want)
	}

	vintageResource, ok := byID["deployment-vintage"]
	if !ok {
		t.Fatalf("missing deployment-vintage resource: %#v", resources)
	}
	if _, present := vintageResource["selector"]; present {
		t.Fatalf("deployment-vintage resource has selector key = %#v, want key absent (tri-state: vintage rows carry no selector truth)", vintageResource["selector"])
	}
	if _, present := vintageResource["pod_template_labels"]; present {
		t.Fatalf("deployment-vintage resource has pod_template_labels key = %#v, want key absent", vintageResource["pod_template_labels"])
	}
	if got, want := vintageResource["namespace"], "prod"; got != want {
		t.Fatalf("namespace = %#v, want %#v", got, want)
	}
}

// TestCollectDeploymentSourceK8sResourcesPreservesSelectorPodTemplateLabelsNamespaceTriState
// drives the real collectDeploymentSourceK8sResources (not buildK8sRelationships
// with hand-made maps) and proves the same comma-ok tri-state preservation on
// the GitOps-controller-scoped resource path: a K8sResource entity under the
// controller's source root carries selector/pod_template_labels through IFF
// its metadata has them, never defaulted to "".
func TestCollectDeploymentSourceK8sResourcesPreservesSelectorPodTemplateLabelsNamespaceTriState(t *testing.T) {
	t.Parallel()

	controllers := []map[string]any{
		{
			"entity_id":       "argocd-app-1",
			"entity_name":     "demo-app",
			"controller_kind": "ArgoCDApplication",
			"repo_id":         "repo-1",
			"relative_path":   "argocd/demo-app.yaml",
			"source_roots":    []string{"deploy"},
		},
	}

	entities := []EntityContent{
		{
			EntityID:     "deployment-with-labels",
			EntityType:   "K8sResource",
			EntityName:   "demo",
			RepoID:       "repo-1",
			RelativePath: "deploy/demo.yaml",
			Metadata: map[string]any{
				"kind":                "Deployment",
				"namespace":           "prod",
				"qualified_name":      "prod/Deployment/demo",
				"selector":            "app=frontend",
				"pod_template_labels": "app=frontend,tier=web",
			},
		},
		{
			EntityID:     "deployment-vintage",
			EntityType:   "K8sResource",
			EntityName:   "demo-vintage",
			RepoID:       "repo-1",
			RelativePath: "deploy/vintage.yaml",
			Metadata: map[string]any{
				"kind":           "Deployment",
				"namespace":      "prod",
				"qualified_name": "prod/Deployment/demo-vintage",
			},
		},
	}

	resources, _ := collectDeploymentSourceK8sResources(controllers, entities)
	if len(resources) != 2 {
		t.Fatalf("len(resources) = %d, want 2: %#v", len(resources), resources)
	}

	byID := make(map[string]map[string]any, len(resources))
	for _, resource := range resources {
		byID[StringVal(resource, "entity_id")] = resource
	}

	withLabelsResource, ok := byID["deployment-with-labels"]
	if !ok {
		t.Fatalf("missing deployment-with-labels resource: %#v", resources)
	}
	if _, present := withLabelsResource["selector"]; !present {
		t.Fatalf("deployment-with-labels resource missing selector key, want present: %#v", withLabelsResource)
	}
	if got, want := withLabelsResource["selector"], "app=frontend"; got != want {
		t.Fatalf("selector = %#v, want %#v", got, want)
	}
	if _, present := withLabelsResource["pod_template_labels"]; !present {
		t.Fatalf("deployment-with-labels resource missing pod_template_labels key, want present: %#v", withLabelsResource)
	}
	if got, want := withLabelsResource["namespace"], "prod"; got != want {
		t.Fatalf("namespace = %#v, want %#v", got, want)
	}

	vintageResource, ok := byID["deployment-vintage"]
	if !ok {
		t.Fatalf("missing deployment-vintage resource: %#v", resources)
	}
	if _, present := vintageResource["selector"]; present {
		t.Fatalf("deployment-vintage resource has selector key = %#v, want key absent (tri-state: vintage rows carry no selector truth)", vintageResource["selector"])
	}
	if _, present := vintageResource["pod_template_labels"]; present {
		t.Fatalf("deployment-vintage resource has pod_template_labels key = %#v, want key absent", vintageResource["pod_template_labels"])
	}
	if got, want := vintageResource["namespace"], "prod"; got != want {
		t.Fatalf("namespace = %#v, want %#v", got, want)
	}
}
