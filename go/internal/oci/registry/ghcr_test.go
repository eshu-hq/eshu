// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import "testing"

func TestGHCRRepositoryNameNormalizesOwnerAndImage(t *testing.T) {
	t.Parallel()

	got, err := GHCRRepositoryName(" Owner/Image ")
	if err != nil {
		t.Fatalf("GHCRRepositoryName() error = %v", err)
	}
	if got != "owner/image" {
		t.Fatalf("GHCRRepositoryName() = %q, want owner/image", got)
	}
}

func TestGHCRRepositoryIdentityUsesGHCRProvider(t *testing.T) {
	t.Parallel()

	identity, err := GHCRRepositoryIdentity("owner/image")
	if err != nil {
		t.Fatalf("GHCRRepositoryIdentity() error = %v", err)
	}
	if identity.Provider != "ghcr" {
		t.Fatalf("Provider = %q", identity.Provider)
	}
	if identity.Registry != GHCRRegistryHost {
		t.Fatalf("Registry = %q, want %q", identity.Registry, GHCRRegistryHost)
	}
	if identity.Repository != "owner/image" {
		t.Fatalf("Repository = %q, want owner/image", identity.Repository)
	}
}

func TestGHCRRepositoryNameRejectsInvalidNames(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "image", "owner//image"} {
		if _, err := GHCRRepositoryName(input); err == nil {
			t.Fatalf("GHCRRepositoryName(%q) error = nil", input)
		}
	}
}
