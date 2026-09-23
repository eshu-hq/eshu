// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/kubernetes"
)

// TestK8sSelectMatchSelectorAuthoritativeMatch proves a Service with a known,
// non-empty selector that is a subset of the workload's pod-template labels
// SELECTS the workload with the selector-match reason, even when the two
// entities have different names -- the false negative this fix closes.
func TestK8sSelectMatchSelectorAuthoritativeMatch(t *testing.T) {
	t.Parallel()

	service := kubernetes.SelectMatchInput{
		Kind:            "Service",
		Name:            "web",
		Namespace:       "prod",
		Selector:        "app=frontend",
		SelectorPresent: true,
	}
	workload := kubernetes.SelectMatchInput{
		Kind:                     "Deployment",
		Name:                     "frontend-deploy",
		Namespace:                "prod",
		PodTemplateLabels:        "app=frontend,tier=web",
		PodTemplateLabelsPresent: true,
	}

	matched, reason, _ := kubernetes.SelectMatch(service, workload)
	if !matched {
		t.Fatalf("kubernetes.SelectMatch() matched = false, want true")
	}
	if reason != kubernetes.SelectReasonSelectorMatch {
		t.Fatalf("kubernetes.SelectMatch() reason = %q, want %q", reason, kubernetes.SelectReasonSelectorMatch)
	}
}

// TestK8sSelectMatchAnchorSelectorMismatchNeverFallsBack is the load-bearing
// anchor: a Service with a KNOWN selector that does not match must never
// produce an edge of any reason, even when name+namespace coincide. The name
// fallback must be structurally unreachable once the selector is known --
// this is what prevents the false-positive-masking trap.
func TestK8sSelectMatchAnchorSelectorMismatchNeverFallsBack(t *testing.T) {
	t.Parallel()

	service := kubernetes.SelectMatchInput{
		Kind:            "Service",
		Name:            "api",
		Namespace:       "prod",
		Selector:        "app=api-v2",
		SelectorPresent: true,
	}
	workload := kubernetes.SelectMatchInput{
		Kind:                     "Deployment",
		Name:                     "api",
		Namespace:                "prod",
		PodTemplateLabels:        "app=api-v1",
		PodTemplateLabelsPresent: true,
	}

	matched, reason, _ := kubernetes.SelectMatch(service, workload)
	if matched {
		t.Fatalf("kubernetes.SelectMatch() matched = true, want false (selector mismatch must never fall back); reason = %q", reason)
	}
}

// TestK8sSelectMatchSelectorlessServiceNeverMatches proves a Service with a
// known, EMPTY selector (ExternalName/manual Endpoints) never SELECTS
// anything -- the empty-selector-vacuous-subset guard. An empty selector map
// is trivially a subset of every label map, so this must be special-cased.
func TestK8sSelectMatchSelectorlessServiceNeverMatches(t *testing.T) {
	t.Parallel()

	service := kubernetes.SelectMatchInput{
		Kind:            "Service",
		Name:            "external",
		Namespace:       "prod",
		Selector:        "",
		SelectorPresent: true,
	}
	workload := kubernetes.SelectMatchInput{
		Kind:                     "Deployment",
		Name:                     "external",
		Namespace:                "prod",
		PodTemplateLabels:        "app=anything",
		PodTemplateLabelsPresent: true,
	}

	matched, _, _ := kubernetes.SelectMatch(service, workload)
	if matched {
		t.Fatalf("kubernetes.SelectMatch() matched = true, want false (empty selector must never vacuously match)")
	}
}

// TestK8sSelectMatchVintageFallback proves that when the selector key is
// ABSENT (pre-upgrade content row, selector truth unknown), the matcher
// falls back to name+namespace matching with the fallback reason.
func TestK8sSelectMatchVintageFallback(t *testing.T) {
	t.Parallel()

	service := kubernetes.SelectMatchInput{
		Kind:            "Service",
		Name:            "demo",
		Namespace:       "prod",
		SelectorPresent: false,
	}
	workload := kubernetes.SelectMatchInput{
		Kind:                     "Deployment",
		Name:                     "demo",
		Namespace:                "prod",
		PodTemplateLabelsPresent: false,
	}

	matched, reason, _ := kubernetes.SelectMatch(service, workload)
	if !matched {
		t.Fatalf("kubernetes.SelectMatch() matched = false, want true (vintage name+namespace fallback)")
	}
	if reason != kubernetes.SelectReasonNameNamespace {
		t.Fatalf("kubernetes.SelectMatch() reason = %q, want %q", reason, kubernetes.SelectReasonNameNamespace)
	}
}

