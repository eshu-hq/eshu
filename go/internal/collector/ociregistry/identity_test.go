package ociregistry

import "testing"

const sha256Digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

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

func TestNormalizeDescriptorIdentityValidatesDigestAndMediaType(t *testing.T) {
	t.Parallel()

	identity := DescriptorIdentity{
		Repository: RepositoryIdentity{
			Provider:   ProviderJFrog,
			Registry:   "https://jfrog.example/artifactory/api/docker/prod",
			Repository: "team/api",
		},
		Digest:    "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		MediaType: "application/vnd.oci.image.manifest.v1+json",
	}

	normalized, err := NormalizeDescriptorIdentity(identity)
	if err != nil {
		t.Fatalf("NormalizeDescriptorIdentity() error = %v", err)
	}

	if normalized.Digest != sha256Digest {
		t.Fatalf("Digest = %q, want %q", normalized.Digest, sha256Digest)
	}
	if normalized.DescriptorID != "oci-descriptor://jfrog.example/artifactory/api/docker/prod/team/api@"+sha256Digest {
		t.Fatalf("DescriptorID = %q", normalized.DescriptorID)
	}

	badDigest := identity
	badDigest.Digest = "latest"
	if _, err := NormalizeDescriptorIdentity(badDigest); err == nil {
		t.Fatalf("NormalizeDescriptorIdentity(invalid digest) error = nil, want non-nil")
	}

	badMediaType := identity
	badMediaType.MediaType = "not-a-media-type"
	if _, err := NormalizeDescriptorIdentity(badMediaType); err == nil {
		t.Fatalf("NormalizeDescriptorIdentity(invalid media type) error = nil, want non-nil")
	}
}
