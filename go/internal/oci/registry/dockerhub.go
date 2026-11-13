// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"fmt"
	"strings"
)

const (
	// DockerHubRegistryHost is the canonical Docker Hub host used in image references.
	DockerHubRegistryHost = "docker.io"
)

// DockerHubRepositoryName normalizes Docker Hub repository names for
// Distribution calls.
func DockerHubRepositoryName(repository string) (string, error) {
	repository = strings.ToLower(strings.Trim(strings.TrimSpace(repository), "/"))
	if repository == "" {
		return "", fmt.Errorf("dockerhub repository is required")
	}
	if strings.Contains(repository, "//") {
		return "", fmt.Errorf("dockerhub repository must not contain empty path segments")
	}
	if !strings.Contains(repository, "/") {
		repository = "library/" + repository
	}
	return repository, nil
}

// DockerHubRepositoryIdentity builds the shared OCI identity for a Docker Hub
// repository.
func DockerHubRepositoryIdentity(repository string) (RepositoryIdentity, error) {
	repository, err := DockerHubRepositoryName(repository)
	if err != nil {
		return RepositoryIdentity{}, err
	}
	return RepositoryIdentity{
		Provider:   ProviderDockerHub,
		Registry:   DockerHubRegistryHost,
		Repository: repository,
	}, nil
}
