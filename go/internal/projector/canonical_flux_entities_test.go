// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractEntitiesCapturesFluxTypedEntities is the issue #5360 PR A
// silent-drop regression: entityTypeLabelMap gates extractEntities (via
// EntityTypeLabel) before a content_entity fact ever becomes an EntityRow.
// Before FluxKustomization/FluxGitRepository/FluxOCIRepository/FluxBucket
// (issue #5360 PR A) and FluxHelmRelease/FluxHelmRepository (issue #5483 C1)
// were registered there, a content_entity fact carrying one of these
// entity_type values was silently skipped (continue on !ok) -- the fact
// existed, but no node was ever produced for the canonical graph writer, the
// same class of gap #5346/#5347 hit for other typed entities. This test
// proves all six labels now reach EntityRow instead of being dropped.
func TestExtractEntitiesCapturesFluxTypedEntities(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
	}
	fluxLabels := []string{
		"FluxKustomization",
		"FluxGitRepository",
		"FluxOCIRepository",
		"FluxBucket",
		"FluxHelmRelease",
		"FluxHelmRepository",
	}
	for i, label := range fluxLabels {
		envelopes = append(envelopes, facts.Envelope{
			FactID:   "e-" + label,
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_id":     "entity-" + label,
				"entity_type":   label,
				"entity_name":   "flux-system",
				"relative_path": "clusters/production/flux-system.yaml",
				"start_line":    i + 1,
				"end_line":      i + 1,
				"language":      "yaml",
				"repo_id":       "repo-abc",
			},
		})
	}

	result, _ := buildCanonicalMaterialization(sc, gen, envelopes)

	if len(result.Entities) != len(fluxLabels) {
		var gotLabels []string
		for _, e := range result.Entities {
			gotLabels = append(gotLabels, e.Label)
		}
		t.Fatalf("len(Entities) = %d, want %d; labels=%v (a Flux typed entity was silently dropped)",
			len(result.Entities), len(fluxLabels), gotLabels)
	}

	seen := make(map[string]bool, len(result.Entities))
	for _, e := range result.Entities {
		seen[e.Label] = true
	}
	for _, want := range fluxLabels {
		if !seen[want] {
			t.Errorf("entity label %q missing from extracted entities; want it present (not silently dropped)", want)
		}
	}
}
