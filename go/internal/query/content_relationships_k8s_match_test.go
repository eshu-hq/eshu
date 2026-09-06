// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestK8sSelectMatchSelectorAuthoritativeMatch proves a Service with a known,
// non-empty selector that is a subset of the workload's pod-template labels
// SELECTS the workload with the selector-match reason, even when the two
// entities have different names -- the false negative this fix closes.
func TestK8sSelectMatchSelectorAuthoritativeMatch(t *testing.T) {
	t.Parallel()

	service := k8sSelectMatchInput{
		Kind:            "Service",
		Name:            "web",
		Namespace:       "prod",
		Selector:        "app=frontend",
		SelectorPresent: true,
	}
	workload := k8sSelectMatchInput{
		Kind:                     "Deployment",
		Name:                     "frontend-deploy",
		Namespace:                "prod",
		PodTemplateLabels:        "app=frontend,tier=web",
		PodTemplateLabelsPresent: true,
	}

	matched, reason, _ := querycontract.K8sSelectMatch(service, workload)
	if !matched {
		t.Fatalf("querycontract.K8sSelectMatch() matched = false, want true")
	}
	if reason != querycontract.K8sSelectReasonSelectorMatch {
		t.Fatalf("querycontract.K8sSelectMatch() reason = %q, want %q", reason, querycontract.K8sSelectReasonSelectorMatch)
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
		Kind:            "Service",
		Name:            "api",
		Namespace:       "prod",
		Selector:        "app=api-v2",
		SelectorPresent: true,
	}
	workload := k8sSelectMatchInput{
		Kind:                     "Deployment",
		Name:                     "api",
		Namespace:                "prod",
		PodTemplateLabels:        "app=api-v1",
		PodTemplateLabelsPresent: true,
	}

	matched, reason, _ := querycontract.K8sSelectMatch(service, workload)
	if matched {
		t.Fatalf("querycontract.K8sSelectMatch() matched = true, want false (selector mismatch must never fall back); reason = %q", reason)
	}
}

// TestK8sSelectMatchSelectorlessServiceNeverMatches proves a Service with a
// known, EMPTY selector (ExternalName/manual Endpoints) never SELECTS
// anything -- the empty-selector-vacuous-subset guard. An empty selector map
// is trivially a subset of every label map, so this must be special-cased.
func TestK8sSelectMatchSelectorlessServiceNeverMatches(t *testing.T) {
	t.Parallel()

	service := k8sSelectMatchInput{
		Kind:            "Service",
		Name:            "external",
		Namespace:       "prod",
		Selector:        "",
		SelectorPresent: true,
	}
	workload := k8sSelectMatchInput{
		Kind:                     "Deployment",
		Name:                     "external",
		Namespace:                "prod",
		PodTemplateLabels:        "app=anything",
		PodTemplateLabelsPresent: true,
	}

	matched, _, _ := querycontract.K8sSelectMatch(service, workload)
	if matched {
		t.Fatalf("querycontract.K8sSelectMatch() matched = true, want false (empty selector must never vacuously match)")
	}
}