// TestK8sSelectMatchMixedVintageNoFallback proves that when the Service has
// a KNOWN, non-empty selector but the workload row predates
// pod_template_labels capture (key absent), the matcher does not match and
// does not fall back -- a transient false negative until re-ingest, which is
// the accuracy-first choice over guessing.
func TestK8sSelectMatchMixedVintageNoFallback(t *testing.T) {
	t.Parallel()

	service := kubernetes.SelectMatchInput{
		Kind:            "Service",
		Name:            "demo",
		Namespace:       "prod",
		Selector:        "app=demo",
		SelectorPresent: true,
	}
	workload := kubernetes.SelectMatchInput{
		Kind:                     "Deployment",
		Name:                     "demo",
		Namespace:                "prod",
		PodTemplateLabelsPresent: false,
	}

	matched, _, mixedVintageDrop := kubernetes.SelectMatch(service, workload)
	if matched {
		t.Fatalf("kubernetes.SelectMatch() matched = true, want false (mixed vintage must not match or fall back)")
	}
	if !mixedVintageDrop {
		t.Fatalf("kubernetes.SelectMatch() mixedVintageDrop = false, want true (this is the diagnostic signal callers log at Debug)")
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
		service  kubernetes.SelectMatchInput
		workload kubernetes.SelectMatchInput
	}{
		"selector_mismatch": {
			service: kubernetes.SelectMatchInput{
				Kind: "Service", Name: "api", Namespace: "prod",
				Selector: "app=api-v2", SelectorPresent: true,
			},
			workload: kubernetes.SelectMatchInput{
				Kind: "Deployment", Name: "api", Namespace: "prod",
				PodTemplateLabels: "app=api-v1", PodTemplateLabelsPresent: true,
			},
		},
		"empty_selector": {
			service: kubernetes.SelectMatchInput{
				Kind: "Service", Name: "external", Namespace: "prod",
				Selector: "", SelectorPresent: true,
			},
			workload: kubernetes.SelectMatchInput{
				Kind: "Deployment", Name: "external", Namespace: "prod",
				PodTemplateLabels: "app=anything", PodTemplateLabelsPresent: true,
			},
		},
		"namespace_mismatch": {
			service: kubernetes.SelectMatchInput{
				Kind: "Service", Name: "web", Namespace: "prod",
				Selector: "app=frontend", SelectorPresent: true,
			},
			workload: kubernetes.SelectMatchInput{
				Kind: "Deployment", Name: "frontend-deploy", Namespace: "staging",
				PodTemplateLabels: "app=frontend", PodTemplateLabelsPresent: true,
			},
		},
		"non_deployment_workload": {
			service: kubernetes.SelectMatchInput{
				Kind: "Service", Name: "web", Namespace: "prod",
				Selector: "app=frontend", SelectorPresent: true,
			},
			workload: kubernetes.SelectMatchInput{
				Kind: "StatefulSet", Name: "frontend-set", Namespace: "prod",
				PodTemplateLabels: "app=frontend", PodTemplateLabelsPresent: true,
			},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, mixedVintageDrop := kubernetes.SelectMatch(tc.service, tc.workload)
			if mixedVintageDrop {
				t.Fatalf("kubernetes.SelectMatch() mixedVintageDrop = true, want false for case %q", name)
			}
		})
	}
}

// TestK8sSelectMatchNamespaceScoped proves that a matching selector across
// different namespaces never produces an edge.
func TestK8sSelectMatchNamespaceScoped(t *testing.T) {
	t.Parallel()

	service := kubernetes.SelectMatchInput{
		Kind:            "Service",
		Name:            "web",
		Namespace:       "prod",
		Selector:        "app=frontend",
		SelectorPresent: true,
	}
	workload := kubernetes.SelectMatchInput{
		Kind:                     "Deployment",
		Name:                     "frontend-deploy",
		Namespace:                "staging",
		PodTemplateLabels:        "app=frontend",
		PodTemplateLabelsPresent: true,
	}

	matched, _, _ := kubernetes.SelectMatch(service, workload)
	if matched {
		t.Fatalf("kubernetes.SelectMatch() matched = true, want false (namespace mismatch)")
	}
}

// TestK8sSelectMatchNonDeploymentWorkloadNeverMatches proves the v1 matcher
// scope stays Deployment-only, even when selector/labels would otherwise
// subset-match -- no new capability claim beyond what's documented.
func TestK8sSelectMatchNonDeploymentWorkloadNeverMatches(t *testing.T) {
	t.Parallel()

	service := kubernetes.SelectMatchInput{
		Kind:            "Service",
		Name:            "web",
		Namespace:       "prod",
		Selector:        "app=frontend",
		SelectorPresent: true,
	}
	workload := kubernetes.SelectMatchInput{
		Kind:                     "StatefulSet",
		Name:                     "frontend-set",
		Namespace:                "prod",
		PodTemplateLabels:        "app=frontend",
		PodTemplateLabelsPresent: true,
	}

	matched, _, _ := kubernetes.SelectMatch(service, workload)
	if matched {
		t.Fatalf("kubernetes.SelectMatch() matched = true, want false (matcher scope is Deployment-only in v1)")
	}
}

// TestK8sSelectorSubsetOfEmptySelectorNeverSubset is a direct unit test of
// the subset-check guard: an empty selector string must never be treated as
// a subset of any label map, however large.
func TestK8sSelectorSubsetOfEmptySelectorNeverSubset(t *testing.T) {
	t.Parallel()

	if kubernetes.SelectorSubsetOf("", "app=anything,tier=web") {
		t.Fatalf("kubernetes.SelectorSubsetOf(empty, ...) = true, want false")
	}
	if kubernetes.SelectorSubsetOf("", "") {
		t.Fatalf("kubernetes.SelectorSubsetOf(empty, empty) = true, want false")
	}
}

// TestK8sSelectorSubsetOfPartialLabelValueMismatch proves the subset check
// requires exact key=value equality, not just key presence.
func TestK8sSelectorSubsetOfPartialLabelValueMismatch(t *testing.T) {
	t.Parallel()

	if kubernetes.SelectorSubsetOf("app=frontend", "app=backend,tier=web") {
		t.Fatalf("kubernetes.SelectorSubsetOf() = true, want false (value mismatch on shared key)")
	}
}
