// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"strings"
	"testing"
)

func TestACRDistributionBaseURLUsesAzureCRHost(t *testing.T) {
	t.Parallel()

	got, err := acrDistributionBaseURL("example.azurecr.io")
	if err != nil {
		t.Fatalf("acrDistributionBaseURL() error = %v", err)
	}
	if want := "https://example.azurecr.io"; got != want {
		t.Fatalf("acrDistributionBaseURL() = %q, want %q", got, want)
	}
}

func TestACRDistributionBaseURLRejectsNonACRHostAndCredentials(t *testing.T) {
	t.Parallel()

	if _, err := acrDistributionBaseURL("https://registry.example.com"); err == nil {
		t.Fatal("acrDistributionBaseURL() error = nil for non-ACR host")
	}
	_, err := acrDistributionBaseURL("https://00000000-0000-0000-0000-000000000000:secret@example.azurecr.io")
	if err == nil {
		t.Fatal("acrDistributionBaseURL() error = nil for credentialed URL")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("acrDistributionBaseURL() leaked credential in error: %v", err)
	}
}

func TestACRRepositoryIdentityUsesACRProvider(t *testing.T) {
	t.Parallel()

	identity, err := ACRRepositoryIdentity("example.azurecr.io", "samples/artifact")
	if err != nil {
		t.Fatalf("ACRRepositoryIdentity() error = %v", err)
	}
	if got, want := string(identity.Provider), "azure_container_registry"; got != want {
		t.Fatalf("Provider = %q, want %q", got, want)
	}
	if got, want := identity.Repository, "samples/artifact"; got != want {
		t.Fatalf("Repository = %q, want %q", got, want)
	}
}
