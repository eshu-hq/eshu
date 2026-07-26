// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestContainerImageIdentityPayloadPersistsBuildProvenanceRepositoryIDs is the
// the contract this consumer depends on. The identity domain computes the narrow
// "genuinely built this image" set -- #5796 gates its own BUILT_FROM projection
// on it -- and publishes it as build_provenance_repository_ids. The emission
// itself is main's since #5817; this asserts the shape the CI/CD join reads, so
// a producer-side change that dropped or renamed the key fails here too.
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
// key is always written, even when no repository has build evidence, so a fact
// inspected directly is self-describing rather than ambiguous about whether the
// producer computed build provenance at all. No consumer branches on key
// presence: this join treats an absent key and an empty one the same way, and
// neither is ever selected.
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

// TestBuildCICDImageIdentityIndexReadsBuildProvenance is the consumer half: the
// index must decode the narrow set, and a payload that omits the key entirely
// must decode to the same empty set as one that declares it empty. Narrowing
// treats both identically, so neither can be selected.
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
