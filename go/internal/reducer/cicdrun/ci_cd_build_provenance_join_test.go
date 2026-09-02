// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestBuildCICDImageIdentityIndexReadsBuildProvenance is the consumer half: the
// index must decode the narrow set, and a payload that omits the key entirely
// must decode to the same empty set as one that declares it empty. Narrowing
// treats both identically, so neither can be selected.
//
// The producer half of this contract -- container_image_identity's own
// publication of build_provenance_repository_ids -- lives in
// internal/reducer/container_image_identity_build_provenance_join_test.go
// (issue #6061): this family cannot import the reducer root, so the two
// halves of the pin live one on each side of the seam.
func TestBuildCICDImageIdentityIndexReadsBuildProvenance(t *testing.T) {
	t.Parallel()

	const (
		digest       = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
		legacyDigest = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
		buildingRepo = "repository:r_actually_built"
	)

	index := buildCICDImageIdentityIndex([]facts.Envelope{
		{
			FactID:   "identity-current",
			FactKind: reducercontract.ContainerImageIdentityFactKind,
			Payload: map[string]any{
				"digest":                          digest,
				"source_repository_ids":           []any{buildingRepo, "repository:r_deploys_only"},
				"build_provenance_repository_ids": []any{buildingRepo},
			},
		},
		{
			FactID:   "identity-legacy",
			FactKind: reducercontract.ContainerImageIdentityFactKind,
			Payload: map[string]any{
				"digest":                digest,
				"source_repository_ids": []any{buildingRepo},
			},
		},
		{
			FactID:   "identity-current-empty-provenance",
			FactKind: reducercontract.ContainerImageIdentityFactKind,
			Payload: map[string]any{
				"digest":                          legacyDigest,
				"source_repository_ids":           []any{buildingRepo},
				"build_provenance_repository_ids": []any{},
			},
		},
	})

	current := index[digest][0]
	if !slices.Equal(current.buildProvenanceRepositoryIDs, []string{buildingRepo}) {
		t.Fatalf("identity-current: buildProvenanceRepositoryIDs = %#v, want %q", current.buildProvenanceRepositoryIDs, buildingRepo)
	}

	// A row published before the key existed decodes to an empty set, so it can
	// never be selected by narrowing. That reproduces the pre-#5766 behavior for
	// legacy rows instead of degrading it -- see cicdImageMatchesForRepository.
	if legacy := index[digest][1]; len(legacy.buildProvenanceRepositoryIDs) != 0 {
		t.Fatalf("identity-legacy: buildProvenanceRepositoryIDs = %#v, want empty", legacy.buildProvenanceRepositoryIDs)
	}

	if empty := index[legacyDigest][0]; len(empty.buildProvenanceRepositoryIDs) != 0 {
		t.Fatalf("identity-current-empty-provenance: buildProvenanceRepositoryIDs = %#v, want empty", empty.buildProvenanceRepositoryIDs)
	}
}
