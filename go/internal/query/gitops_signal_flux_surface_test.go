// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
)

// TestContainsGitOpsSignalsDoesNotClaimFluxSurface is a no-accidental-surface
// regression for issue #5342: containsGitOpsSignals used to list
// "flux_kustomization"/"flux_helmrelease" as matched literals even though no
// parser or collector ever emits those exact platform/family values (the
// Flux Kustomization parse path captures typed sourceRef/path/targetNamespace
// evidence only, in its own "flux_kustomizations" bucket -- it never writes a
// platform-kind string). Those two literals were removed rather than kept as
// dead cases; this test proves they no longer trigger a GitOps-signal match,
// so nothing quietly resurrects an unbacked "Flux is queryable" claim.
func TestContainsGitOpsSignalsDoesNotClaimFluxSurface(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		{"flux_kustomization"},
		{"flux_helmrelease"},
		{"flux"},
	}
	for _, platforms := range cases {
		if repository.ContainsGitOpsSignals(platforms, nil) {
			t.Fatalf("repository.ContainsGitOpsSignals(%v, nil) = true, want false (no Flux emitter backs this label)", platforms)
		}
		if repository.ContainsGitOpsSignals(nil, platforms) {
			t.Fatalf("repository.ContainsGitOpsSignals(nil, %v) = true, want false (no Flux emitter backs this label)", platforms)
		}
	}

	// The still-live GitOps literals must keep matching.
	if !repository.ContainsGitOpsSignals([]string{"argocd"}, nil) {
		t.Fatal("repository.ContainsGitOpsSignals([argocd], nil) = false, want true")
	}
}

// TestDeploymentTraceGitOpsToolFamiliesDoesNotClaimFluxSurface is the
// deployment_trace_support_helpers.go counterpart of the test above: a
// platform kind of "flux", "flux_kustomization", or "flux_helmrelease" must
// not be classified into the "flux" tool family, since nothing emits those
// platform-kind values today (issue #5342).
func TestDeploymentTraceGitOpsToolFamiliesDoesNotClaimFluxSurface(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"flux", "flux_kustomization", "flux_helmrelease"} {
		families := impacttrace.DeploymentTraceGitOpsToolFamilies([]string{kind}, nil, nil, nil)
		for _, family := range families {
			if family == "flux" {
				t.Fatalf("impacttrace.DeploymentTraceGitOpsToolFamilies([%q], ...) = %v, want no \"flux\" family (no emitter backs it)", kind, families)
			}
		}
	}

	// argocd must keep classifying correctly.
	families := impacttrace.DeploymentTraceGitOpsToolFamilies([]string{"argocd"}, nil, nil, nil)
	found := false
	for _, family := range families {
		if family == "argocd" {
			found = true
		}
	}
	if !found {
		t.Fatalf("impacttrace.DeploymentTraceGitOpsToolFamilies([argocd], ...) = %v, want to include \"argocd\"", families)
	}
}