// TestK8sSelectMatchVintageFallback proves that when the selector key is
// ABSENT (pre-upgrade content row, selector truth unknown), the matcher
// falls back to name+namespace matching with the fallback reason.
func TestK8sSelectMatchVintageFallback(t *testing.T) {
	t.Parallel()

	service := k8sSelectMatchInput{
		Kind:            "Service",
		Name:            "demo",
		Namespace:       "prod",
		SelectorPresent: false,
	}
	workload := k8sSelectMatchInput{
		Kind:                     "Deployment",
		Name:                     "demo",
		Namespace:                "prod",
		PodTemplateLabelsPresent: false,
	}

	matched, reason, _ := querycontract.K8sSelectMatch(service, workload)
	if !matched {
		t.Fatalf("querycontract.K8sSelectMatch() matched = false, want true (vintage name+namespace fallback)")
	}
	if reason != k8sSelectReasonNameNamespace {
		t.Fatalf("querycontract.K8sSelectMatch() reason = %q, want %q", reason, k8sSelectReasonNameNamespace)
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
		Kind:            "Service",
		Name:            "demo",
		Namespace:       "prod",
		Selector:        "app=demo",
		SelectorPresent: true,
	}
	workload := k8sSelectMatchInput{
		Kind:                     "Deployment",
		Name:                     "demo",
		Namespace:                "prod",
		PodTemplateLabelsPresent: false,
	}

	matched, _, mixedVintageDrop := querycontract.K8sSelectMatch(service, workload)
	if matched {
		t.Fatalf("querycontract.K8sSelectMatch() matched = true, want false (mixed vintage must not match or fall back)")
	}
	if !mixedVintageDrop {
		t.Fatalf("querycontract.K8sSelectMatch() mixedVintageDrop = false, want true (this is the diagnostic signal callers log at Debug)")
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
				Kind: "Service", Name: "api", Namespace: "prod",
				Selector: "app=api-v2", SelectorPresent: true,
			},
			workload: k8sSelectMatchInput{
				Kind: "Deployment", Name: "api", Namespace: "prod",
				PodTemplateLabels: "app=api-v1", PodTemplateLabelsPresent: true,
			},
		},
		"empty_selector": {
			service: k8sSelectMatchInput{
				Kind: "Service", Name: "external", Namespace: "prod",
				Selector: "", SelectorPresent: true,
			},
			workload: k8sSelectMatchInput{
				Kind: "Deployment", Name: "external", Namespace: "prod",
				PodTemplateLabels: "app=anything", PodTemplateLabelsPresent: true,
			},
		},
		"namespace_mismatch": {
			service: k8sSelectMatchInput{
				Kind: "Service", Name: "web", Namespace: "prod",
				Selector: "app=frontend", SelectorPresent: true,
			},
			workload: k8sSelectMatchInput{
				Kind: "Deployment", Name: "frontend-deploy", Namespace: "staging",
				PodTemplateLabels: "app=frontend", PodTemplateLabelsPresent: true,
			},
		},
		"non_deployment_workload": {
			service: k8sSelectMatchInput{
				Kind: "Service", Name: "web", Namespace: "prod",
				Selector: "app=frontend", SelectorPresent: true,
			},
			workload: k8sSelectMatchInput{
				Kind: "StatefulSet", Name: "frontend-set", Namespace: "prod",
				PodTemplateLabels: "app=frontend", PodTemplateLabelsPresent: true,
			},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, mixedVintageDrop := querycontract.K8sSelectMatch(tc.service, tc.workload)
			if mixedVintageDrop {
				t.Fatalf("querycontract.K8sSelectMatch() mixedVintageDrop = true, want false for case %q", name)
			}
		})
	}
}

// TestK8sSelectMatchNamespaceScoped proves that a matching selector across
// different namespaces never produces an edge.
func TestK8sSelectMatchNamespaceScoped(t *testing.T) {
	t.Parallel()

	service := k8sSelectMatchInput{
		Kind:            "Service",
		Name:            "web",
		Namespace:       "prod",
		Selector:        "app=frontend",
		SelectorPresent: true,
	}
	workload := k8sSelectMatchInput{
		Kind:                     "Deployment",
		Name:                     "frontend-deploy",
		Namespace:                "staging",
		PodTemplateLabels:        "app=frontend",
		PodTemplateLabelsPresent: true,
	}

	matched, _, _ := querycontract.K8sSelectMatch(service, workload)
	if matched {
		t.Fatalf("querycontract.K8sSelectMatch() matched = true, want false (namespace mismatch)")
	}
}

// TestK8sSelectMatchNonDeploymentWorkloadNeverMatches proves the v1 matcher
// scope stays Deployment-only, even when selector/labels would otherwise
// subset-match -- no new capability claim beyond what's documented.
func TestK8sSelectMatchNonDeploymentWorkloadNeverMatches(t *testing.T) {
	t.Parallel()

	service := k8sSelectMatchInput{
		Kind:            "Service",
		Name:            "web",
		Namespace:       "prod",
		Selector:        "app=frontend",
		SelectorPresent: true,
	}
	workload := k8sSelectMatchInput{
		Kind:                     "StatefulSet",
		Name:                     "frontend-set",
		Namespace:                "prod",
		PodTemplateLabels:        "app=frontend",
		PodTemplateLabelsPresent: true,
	}

	matched, _, _ := querycontract.K8sSelectMatch(service, workload)
	if matched {
		t.Fatalf("querycontract.K8sSelectMatch() matched = true, want false (matcher scope is Deployment-only in v1)")
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
