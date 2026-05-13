package acr

import (
	"strings"
	"testing"
)

func TestDistributionBaseURLUsesAzureCRHost(t *testing.T) {
	t.Parallel()

	got, err := DistributionBaseURL("example.azurecr.io")
	if err != nil {
		t.Fatalf("DistributionBaseURL() error = %v", err)
	}
	if want := "https://example.azurecr.io"; got != want {
		t.Fatalf("DistributionBaseURL() = %q, want %q", got, want)
	}
}

func TestDistributionBaseURLRejectsNonACRHostAndCredentials(t *testing.T) {
	t.Parallel()

	if _, err := DistributionBaseURL("https://registry.example.com"); err == nil {
		t.Fatal("DistributionBaseURL() error = nil for non-ACR host")
	}
	_, err := DistributionBaseURL("https://00000000-0000-0000-0000-000000000000:secret@example.azurecr.io")
	if err == nil {
		t.Fatal("DistributionBaseURL() error = nil for credentialed URL")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("DistributionBaseURL() leaked credential in error: %v", err)
	}
}

func TestRepositoryIdentityUsesACRProvider(t *testing.T) {
	t.Parallel()

	identity, err := RepositoryIdentity("example.azurecr.io", "samples/artifact")
	if err != nil {
		t.Fatalf("RepositoryIdentity() error = %v", err)
	}
	if got, want := string(identity.Provider), "azure_container_registry"; got != want {
		t.Fatalf("Provider = %q, want %q", got, want)
	}
	if got, want := identity.Repository, "samples/artifact"; got != want {
		t.Fatalf("Repository = %q, want %q", got, want)
	}
}
