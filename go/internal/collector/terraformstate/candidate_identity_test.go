package terraformstate

import (
	"strings"
	"testing"
)

func TestCandidatePlanningIDIncludesVersionIdentity(t *testing.T) {
	t.Parallel()

	first := DiscoveryCandidate{
		State: StateKey{
			BackendKind: BackendS3,
			Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
			VersionID:   "version-a",
		},
		Source: DiscoveryCandidateSourceSeed,
		Region: "us-east-1",
	}
	second := first
	second.State.VersionID = "version-b"

	firstID, err := CandidatePlanningID(first)
	if err != nil {
		t.Fatalf("CandidatePlanningID() error = %v, want nil", err)
	}
	secondID, err := CandidatePlanningID(second)
	if err != nil {
		t.Fatalf("CandidatePlanningID() error = %v, want nil", err)
	}

	if firstID == secondID {
		t.Fatalf("CandidatePlanningID() = %q for both versions, want distinct IDs", firstID)
	}
	for _, raw := range []string{"tfstate-prod", "services/api/terraform.tfstate", "version-a", "version-b"} {
		if strings.Contains(firstID, raw) || strings.Contains(secondID, raw) {
			t.Fatalf("candidate planning IDs must not expose raw state identity: %q / %q", firstID, secondID)
		}
	}
}

func TestCandidatePlanningIDRejectsInvalidCandidate(t *testing.T) {
	t.Parallel()

	_, err := CandidatePlanningID(DiscoveryCandidate{
		State: StateKey{
			BackendKind: BackendS3,
			Locator:     "s3://tfstate-prod/",
		},
		Source: DiscoveryCandidateSourceSeed,
	})
	if err == nil {
		t.Fatal("CandidatePlanningID() error = nil, want invalid candidate error")
	}
}
