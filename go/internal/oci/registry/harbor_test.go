// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"strings"
	"testing"
)

func TestHarborDistributionBaseURLUsesHarborRegistryEndpoint(t *testing.T) {
	t.Parallel()

	got, err := harborDistributionBaseURL(" https://harbor.example.com/ ")
	if err != nil {
		t.Fatalf("harborDistributionBaseURL() error = %v", err)
	}
	if want := "https://harbor.example.com"; got != want {
		t.Fatalf("harborDistributionBaseURL() = %q, want %q", got, want)
	}
}

func TestHarborDistributionBaseURLRejectsCredentials(t *testing.T) {
	t.Parallel()

	_, err := harborDistributionBaseURL("https://robot$reader:secret@harbor.example.com")
	if err == nil {
		t.Fatal("harborDistributionBaseURL() error = nil for credentialed URL")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("harborDistributionBaseURL() leaked credential in error: %v", err)
	}
}

func TestHarborRepositoryIdentityUsesHarborProvider(t *testing.T) {
	t.Parallel()

	identity, err := HarborRepositoryIdentity("https://harbor.example.com", "Project/API")
	if err != nil {
		t.Fatalf("HarborRepositoryIdentity() error = %v", err)
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
