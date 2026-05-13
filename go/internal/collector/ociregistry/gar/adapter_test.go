package gar

import (
	"strings"
	"testing"
)

func TestDistributionBaseURLUsesDockerPkgHost(t *testing.T) {
	t.Parallel()

	got, err := DistributionBaseURL("us-west1-docker.pkg.dev")
	if err != nil {
		t.Fatalf("DistributionBaseURL() error = %v", err)
	}
	if want := "https://us-west1-docker.pkg.dev"; got != want {
		t.Fatalf("DistributionBaseURL() = %q, want %q", got, want)
	}
}

func TestDistributionBaseURLRejectsNonGARHostAndCredentials(t *testing.T) {
	t.Parallel()

	if _, err := DistributionBaseURL("https://registry.example.com"); err == nil {
		t.Fatal("DistributionBaseURL() error = nil for non-GAR host")
	}
	_, err := DistributionBaseURL("https://_json_key:secret@us-west1-docker.pkg.dev")
	if err == nil {
		t.Fatal("DistributionBaseURL() error = nil for credentialed URL")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("DistributionBaseURL() leaked credential in error: %v", err)
	}
}

func TestRepositoryIdentityUsesGARProvider(t *testing.T) {
	t.Parallel()

	identity, err := RepositoryIdentity("us-west1-docker.pkg.dev", "example-project/team-api/service")
	if err != nil {
		t.Fatalf("RepositoryIdentity() error = %v", err)
	}
	if got, want := string(identity.Provider), "google_artifact_registry"; got != want {
		t.Fatalf("Provider = %q, want %q", got, want)
	}
	if got, want := identity.Repository, "example-project/team-api/service"; got != want {
		t.Fatalf("Repository = %q, want %q", got, want)
	}
}
