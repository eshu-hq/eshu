// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"
)

func TestContainerImageBuiltFromSupportRowsMatchDecisionRows(t *testing.T) {
	t.Parallel()

	decisions := containerImageIdentityGraphEquivalenceDecisions()
	supports := containerImageIdentityGraphEquivalenceSupports(t, decisions)
	if got, want := containerImageBuiltFromSupportRows(supports), containerImageBuiltFromRows(decisions); !reflect.DeepEqual(got, want) {
		t.Fatalf("support BUILT_FROM rows = %#v, want decision rows %#v", got, want)
	}
}

func TestContainerImageDerivedFromSupportRowsMatchDecisionRows(t *testing.T) {
	t.Parallel()

	const repositoryID = "repository:synthetic"
	decisions := containerImageIdentityGraphEquivalenceDecisions()
	supports := containerImageIdentityGraphEquivalenceSupports(t, decisions)
	if got, want := containerImageDerivedFromSupportRows(supports, repositoryID), containerImageDerivedFromRows(decisions, repositoryID); !reflect.DeepEqual(got, want) {
		t.Fatalf("support DERIVED_FROM rows = %#v, want decision rows %#v", got, want)
	}
}

func containerImageIdentityGraphEquivalenceDecisions() []ContainerImageIdentityDecision {
	const repositoryID = "repository:synthetic"
	return []ContainerImageIdentityDecision{
		{
			ImageRef:                     "registry.example.com/team/app@sha256:aaaaaaaa",
			Digest:                       "sha256:aaaaaaaa",
			BuildProvenanceRepositoryIDs: []string{repositoryID, repositoryID},
			Outcome:                      ContainerImageIdentityExactDigest,
			CanonicalWrites:              1,
		},
		{
			ImageRef:                     "registry.example.com/team/app-copy@sha256:aaaaaaaa",
			Digest:                       "sha256:aaaaaaaa",
			BuildProvenanceRepositoryIDs: []string{repositoryID},
			Outcome:                      ContainerImageIdentityExactDigest,
			CanonicalWrites:              1,
		},
		{
			ImageRef:                  "registry.example.com/team/base@sha256:bbbbbbbb",
			Digest:                    "sha256:bbbbbbbb",
			BaseImageForRepositoryIDs: []string{repositoryID},
			Outcome:                   ContainerImageIdentityExactDigest,
			CanonicalWrites:           1,
		},
		{
			ImageRef:                     "registry.example.com/team/tagged:latest",
			Digest:                       "sha256:cccccccc",
			BuildProvenanceRepositoryIDs: []string{repositoryID},
			Outcome:                      ContainerImageIdentityTagResolved,
			CanonicalWrites:              1,
		},
	}
}

func containerImageIdentityGraphEquivalenceSupports(
	t testing.TB,
	decisions []ContainerImageIdentityDecision,
) []containerImageIdentitySupport {
	t.Helper()
	supports := make([]containerImageIdentitySupport, 0, len(decisions))
	for _, decision := range decisions {
		support, err := containerImageIdentitySupportFromDecision(decision)
		if err != nil {
			t.Fatalf("normalize support: %v", err)
		}
		supports = append(supports, support)
	}
	return supports
}
