// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"strings"
	"testing"
)

func TestJFrogDistributionBaseURLBuildsArtifactoryDockerEndpoint(t *testing.T) {
	t.Parallel()

	got, err := jfrogDistributionBaseURL("https://example.jfrog.io/", "docker-local")
	if err != nil {
		t.Fatalf("jfrogDistributionBaseURL() error = %v", err)
	}
	want := "https://example.jfrog.io/artifactory/api/docker/docker-local"
	if got != want {
		t.Fatalf("jfrogDistributionBaseURL() = %q, want %q", got, want)
	}
}

func TestJFrogRepositoryIdentityUsesJFrogProvider(t *testing.T) {
	t.Parallel()

	identity, err := JFrogRepositoryIdentity("https://example.jfrog.io", "docker-local", "team/api")
	if err != nil {
		t.Fatalf("JFrogRepositoryIdentity() error = %v", err)
	}
	if identity.Provider != "jfrog" {
		t.Fatalf("Provider = %q", identity.Provider)
	}
	if !strings.Contains(identity.Registry, "/artifactory/api/docker/docker-local") {
		t.Fatalf("Registry = %q", identity.Registry)
	}
	if identity.Repository != "team/api" {
		t.Fatalf("Repository = %q, want team/api", identity.Repository)
	}
}

func TestJFrogDistributionBaseURLRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()

	if _, err := jfrogDistributionBaseURL("", "docker-local"); err == nil {
		t.Fatal("jfrogDistributionBaseURL() error = nil for blank base URL")
	}
	if _, err := jfrogDistributionBaseURL("https://example.jfrog.io", ""); err == nil {
		t.Fatal("jfrogDistributionBaseURL() error = nil for blank repository key")
	}
}
