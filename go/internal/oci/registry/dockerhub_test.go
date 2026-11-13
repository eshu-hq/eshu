// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import "testing"

func TestDockerHubRepositoryNameAddsLibraryNamespace(t *testing.T) {
	t.Parallel()

	got, err := DockerHubRepositoryName("busybox")
	if err != nil {
		t.Fatalf("DockerHubRepositoryName() error = %v", err)
	}
	if got != "library/busybox" {
		t.Fatalf("DockerHubRepositoryName() = %q, want library/busybox", got)
	}
}

func TestDockerHubRepositoryNamePreservesExplicitNamespace(t *testing.T) {
	t.Parallel()

	got, err := DockerHubRepositoryName("team/api")
	if err != nil {
		t.Fatalf("DockerHubRepositoryName() error = %v", err)
	}
	if got != "team/api" {
		t.Fatalf("DockerHubRepositoryName() = %q, want team/api", got)
	}
}

func TestDockerHubRepositoryIdentityUsesDockerHubProvider(t *testing.T) {
	t.Parallel()

	identity, err := DockerHubRepositoryIdentity("busybox")
	if err != nil {
		t.Fatalf("DockerHubRepositoryIdentity() error = %v", err)
	}
	if identity.Provider != "dockerhub" {
		t.Fatalf("Provider = %q", identity.Provider)
	}
	if identity.Registry != DockerHubRegistryHost {
		t.Fatalf("Registry = %q, want %q", identity.Registry, DockerHubRegistryHost)
	}
	if identity.Repository != "library/busybox" {
		t.Fatalf("Repository = %q, want library/busybox", identity.Repository)
	}
}

func TestDockerHubRepositoryNameRejectsInvalidNames(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "/", "team//api"} {
		if _, err := DockerHubRepositoryName(input); err == nil {
			t.Fatalf("DockerHubRepositoryName(%q) error = nil", input)
		}
	}
}
