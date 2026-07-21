// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"
)

// TestResourceInvestigationDefaultLabelPredicateUsesFluxHelmReleaseNotDeadHelmReleaseLabel
// is the issue #5483 C1 dead-label fix regression: the default (unscoped)
// resourceType label disjunction previously included "n:HelmRelease" -- a
// label the canonical graph writer never produces (a manifest's kind:
// HelmRelease previously projected as a generic K8sResource node with
// kind='HelmRelease', never a distinct :HelmRelease label). That branch was
// dead code masking as coverage. It is replaced with "n:FluxHelmRelease", the
// real typed label issue #5483 C1 introduces, so the default investigation
// predicate can actually reach a projected HelmRelease node.
func TestResourceInvestigationDefaultLabelPredicateUsesFluxHelmReleaseNotDeadHelmReleaseLabel(t *testing.T) {
	t.Parallel()

	got := resourceInvestigationLabelPredicate("")
	if !strings.Contains(got, "n:FluxHelmRelease") {
		t.Fatalf("default label predicate = %q, want it to include n:FluxHelmRelease", got)
	}
	if strings.Contains(got, "n:HelmRelease") && !strings.Contains(got, "n:FluxHelmRelease") {
		t.Fatalf("default label predicate = %q, still references the dead n:HelmRelease label (never produced by the canonical graph writer)", got)
	}
	// Precise guard: "n:HelmRelease" must not appear as its own disjunct
	// (distinguishing it from "n:FluxHelmRelease", which contains the
	// substring "HelmRelease" too).
	for _, disjunct := range strings.Split(got, " OR ") {
		if strings.TrimSpace(disjunct) == "n:HelmRelease)" || strings.TrimSpace(disjunct) == "n:HelmRelease" {
			t.Fatalf("default label predicate still contains the bare dead disjunct n:HelmRelease: %q", got)
		}
	}
}

// TestInvestigateResourceDefaultPredicateReachesProjectedFluxHelmReleaseNode
// is the end-to-end sibling of the test above: it drives
// resolveResourceInvestigationTarget (the resolver stage the fixed default
// predicate feeds) with the default (unscoped) resource_type against a fake
// graph reader returning a FluxHelmRelease-labeled row, proving the fixed
// predicate is not only textually correct but actually resolves a projected
// FluxHelmRelease fixture node -- the dead "n:HelmRelease" branch it replaced
// could never have resolved any real node, since the canonical graph writer
// never wrote that label.
func TestInvestigateResourceDefaultPredicateReachesProjectedFluxHelmReleaseNode(t *testing.T) {
	t.Parallel()

	graph := &recordingResourceInvestigationGraph{
		runRows: [][]map[string]any{{
			{
				"id": "flux:helmrelease:podinfo", "name": "podinfo", "labels": []any{"FluxHelmRelease"},
				"resource_type": "", "provider": "", "environment": "",
				"repo_id": "repo-gitops", "config_path": "clusters/production/helmrelease.yaml",
			},
		}},
	}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}

	req := resourceInvestigationRequest{Query: "podinfo", Limit: 5}
	if err := req.normalize(); err != nil {
		t.Fatalf("req.normalize() error = %v, want nil", err)
	}

	selected, resolution, err := handler.resolveResourceInvestigationTarget(context.Background(), req)
	if err != nil {
		t.Fatalf("resolveResourceInvestigationTarget() error = %v, want nil", err)
	}
	if !strings.Contains(graph.runCalls[0].cypher, "n:FluxHelmRelease") {
		t.Fatalf("resolver cypher does not carry the default n:FluxHelmRelease disjunct: %s", graph.runCalls[0].cypher)
	}
	if got, want := resolution["status"], "resolved"; got != want {
		t.Fatalf("resolution.status = %#v, want %#v (the FluxHelmRelease fixture node must resolve through the default predicate)", got, want)
	}
	if selected == nil {
		t.Fatal("selected candidate = nil, want the resolved FluxHelmRelease node")
	}
	if len(selected.Labels) != 1 || selected.Labels[0] != "FluxHelmRelease" {
		t.Fatalf("selected.Labels = %#v, want [FluxHelmRelease]", selected.Labels)
	}
}
