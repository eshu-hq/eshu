package terraformstate

import "testing"

func TestCandidateFromSeedRejectsLocalVersionID(t *testing.T) {
	t.Parallel()

	_, err := candidateFromSeed(DiscoverySeed{
		Kind:      BackendLocal,
		Path:      "/workspace/terraform.tfstate",
		VersionID: "version-1",
	})

	if err == nil {
		t.Fatal("candidateFromSeed() error = nil, want non-nil")
	}
}

func TestDiscoveryCandidateValidateRejectsLocalVersionID(t *testing.T) {
	t.Parallel()

	candidate := DiscoveryCandidate{
		State: StateKey{
			BackendKind: BackendLocal,
			Locator:     "/workspace/terraform.tfstate",
			VersionID:   "version-1",
		},
		Source: DiscoveryCandidateSourceSeed,
	}

	if err := candidate.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}
