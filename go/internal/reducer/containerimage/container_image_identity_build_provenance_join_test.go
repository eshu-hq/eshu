// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"slices"
	"testing"
)

// TestContainerImageIdentityPayloadPersistsBuildProvenanceRepositoryIDs is the
// the contract this consumer depends on. The identity domain computes the narrow
// "genuinely built this image" set -- #5796 gates its own BUILT_FROM projection
// on it -- and publishes it as build_provenance_repository_ids. The emission
// itself is main's since #5817; this asserts the shape the CI/CD join reads, so
// a producer-side change that dropped or renamed the key fails here too.
//
// The consumer half of this contract -- the ci_cd_run_correlation family's own
// decode of build_provenance_repository_ids -- lives in
// internal/reducer/cicdrun/ci_cd_build_provenance_join_test.go (issue #6061):
// that family cannot import this root package, so the two halves of the pin
// live one on each side of the seam.
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
