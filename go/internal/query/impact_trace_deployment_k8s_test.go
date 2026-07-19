// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestBuildK8sRelationshipsAnchorSelectorMismatchNeverFallsBackToName is the
// impact-trace counterpart of the content_relationships anchor test: a
// Service and Deployment sharing the SAME name (and namespace) but with a
// KNOWN, non-matching selector must produce NO SELECTS edge of any reason.
// Before this fix, buildK8sRelationships matched purely on entity_name and
// would have produced a false-positive k8s_service_name_namespace edge here.
func TestBuildK8sRelationshipsAnchorSelectorMismatchNeverFallsBackToName(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{
		{
			"entity_id":   "service-1",
			"entity_name": "api",
			"kind":        "Service",
			"namespace":   "prod",
			"selector":    "app=api-v2",
		},
		{
			"entity_id":           "deployment-1",
			"entity_name":         "api",
			"kind":                "Deployment",
			"namespace":           "prod",
			"pod_template_labels": "app=api-v1",
		},
	}

	relationships := buildK8sRelationships(resources)
	for _, relationship := range relationships {
		if relationship["type"] == "SELECTS" {
			t.Fatalf("buildK8sRelationships() produced a SELECTS edge, want none: %#v", relationship)
		}
	}
}

// TestBuildK8sRelationshipsSelectorMatchNamespaceScoped proves the
// impact-trace path tightens namespace scoping: a matching selector across
// different namespaces must not produce a SELECTS edge. The pre-fix
// buildK8sRelationships had no namespace check at all.
func TestBuildK8sRelationshipsSelectorMatchNamespaceScoped(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{
		{
			"entity_id":   "service-1",
			"entity_name": "web",
			"kind":        "Service",
			"namespace":   "prod",
			"selector":    "app=frontend",
		},
		{
			"entity_id":           "deployment-1",
			"entity_name":         "web",
			"kind":                "Deployment",
			"namespace":           "staging",
			"pod_template_labels": "app=frontend",
		},
	}

	relationships := buildK8sRelationships(resources)
	for _, relationship := range relationships {
		if relationship["type"] == "SELECTS" {
			t.Fatalf("buildK8sRelationships() produced a SELECTS edge across namespaces, want none: %#v", relationship)
		}
	}
}

// TestBuildK8sRelationshipsSelectorAuthoritativeMatch proves the positive
// case still works end to end: a known, matching selector produces a SELECTS
// edge with reason k8s_service_selector_match.
func TestBuildK8sRelationshipsSelectorAuthoritativeMatch(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{
		{
			"entity_id":   "service-1",
			"entity_name": "comprehensive-api",
			"kind":        "Service",
			"namespace":   "production",
			"selector":    "app=comprehensive-api",
		},
		{
			"entity_id":           "deployment-1",
			"entity_name":         "comprehensive-api",
			"kind":                "Deployment",
			"namespace":           "production",
			"pod_template_labels": "app=comprehensive-api,version=v2",
		},
	}

	relationships := buildK8sRelationships(resources)
	var selects []map[string]any
	for _, relationship := range relationships {
		if relationship["type"] == "SELECTS" {
			selects = append(selects, relationship)
		}
	}
	if len(selects) != 1 {
		t.Fatalf("len(selects) = %d, want 1: %#v", len(selects), relationships)
	}
	if got, want := selects[0]["reason"], "k8s_service_selector_match"; got != want {
		t.Fatalf("reason = %#v, want %#v", got, want)
	}
}

// TestBuildK8sRelationshipsVintageFallbackPreserved proves the impact-trace
// path still falls back to name+namespace matching when the selector key is
// absent (pre-upgrade content rows).
func TestBuildK8sRelationshipsVintageFallbackPreserved(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{
		{
			"entity_id":   "service-1",
			"entity_name": "demo",
			"kind":        "Service",
			"namespace":   "prod",
		},
		{
			"entity_id":   "deployment-1",
			"entity_name": "demo",
			"kind":        "Deployment",
			"namespace":   "prod",
		},
	}

	relationships := buildK8sRelationships(resources)
	var selects []map[string]any
	for _, relationship := range relationships {
		if relationship["type"] == "SELECTS" {
			selects = append(selects, relationship)
		}
	}
	if len(selects) != 1 {
		t.Fatalf("len(selects) = %d, want 1: %#v", len(selects), relationships)
	}
	if got, want := selects[0]["reason"], "k8s_service_name_namespace"; got != want {
		t.Fatalf("reason = %#v, want %#v", got, want)
	}
}
