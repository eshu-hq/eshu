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

func TestCandidateFromSeedCarriesPriorETagMetadata(t *testing.T) {
	t.Parallel()

	candidate, err := candidateFromSeed(DiscoverySeed{
		Kind:         BackendS3,
		Bucket:       "tfstate-prod",
		Key:          "services/api/terraform.tfstate",
		Region:       "us-east-1",
		PreviousETag: " \t\"etag-123\"\t ",
	})
	if err != nil {
		t.Fatalf("candidateFromSeed() error = %v, want nil", err)
	}

	if got, want := candidate.PreviousETag, " \t\"etag-123\"\t "; got != want {
		t.Fatalf("PreviousETag = %q, want opaque ETag %q", got, want)
	}
}
