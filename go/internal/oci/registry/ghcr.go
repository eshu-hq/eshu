// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"fmt"
	"strings"
)

const (
	// GHCRRegistryHost is the canonical GitHub Container Registry host.
	GHCRRegistryHost = "ghcr.io"
)

// GHCRRepositoryName normalizes GHCR owner/image repository names.
func GHCRRepositoryName(repository string) (string, error) {
	repository = strings.ToLower(strings.Trim(strings.TrimSpace(repository), "/"))
	if repository == "" {
		return "", fmt.Errorf("ghcr repository is required")
	}
	if strings.Contains(repository, "//") {
		return "", fmt.Errorf("ghcr repository must not contain empty path segments")
	}
	if !strings.Contains(repository, "/") {
		return "", fmt.Errorf("ghcr repository must include owner and image name")
	}
	return repository, nil
}

// GHCRRepositoryIdentity builds the shared OCI identity for a GHCR repository.
func GHCRRepositoryIdentity(repository string) (RepositoryIdentity, error) {
	repository, err := GHCRRepositoryName(repository)
	if err != nil {
		return RepositoryIdentity{}, err
	}
	return RepositoryIdentity{
		Provider:   ProviderGHCR,
		Registry:   GHCRRegistryHost,
		Repository: repository,
	}, nil
}
