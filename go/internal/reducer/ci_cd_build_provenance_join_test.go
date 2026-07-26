// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestContainerImageIdentityPayloadPersistsBuildProvenanceRepositoryIDs is the
// producer half of #5823. The identity domain already computes the narrow
// "genuinely built this image" set -- #5796 gates its own BUILT_FROM projection
// on it -- but published only the broad source_repository_ids, so every
// cross-domain consumer was forced onto the reference-conflating join.
func TestContainerImageIdentityPayloadPersistsBuildProvenanceRepositoryIDs(t *testing.T) {
	t.Parallel()

	const buildingRepo = "repository:r_actually_built"

	payload := containerImageIdentityPayload(
		ContainerImageIdentityWrite{
			IntentID:     "intent-1",
			ScopeID:      "scope-1",
			GenerationID: "generation-1",
		},
		ContainerImageIdentityDecision{
			Digest:                       "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			SourceRepositoryIDs:          []string{buildingRepo, "repository:r_deploys_only"},
			BuildProvenanceRepositoryIDs: []string{buildingRepo},
		},
		"canonical-1",
	)

	raw, ok := payload["build_provenance_repository_ids"]
	if !ok {
		t.Fatalf("containerImageIdentityPayload() omitted build_provenance_repository_ids: %#v", payload)
	}
	got, ok := raw.([]string)
	if !ok {
		t.Fatalf("build_provenance_repository_ids = %T, want []string", raw)
	}
	if !slices.Equal(got, []string{buildingRepo}) {
		t.Fatalf("build_provenance_repository_ids = %#v, want only the build-evidence repository", got)
	}
}

// TestContainerImageIdentityPayloadEmitsEmptyBuildProvenanceKey pins that the
// key is always written, even when no repository has build evidence. A consumer
// distinguishes "this generation publishes build provenance and it names nobody"
// from "this fact predates the field" by key presence alone, so an omitted key
// on an empty set would be read as legacy and silently re-enable the broad join.
func TestContainerImageIdentityPayloadEmitsEmptyBuildProvenanceKey(t *testing.T) {
	t.Parallel()

	payload := containerImageIdentityPayload(
		ContainerImageIdentityWrite{IntentID: "intent-1", ScopeID: "scope-1"},
		ContainerImageIdentityDecision{
			Digest:              "sha256:2222222222222222222222222222222222222222222222222222222222222222",
			SourceRepositoryIDs: []string{"repository:r_deploys_only"},
		},
		"canonical-1",
	)

	if _, ok := payload["build_provenance_repository_ids"]; !ok {
		t.Fatalf("containerImageIdentityPayload() omitted the key for an empty build-provenance set: %#v", payload)
	}
}

// TestBuildCICDImageIdentityIndexReadsBuildProvenance is the consumer half:
// the index must carry both the narrow set and whether the payload declared it.
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
			FactKind: containerImageIdentityFactKind,
			Payload: map[string]any{
				"digest":                          digest,
				"source_repository_ids":           []any{buildingRepo, "repository:r_deploys_only"},
				"build_provenance_repository_ids": []any{buildingRepo},
			},
		},
		{
			FactID:   "identity-legacy",
			FactKind: containerImageIdentityFactKind,
			Payload: map[string]any{
				"digest":                digest,
				"source_repository_ids": []any{buildingRepo},
			},
		},
		{
			FactID:   "identity-current-empty-provenance",
			FactKind: containerImageIdentityFactKind,
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
