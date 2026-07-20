// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

// TestContentReaderListRepoK8sSelectCandidatesScansTriState proves the narrow
// projection scan maps its eight positional columns into K8sSelectCandidate
// correctly: the namespace text is trimmed to mirror k8sNamespace, and the
// jsonb_typeof presence booleans drive SelectorPresent / PodTemplateLabelsPresent
// independently of the (possibly empty) value column, preserving the tri-state
// the matcher depends on. Converting through matchInput yields the same
// k8sSelectMatchInput the EntityContent path would produce.
func TestContentReaderListRepoK8sSelectCandidatesScansTriState(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "entity_name", "kind", "namespace",
				"selector_present", "selector", "pod_template_labels_present", "pod_template_labels",
			},
			rows: [][]driver.Value{
				// Service with a known, non-empty selector; namespace carries
				// surrounding whitespace that must be trimmed.
				{"svc-1", "web-svc", "Service", "  prod  ", true, "app=web", false, ""},
				// Vintage Service: selector key absent (present=false) even though
				// the value column coalesced to "".
				{"svc-2", "legacy", "Service", "prod", false, "", false, ""},
				// Deployment with pod-template labels present.
				{"dep-1", "web", "Deployment", "prod", false, "", true, "app=web,tier=api"},
			},
		},
	})

	reader := NewContentReader(db)
	candidates, err := reader.ListRepoK8sSelectCandidates(context.Background(), "repo-1", 5001)
	if err != nil {
		t.Fatalf("ListRepoK8sSelectCandidates() error = %v, want nil", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("len(candidates) = %d, want 3: %#v", len(candidates), candidates)
	}

	svc1 := candidates[0]
	if svc1.Namespace != "prod" {
		t.Fatalf("svc-1 namespace = %q, want trimmed %q", svc1.Namespace, "prod")
	}
	if !svc1.SelectorPresent || svc1.Selector != "app=web" {
		t.Fatalf("svc-1 selector tri-state = (%v, %q), want (true, app=web)", svc1.SelectorPresent, svc1.Selector)
	}
	if svc1.PodTemplateLabelsPresent {
		t.Fatalf("svc-1 pod_template_labels present = true, want false")
	}

	svc2 := candidates[1]
	if svc2.SelectorPresent {
		t.Fatalf("svc-2 SelectorPresent = true, want false (key absent, value coalesced to \"\")")
	}

	dep1 := candidates[2]
	if !dep1.PodTemplateLabelsPresent || dep1.PodTemplateLabels != "app=web,tier=api" {
		t.Fatalf("dep-1 pod_template_labels tri-state = (%v, %q), want (true, app=web,tier=api)", dep1.PodTemplateLabelsPresent, dep1.PodTemplateLabels)
	}

	// matchInput mirrors k8sSelectMatchInputFromEntity for the equivalent row:
	// svc-1 SELECTS dep-1 by selector subset, strictly namespace-scoped.
	target := newK8sWorkloadMatchTarget(dep1.matchInput())
	matched, reason, _ := target.Match(svc1.matchInput())
	if !matched || reason != k8sSelectReasonSelectorMatch {
		t.Fatalf("svc-1 -> dep-1 match = (%v, %q), want (true, %q)", matched, reason, k8sSelectReasonSelectorMatch)
	}
}
