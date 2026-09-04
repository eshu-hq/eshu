// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

const (
	goldenServiceChangedSinceServiceID        = "component:default/deployable-config"
	goldenServiceChangedSinceBaselineOwner    = "group:default/platform"
	goldenServiceChangedSinceCurrentOwner     = "group:default/runtime-platform"
	goldenServiceChangedSinceBaselineOwnerKey = "ownership:component:default/deployable-config:group:default/platform"
	goldenServiceChangedSinceCurrentOwnerKey  = "ownership:component:default/deployable-config:group:default/runtime-platform"
)

func TestGoldenServiceChangedSinceOwnerChangeCreatesDistinctIdempotentGeneration(t *testing.T) {
	t.Parallel()

	baselineWrite := goldenServiceChangedSinceWrite(goldenServiceChangedSinceBaselineOwner)
	currentWrite := goldenServiceChangedSinceWrite(goldenServiceChangedSinceCurrentOwner)
	baseline := ServiceMaterializationGenerationID(baselineWrite)
	current := ServiceMaterializationGenerationID(currentWrite)
	if baseline == current {
		t.Fatal("baseline and current generation IDs are equal despite an owner change")
	}
	if repeated := ServiceMaterializationGenerationID(goldenServiceChangedSinceWrite(goldenServiceChangedSinceCurrentOwner)); repeated != current {
		t.Errorf("identical current write generation = %q, want idempotent %q", repeated, current)
	}

	if got := ServiceOwnershipEvidenceKey(goldenServiceChangedSinceServiceID, goldenServiceChangedSinceBaselineOwner); got != goldenServiceChangedSinceBaselineOwnerKey {
		t.Errorf("baseline ownership stable key = %q, want %q", got, goldenServiceChangedSinceBaselineOwnerKey)
	}
	if got := ServiceOwnershipEvidenceKey(goldenServiceChangedSinceServiceID, goldenServiceChangedSinceCurrentOwner); got != goldenServiceChangedSinceCurrentOwnerKey {
		t.Errorf("current ownership stable key = %q, want %q", got, goldenServiceChangedSinceCurrentOwnerKey)
	}
	if baselineHash, currentHash := ServiceEvidencePayloadHash(baselineWrite.Ownership[0].Payload),
		ServiceEvidencePayloadHash(currentWrite.Ownership[0].Payload); baselineHash == currentHash {
		t.Errorf("baseline and current payload hashes are equal: %q", baselineHash)
	}
}

func goldenServiceChangedSinceWrite(ownerRef string) ServiceMaterializationWrite {
	return ServiceMaterializationWrite{
		IntentID:    "golden-service-changed-since",
		ServiceID:   goldenServiceChangedSinceServiceID,
		TriggerKind: "golden_corpus",
		Ownership: []ServiceOwnershipEvidence{{
			OwnerRef: ownerRef,
			Payload: map[string]any{
				"owner_ref":  ownerRef,
				"provider":   "backstage",
				"entity_ref": goldenServiceChangedSinceServiceID,
				"lifecycle":  "production",
				"tier":       "",
			},
		}},
	}
}
