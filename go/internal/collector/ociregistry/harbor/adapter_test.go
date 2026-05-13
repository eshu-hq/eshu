package harbor

import (
	"strings"
	"testing"
)

func TestDistributionBaseURLUsesHarborRegistryEndpoint(t *testing.T) {
	t.Parallel()

	got, err := DistributionBaseURL(" https://harbor.example.com/ ")
	if err != nil {
		t.Fatalf("DistributionBaseURL() error = %v", err)
	}
	if want := "https://harbor.example.com"; got != want {
		t.Fatalf("DistributionBaseURL() = %q, want %q", got, want)
	}
}

func TestDistributionBaseURLRejectsCredentials(t *testing.T) {
	t.Parallel()

	_, err := DistributionBaseURL("https://robot$reader:secret@harbor.example.com")
	if err == nil {
		t.Fatal("DistributionBaseURL() error = nil for credentialed URL")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("DistributionBaseURL() leaked credential in error: %v", err)
	}
}

func TestRepositoryIdentityUsesHarborProvider(t *testing.T) {
	t.Parallel()

	identity, err := RepositoryIdentity("https://harbor.example.com", "Project/API")
	if err != nil {
		t.Fatalf("RepositoryIdentity() error = %v", err)
	}
	if got, want := string(identity.Provider), "harbor"; got != want {
		t.Fatalf("Provider = %q, want %q", got, want)
	}
	if got, want := identity.Registry, "https://harbor.example.com"; got != want {
		t.Fatalf("Registry = %q, want %q", got, want)
	}
	if got, want := identity.Repository, "project/api"; got != want {
		t.Fatalf("Repository = %q, want %q", got, want)
	}
}
