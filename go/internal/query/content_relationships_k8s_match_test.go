// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestK8sSelectMatchSelectorAuthoritativeMatch proves a Service with a known,
// non-empty selector that is a subset of the workload's pod-template labels
// SELECTS the workload with the selector-match reason, even when the two
// entities have different names -- the false negative this fix closes.
func TestK8sSelectMatchSelectorAuthoritativeMatch(t *testing.T) {
	t.Parallel()

	service := k8sSelectMatchInput{
		kind:            "Service",
		name:            "web",
		namespace:       "prod",
		selector:        "app=frontend",
		selectorPresent: true,
	}
	workload := k8sSelectMatchInput{
		kind:                     "Deployment",
		name:                     "frontend-deploy",
		namespace:                "prod",
		podTemplateLabels:        "app=frontend,tier=web",
		podTemplateLabelsPresent: true,
	}

	matched, reason, _ := k8sSelectMatch(service, workload)
	if !matched {
		t.Fatalf("k8sSelectMatch() matched = false, want true")
	}
	if reason != k8sSelectReasonSelectorMatch {
		t.Fatalf("k8sSelectMatch() reason = %q, want %q", reason, k8sSelectReasonSelectorMatch)
	}
}

// TestK8sSelectMatchAnchorSelectorMismatchNeverFallsBack is the load-bearing
// anchor: a Service with a KNOWN selector that does not match must never
// produce an edge of any reason, even when name+namespace coincide. The name
// fallback must be structurally unreachable once the selector is known --
// this is what prevents the false-positive-masking trap.
func TestK8sSelectMatchAnchorSelectorMismatchNeverFallsBack(t *testing.T) {
	t.Parallel()

	service := k8sSelectMatchInput{
		kind:            "Service",
		name:            "api",
		namespace:       "prod",
		selector:        "app=api-v2",
		selectorPresent: true,
	}
	workload := k8sSelectMatchInput{
		kind:                     "Deployment",
		name:                     "api",
		namespace:                "prod",
		podTemplateLabels:        "app=api-v1",
		podTemplateLabelsPresent: true,
	}

	matched, reason, _ := k8sSelectMatch(service, workload)
	if matched {
		t.Fatalf("k8sSelectMatch() matched = true, want false (selector mismatch must never fall back); reason = %q", reason)
	}
}

// TestK8sSelectMatchSelectorlessServiceNeverMatches proves a Service with a
// known, EMPTY selector (ExternalName/manual Endpoints) never SELECTS
// anything -- the empty-selector-vacuous-subset guard. An empty selector map
// is trivially a subset of every label map, so this must be special-cased.
func TestK8sSelectMatchSelectorlessServiceNeverMatches(t *testing.T) {
	t.Parallel()

	service := k8sSelectMatchInput{
		kind:            "Service",
		name:            "external",
		namespace:       "prod",
		selector:        "",
		selectorPresent: true,
	}
	workload := k8sSelectMatchInput{
		kind:                     "Deployment",
		name:                     "external",
		namespace:                "prod",
		podTemplateLabels:        "app=anything",
		podTemplateLabelsPresent: true,
	}

	matched, _, _ := k8sSelectMatch(service, workload)
	if matched {
		t.Fatalf("k8sSelectMatch() matched = true, want false (empty selector must never vacuously match)")
	}
}

// TestK8sSelectMatchVintageFallback proves that when the selector key is
// ABSENT (pre-upgrade content row, selector truth unknown), the matcher
// falls back to name+namespace matching with the fallback reason.
func TestK8sSelectMatchVintageFallback(t *testing.T) {
	t.Parallel()

	service := k8sSelectMatchInput{
		kind:            "Service",
		name:            "demo",
		namespace:       "prod",
		selectorPresent: false,
	}
	workload := k8sSelectMatchInput{
		kind:                     "Deployment",
		name:                     "demo",
		namespace:                "prod",
		podTemplateLabelsPresent: false,
	}

	matched, reason, _ := k8sSelectMatch(service, workload)
	if !matched {
		t.Fatalf("k8sSelectMatch() matched = false, want true (vintage name+namespace fallback)")
	}
	if reason != k8sSelectReasonNameNamespace {
		t.Fatalf("k8sSelectMatch() reason = %q, want %q", reason, k8sSelectReasonNameNamespace)
	}
}

// TestK8sSelectMatchMixedVintageNoFallback proves that when the Service has
// a KNOWN, non-empty selector but the workload row predates
// pod_template_labels capture (key absent), the matcher does not match and
// does not fall back -- a transient false negative until re-ingest, which is
// the accuracy-first choice over guessing.
func TestK8sSelectMatchMixedVintageNoFallback(t *testing.T) {
	t.Parallel()

	service := k8sSelectMatchInput{
		kind:            "Service",
		name:            "demo",
		namespace:       "prod",
		selector:        "app=demo",
		selectorPresent: true,
	}
	workload := k8sSelectMatchInput{
		kind:                     "Deployment",
		name:                     "demo",
		namespace:                "prod",
		podTemplateLabelsPresent: false,
	}

	matched, _, mixedVintageDrop := k8sSelectMatch(service, workload)
	if matched {
		t.Fatalf("k8sSelectMatch() matched = true, want false (mixed vintage must not match or fall back)")
	}
	if !mixedVintageDrop {
		t.Fatalf("k8sSelectMatch() mixedVintageDrop = false, want true (this is the diagnostic signal callers log at Debug)")
	}
}

