package dockerhub

import "testing"

func TestRepositoryNameAddsLibraryNamespace(t *testing.T) {
	t.Parallel()

	got, err := RepositoryName("busybox")
	if err != nil {
		t.Fatalf("RepositoryName() error = %v", err)
	}
	if got != "library/busybox" {
		t.Fatalf("RepositoryName() = %q, want library/busybox", got)
	}
}

func TestRepositoryNamePreservesExplicitNamespace(t *testing.T) {
	t.Parallel()

	got, err := RepositoryName("team/api")
	if err != nil {
		t.Fatalf("RepositoryName() error = %v", err)
	}
	if got != "team/api" {
		t.Fatalf("RepositoryName() = %q, want team/api", got)
	}
}

func TestRepositoryIdentityUsesDockerHubProvider(t *testing.T) {
	t.Parallel()

	identity, err := RepositoryIdentity("busybox")
	if err != nil {
		t.Fatalf("RepositoryIdentity() error = %v", err)
	}
	if identity.Provider != "dockerhub" {
		t.Fatalf("Provider = %q", identity.Provider)
	}
	if identity.Registry != RegistryHost {
		t.Fatalf("Registry = %q, want %q", identity.Registry, RegistryHost)
	}
	if identity.Repository != "library/busybox" {
		t.Fatalf("Repository = %q, want library/busybox", identity.Repository)
	}
}

func TestRepositoryNameRejectsInvalidNames(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "/", "team//api"} {
		if _, err := RepositoryName(input); err == nil {
			t.Fatalf("RepositoryName(%q) error = nil", input)
		}
	}
}
