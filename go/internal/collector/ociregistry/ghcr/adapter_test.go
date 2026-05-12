package ghcr

import "testing"

func TestRepositoryNameNormalizesOwnerAndImage(t *testing.T) {
	t.Parallel()

	got, err := RepositoryName(" Owner/Image ")
	if err != nil {
		t.Fatalf("RepositoryName() error = %v", err)
	}
	if got != "owner/image" {
		t.Fatalf("RepositoryName() = %q, want owner/image", got)
	}
}

func TestRepositoryIdentityUsesGHCRProvider(t *testing.T) {
	t.Parallel()

	identity, err := RepositoryIdentity("owner/image")
	if err != nil {
		t.Fatalf("RepositoryIdentity() error = %v", err)
	}
	if identity.Provider != "ghcr" {
		t.Fatalf("Provider = %q", identity.Provider)
	}
	if identity.Registry != RegistryHost {
		t.Fatalf("Registry = %q, want %q", identity.Registry, RegistryHost)
	}
	if identity.Repository != "owner/image" {
		t.Fatalf("Repository = %q, want owner/image", identity.Repository)
	}
}

func TestRepositoryNameRejectsInvalidNames(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "image", "owner//image"} {
		if _, err := RepositoryName(input); err == nil {
			t.Fatalf("RepositoryName(%q) error = nil", input)
		}
	}
}