// TestK8sSelectMatchMixedVintageDropFlagOnlySetOnThatPath proves
// mixedVintageDrop is a precise signal: it must NOT be set on the other
// no-match paths (selector mismatch, empty selector, namespace mismatch,
// non-Deployment workload) -- only on the exact "selector known, workload
// pod_template_labels absent" case. A caller logging on this flag must not
// fire for those unrelated no-match reasons.
func TestK8sSelectMatchMixedVintageDropFlagOnlySetOnThatPath(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		service  k8sSelectMatchInput
		workload k8sSelectMatchInput
	}{
		"selector_mismatch": {
			service: k8sSelectMatchInput{
				kind: "Service", name: "api", namespace: "prod",
				selector: "app=api-v2", selectorPresent: true,
			},
			workload: k8sSelectMatchInput{
				kind: "Deployment", name: "api", namespace: "prod",
				podTemplateLabels: "app=api-v1", podTemplateLabelsPresent: true,
			},
		},
		"empty_selector": {
			service: k8sSelectMatchInput{
				kind: "Service", name: "external", namespace: "prod",
				selector: "", selectorPresent: true,
			},
			workload: k8sSelectMatchInput{
				kind: "Deployment", name: "external", namespace: "prod",
				podTemplateLabels: "app=anything", podTemplateLabelsPresent: true,
			},
		},
		"namespace_mismatch": {
			service: k8sSelectMatchInput{
				kind: "Service", name: "web", namespace: "prod",
				selector: "app=frontend", selectorPresent: true,
			},
			workload: k8sSelectMatchInput{
				kind: "Deployment", name: "frontend-deploy", namespace: "staging",
				podTemplateLabels: "app=frontend", podTemplateLabelsPresent: true,
			},
		},
		"non_deployment_workload": {
			service: k8sSelectMatchInput{
				kind: "Service", name: "web", namespace: "prod",
				selector: "app=frontend", selectorPresent: true,
			},
			workload: k8sSelectMatchInput{
				kind: "StatefulSet", name: "frontend-set", namespace: "prod",
				podTemplateLabels: "app=frontend", podTemplateLabelsPresent: true,
			},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, mixedVintageDrop := k8sSelectMatch(tc.service, tc.workload)
			if mixedVintageDrop {
				t.Fatalf("k8sSelectMatch() mixedVintageDrop = true, want false for case %q", name)
			}
		})
	}
}

// TestK8sSelectMatchNamespaceScoped proves that a matching selector across
// different namespaces never produces an edge.
func TestK8sSelectMatchNamespaceScoped(t *testing.T) {
	t.Parallel()

	service := k8sSelectMatchInput{
		kind:            "Service",
		name:            "web",
		namespace:       "prod",
		selector:        "app=frontend",
		selectorPresent: true,
	}
	workload := k8sSelectMatchInput{
		kind:                     "Deployment",
		name:                     "frontend-deploy",
		namespace:                "staging",
		podTemplateLabels:        "app=frontend",
		podTemplateLabelsPresent: true,
	}

	matched, _, _ := k8sSelectMatch(service, workload)
	if matched {
		t.Fatalf("k8sSelectMatch() matched = true, want false (namespace mismatch)")
	}
}

// TestK8sSelectMatchNonDeploymentWorkloadNeverMatches proves the v1 matcher
// scope stays Deployment-only, even when selector/labels would otherwise
// subset-match -- no new capability claim beyond what's documented.
func TestK8sSelectMatchNonDeploymentWorkloadNeverMatches(t *testing.T) {
	t.Parallel()

	service := k8sSelectMatchInput{
		kind:            "Service",
		name:            "web",
		namespace:       "prod",
		selector:        "app=frontend",
		selectorPresent: true,
	}
	workload := k8sSelectMatchInput{
		kind:                     "StatefulSet",
		name:                     "frontend-set",
		namespace:                "prod",
		podTemplateLabels:        "app=frontend",
		podTemplateLabelsPresent: true,
	}

	matched, _, _ := k8sSelectMatch(service, workload)
	if matched {
		t.Fatalf("k8sSelectMatch() matched = true, want false (matcher scope is Deployment-only in v1)")
	}
}

// TestK8sSelectorSubsetOfEmptySelectorNeverSubset is a direct unit test of
// the subset-check guard: an empty selector string must never be treated as
// a subset of any label map, however large.
func TestK8sSelectorSubsetOfEmptySelectorNeverSubset(t *testing.T) {
	t.Parallel()

	if k8sSelectorSubsetOf("", "app=anything,tier=web") {
		t.Fatalf("k8sSelectorSubsetOf(empty, ...) = true, want false")
	}
	if k8sSelectorSubsetOf("", "") {
		t.Fatalf("k8sSelectorSubsetOf(empty, empty) = true, want false")
	}
}

// TestK8sSelectorSubsetOfPartialLabelValueMismatch proves the subset check
// requires exact key=value equality, not just key presence.
func TestK8sSelectorSubsetOfPartialLabelValueMismatch(t *testing.T) {
	t.Parallel()

	if k8sSelectorSubsetOf("app=frontend", "app=backend,tier=web") {
		t.Fatalf("k8sSelectorSubsetOf() = true, want false (value mismatch on shared key)")
	}
}
