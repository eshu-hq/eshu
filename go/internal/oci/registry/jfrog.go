// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// jfrogDistributionBaseURL returns the Artifactory Docker API base URL for a
// repository key.
func jfrogDistributionBaseURL(baseURL, repositoryKey string) (string, error) {
	trimmedBase := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmedBase == "" {
		return "", fmt.Errorf("jfrog base URL is required")
	}
	key := strings.Trim(strings.TrimSpace(repositoryKey), "/")
	if key == "" {
		return "", fmt.Errorf("jfrog docker repository key is required")
	}
	parsed, err := url.Parse(trimmedBase)
	if err != nil {
		return "", fmt.Errorf("parse jfrog base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("jfrog base URL must include scheme and host")
	}
	parsed.Path = path.Join(parsed.Path, "artifactory", "api", "docker", key)
	return parsed.String(), nil
}

// JFrogRepositoryIdentity builds the shared OCI repository identity for one
// image repository served by Artifactory.
func JFrogRepositoryIdentity(baseURL, repositoryKey, imageRepository string) (RepositoryIdentity, error) {
	distributionBase, err := jfrogDistributionBaseURL(baseURL, repositoryKey)
	if err != nil {
		return RepositoryIdentity{}, err
	}
	return RepositoryIdentity{
		Provider:   ProviderJFrog,
		Registry:   distributionBase,
		Repository: imageRepository,
	}, nil
}
