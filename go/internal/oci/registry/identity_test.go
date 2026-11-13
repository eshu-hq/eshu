// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import "testing"

func TestNormalizeRepositoryIdentityBuildsStableScope(t *testing.T) {
	t.Parallel()

	identity := RepositoryIdentity{
		Provider:   ProviderECR,
		Registry:   "https://AWS:secret@123456789012.dkr.ecr.us-east-1.amazonaws.com/",
		Repository: "/Team/API-Service/",
	}

	normalized, err := NormalizeRepositoryIdentity(identity)
	if err != nil {
		t.Fatalf("NormalizeRepositoryIdentity() error = %v", err)
	}

	if normalized.Provider != ProviderECR {
		t.Fatalf("Provider = %q, want %q", normalized.Provider, ProviderECR)
	}
	if normalized.Registry != "123456789012.dkr.ecr.us-east-1.amazonaws.com" {
		t.Fatalf("Registry = %q", normalized.Registry)
	}
	if normalized.Repository != "team/api-service" {
		t.Fatalf("Repository = %q", normalized.Repository)
	}
	if normalized.RepositoryID != "oci-registry://123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api-service" {
		t.Fatalf("RepositoryID = %q", normalized.RepositoryID)
	}
	if normalized.ScopeID != normalized.RepositoryID {
		t.Fatalf("ScopeID = %q, want repository id", normalized.ScopeID)
	}
}

func TestNormalizeRepositoryIdentityLowercasesBareRegistryHost(t *testing.T) {
	t.Parallel()

	normalized, err := NormalizeRepositoryIdentity(RepositoryIdentity{
		Provider:   ProviderJFrog,
		Registry:   "JFrog.Example/Artifactory/API/Docker/Prod",
		Repository: "Team/API",
	})
	if err != nil {
		t.Fatalf("NormalizeRepositoryIdentity() error = %v", err)
	}
	if normalized.Registry != "jfrog.example/Artifactory/API/Docker/Prod" {
		t.Fatalf("Registry = %q", normalized.Registry)
	}
}

func TestNormalizeRepositoryIdentityRejectsBlankRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		identity RepositoryIdentity
	}{
		{name: "provider", identity: RepositoryIdentity{Registry: "registry.example", Repository: "team/api"}},
		{name: "registry", identity: RepositoryIdentity{Provider: ProviderJFrog, Repository: "team/api"}},
		{name: "repository", identity: RepositoryIdentity{Provider: ProviderJFrog, Registry: "registry.example"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NormalizeRepositoryIdentity(tt.identity); err == nil {
				t.Fatalf("NormalizeRepositoryIdentity(%s) error = nil, want non-nil", tt.name)
			}
		})
	}
}
