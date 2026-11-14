// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"strings"
	"testing"
)

func TestGARDistributionBaseURLUsesDockerPkgHost(t *testing.T) {
	t.Parallel()

	got, err := garDistributionBaseURL("us-west1-docker.pkg.dev")
	if err != nil {
		t.Fatalf("garDistributionBaseURL() error = %v", err)
	}
	if want := "https://us-west1-docker.pkg.dev"; got != want {
		t.Fatalf("garDistributionBaseURL() = %q, want %q", got, want)
	}
}

func TestGARDistributionBaseURLRejectsNonGARHostAndCredentials(t *testing.T) {
	t.Parallel()

	if _, err := garDistributionBaseURL("https://registry.example.com"); err == nil {
		t.Fatal("garDistributionBaseURL() error = nil for non-GAR host")
	}
	_, err := garDistributionBaseURL("https://_json_key:secret@us-west1-docker.pkg.dev")
	if err == nil {
		t.Fatal("garDistributionBaseURL() error = nil for credentialed URL")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("garDistributionBaseURL() leaked credential in error: %v", err)
	}
}

func TestGARRepositoryIdentityUsesGARProvider(t *testing.T) {
	t.Parallel()

	identity, err := GARRepositoryIdentity("us-west1-docker.pkg.dev", "Example-Project/Team-API/Service")
	if err != nil {
		t.Fatalf("GARRepositoryIdentity() error = %v", err)
	}
	if got, want := string(identity.Provider), "google_artifact_registry"; got != want {
		t.Fatalf("Provider = %q, want %q", got, want)
	}
	if got, want := identity.Repository, "example-project/team-api/service"; got != want {
		t.Fatalf("Repository = %q, want %q", got, want)
	}
}
