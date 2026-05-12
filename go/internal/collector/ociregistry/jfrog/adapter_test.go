package jfrog

import (
	"strings"
	"testing"
)

func TestDistributionBaseURLBuildsArtifactoryDockerEndpoint(t *testing.T) {
	t.Parallel()

	got, err := DistributionBaseURL("https://example.jfrog.io/", "docker-local")
	if err != nil {
		t.Fatalf("DistributionBaseURL() error = %v", err)
	}
	want := "https://example.jfrog.io/artifactory/api/docker/docker-local"
	if got != want {
		t.Fatalf("DistributionBaseURL() = %q, want %q", got, want)
	}
}

func TestRepositoryIdentityUsesJFrogProvider(t *testing.T) {
	t.Parallel()

	identity, err := RepositoryIdentity("https://example.jfrog.io", "docker-local", "team/api")
	if err != nil {
		t.Fatalf("RepositoryIdentity() error = %v", err)
	}
	if identity.Provider != "jfrog" {
		t.Fatalf("Provider = %q", identity.Provider)
	}
	if !strings.Contains(identity.Registry, "/artifactory/api/docker/docker-local") {
		t.Fatalf("Registry = %q", identity.Registry)
	}
	if identity.Repository != "team/api" {
		t.Fatalf("Repository = %q", identity.Repository)
	}
}

func TestDistributionBaseURLRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()

	if _, err := DistributionBaseURL("", "docker-local"); err == nil {
		t.Fatal("DistributionBaseURL() error = nil for blank base URL")
	}
	if _, err := DistributionBaseURL("https://example.jfrog.io", ""); err == nil {
		t.Fatal("DistributionBaseURL() error = nil for blank repository key")
	}
}
