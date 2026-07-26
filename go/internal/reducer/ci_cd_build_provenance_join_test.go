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
	if !current.buildProvenanceKeyPresent {
		t.Fatalf("identity-current: buildProvenanceKeyPresent = false, want true")
	}
	if !slices.Equal(current.buildProvenanceRepositoryIDs, []string{buildingRepo}) {
		t.Fatalf("identity-current: buildProvenanceRepositoryIDs = %#v, want %q", current.buildProvenanceRepositoryIDs, buildingRepo)
	}

	if legacy := index[digest][1]; legacy.buildProvenanceKeyPresent {
		t.Fatalf("identity-legacy: buildProvenanceKeyPresent = true, want false for a payload without the key")
	}

	empty := index[legacyDigest][0]
	if !empty.buildProvenanceKeyPresent {
		t.Fatalf("identity-current-empty-provenance: an explicitly empty list must still count as present")
	}
	if len(empty.buildProvenanceRepositoryIDs) != 0 {
		t.Fatalf("identity-current-empty-provenance: buildProvenanceRepositoryIDs = %#v, want empty", empty.buildProvenanceRepositoryIDs)
	}
}

// TestApplyCIRunDigestRevisionConfersBuildProvenance closes the asymmetry
// between the ci.run and SLSA digest-anchor paths. applySLSADigestRevision
// appends its anchor's repositories to BuildProvenanceRepositoryIDs; the ci.run
// path appended them only to SourceRepositoryIDs, so a competing decision that
// won the upsert carried its genuine builder in the broad field alone and had
// no build provenance at all -- the same gap class #5808 fixed one path over.
//
// A CI run that reported producing this digest is build evidence regardless of
// whether a commit revision resolved, and regardless of which tier won the
// source-revision race, so the attribution must not sit behind either gate.
func TestApplyCIRunDigestRevisionConfersBuildProvenance(t *testing.T) {
	t.Parallel()

	const (
		digest       = "sha256:5555555555555555555555555555555555555555555555555555555555555555"
		buildingRepo = "repository:r_actually_built"
	)

	decision := ContainerImageIdentityDecision{
		Digest: digest,
		// A stronger tier already resolved the source revision. Build
		// provenance is a set, not a winner-take-all tier, so the ci.run
		// attribution must still land.
		SourceRevisionProvenance: containerImageSourceRevisionOCIConfigLabel,
	}

	applyCIRunDigestRevision(&decision, map[string]ciRunDigestAnchor{
		digest: {
			sourceRepositoryIDs: []string{buildingRepo},
			factIDs:             []string{"ci-run-fact-1"},
		},
	})

	if !slices.Contains(decision.BuildProvenanceRepositoryIDs, buildingRepo) {
		t.Fatalf("BuildProvenanceRepositoryIDs = %#v, want %q: a ci.run that reported producing "+
			"this digest is build evidence for its repository", decision.BuildProvenanceRepositoryIDs, buildingRepo)
	}
}
