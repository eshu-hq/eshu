// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package flux

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// fluxSourceRowEntity builds a FluxGitRepository/FluxOCIRepository/FluxBucket
// EntityRow with the fields sourceRef resolution needs. name == "" mirrors a
// generateName-only CR (never joinable).
func fluxSourceRowEntity(label, uid, name, namespace, filePath string) canonical.EntityRow {
	meta := map[string]any{}
	if namespace != "" {
		meta["namespace"] = namespace
	}
	return canonical.EntityRow{
		Label:      label,
		EntityID:   uid,
		EntityName: name,
		FilePath:   filePath,
		Metadata:   meta,
	}
}

// TestCollectFluxSourceEntitiesExcludesEmptyNameCandidates proves the
// candidate-collection guard directly: a generateName-only source CR (empty
// EntityName) is never inserted into the (label,name)-keyed candidate map,
// even though its FilePath/namespace fields are otherwise well-formed. This is
// the flux.md-documented invariant that an empty-name source must never
// false-join a Kustomization whose sourceRef.name is also empty -- exercised
// directly since the only current caller (collectFluxKustomizationEntities)
// separately guarantees no Kustomization ever reaches resolution with an
// empty sourceRef.name, making the two guards intentionally redundant
// (defense-in-depth), not mutually exclusive.
func TestCollectFluxSourceEntitiesExcludesEmptyNameCandidates(t *testing.T) {
	t.Parallel()

	entities := []canonical.EntityRow{
		fluxSourceRowEntity("FluxGitRepository", "uid-named", "flux-system", "flux-system", "/repo/sources.yaml"),
		fluxSourceRowEntity("FluxGitRepository", "uid-generated", "", "flux-system", "/repo/sources.yaml"),
	}

	got := collectFluxSourceEntities(entities)

	if candidates := got["FluxGitRepository\x00flux-system"]; len(candidates) != 1 || candidates[0].uid != "uid-named" {
		t.Fatalf("candidates for flux-system = %+v, want exactly [uid-named]", candidates)
	}
	if candidates, ok := got["FluxGitRepository\x00"]; ok || len(candidates) != 0 {
		t.Fatalf("empty-name key must never be populated, got %+v", candidates)
	}
}
