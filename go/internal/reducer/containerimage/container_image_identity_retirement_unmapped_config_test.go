// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"slices"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestPlanContainerImageIdentityRetirementHoldsOnlyRepositoryWithUnmappedConfigWarning(
	t *testing.T,
) {
	t.Parallel()

	const otherRepositoryID = "oci-registry://registry.example.com/team/worker"
	affected := ContainerImageIdentityDecision{
		ImageRef:     "registry.example.com/team/api@" + retirementTestDigest,
		Digest:       retirementTestDigest,
		RepositoryID: retirementTestRepositoryID,
		Outcome:      reducercontract.ContainerImageIdentityUnresolved,
	}
	unrelated := ContainerImageIdentityDecision{
		ImageRef:     "registry.example.com/team/worker@" + retirementTestDigest,
		Digest:       retirementTestDigest,
		RepositoryID: otherRepositoryID,
		Outcome:      reducercontract.ContainerImageIdentityUnresolved,
	}
	write := retirementTestWrite(affected)
	write.Decisions = []ContainerImageIdentityDecision{affected, unrelated}

	plan, err := planContainerImageIdentityRetirement(
		write,
		nil,
		[]facts.Envelope{
			retirementWarningEnvelope("config_blob_unavailable", retirementTestConfigDigest),
		},
	)
	if err != nil {
		t.Fatalf("planContainerImageIdentityRetirement() error = %v", err)
	}
	if got := plan.HeldByReason[containerImageIdentityRetireHoldConfigBlobUnavailable]; got != 1 {
		t.Fatalf("config-blob holds = %d, want 1 for the affected repository", got)
	}
	if len(plan.Tombstones) != 1 || plan.Tombstones[0].RepositoryID != otherRepositoryID {
		t.Fatalf("tombstones = %#v, want only unrelated repository", plan.Tombstones)
	}
	affectedLegacyID := legacyContainerImageIdentityFactID(
		write,
		containerImageIdentityDecisionWithOutcome(
			affected,
			reducercontract.ContainerImageIdentityExactDigest,
		),
	)
	if slices.Contains(plan.LegacyFactIDs, affectedLegacyID) {
		t.Fatalf("legacy cleanup includes held affected row %q", affectedLegacyID)
	}
	unrelatedLegacyID := legacyContainerImageIdentityFactID(
		write,
		containerImageIdentityDecisionWithOutcome(
			unrelated,
			reducercontract.ContainerImageIdentityExactDigest,
		),
	)
	if !slices.Contains(plan.LegacyFactIDs, unrelatedLegacyID) {
		t.Fatalf("legacy cleanup = %v, want unrelated row %q", plan.LegacyFactIDs, unrelatedLegacyID)
	}
}

func TestPlanContainerImageIdentityRetirementRejectsMalformedManifestDigestMapping(
	t *testing.T,
) {
	t.Parallel()

	affected := ContainerImageIdentityDecision{
		ImageRef:     "registry.example.com/team/api@" + retirementTestDigest,
		Digest:       retirementTestDigest,
		RepositoryID: retirementTestRepositoryID,
		Outcome:      reducercontract.ContainerImageIdentityUnresolved,
	}
	write := retirementTestWrite(affected)
	plan, err := planContainerImageIdentityRetirement(
		write,
		[]facts.Envelope{{
			FactID:   "manifest-malformed-digest-5854",
			FactKind: facts.OCIImageManifestFactKind,
			Payload: map[string]any{
				"repository_id": retirementTestRepositoryID,
				"digest":        "sha256:not-a-usable-manifest-digest",
				"config": map[string]any{
					"digest": retirementTestConfigDigest,
				},
			},
		}},
		[]facts.Envelope{
			retirementWarningEnvelope("config_blob_unavailable", retirementTestConfigDigest),
		},
	)
	if err != nil {
		t.Fatalf("planContainerImageIdentityRetirement() error = %v", err)
	}
	if got := plan.HeldByReason[containerImageIdentityRetireHoldConfigBlobUnavailable]; got != 1 {
		t.Fatalf("config-blob holds = %d, want repository-wide hold", got)
	}
	if len(plan.Tombstones) != 0 || len(plan.LegacyFactIDs) != 0 {
		t.Fatalf(
			"malformed mapping retirement = tombstones %v legacy cleanup %v, want none",
			plan.Tombstones,
			plan.LegacyFactIDs,
		)
	}
}
