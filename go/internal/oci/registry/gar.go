// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"fmt"
	"net/url"
	"strings"
)

// garDistributionBaseURL returns the HTTPS Distribution API base URL for GAR.
func garDistributionBaseURL(registryHost string) (string, error) {
	parsed, err := parseGARHost(registryHost)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

// garRepositoryName normalizes GAR project/repository/image paths.
func garRepositoryName(repository string) (string, error) {
	repository = strings.ToLower(strings.Trim(strings.TrimSpace(repository), "/"))
	if repository == "" {
		return "", fmt.Errorf("google artifact registry repository is required")
	}
	if strings.Contains(repository, "//") || strings.Count(repository, "/") < 2 {
		return "", fmt.Errorf("google artifact registry repository must include project, repository, and image path")
	}
	return repository, nil
}

// GARRepositoryIdentity builds the shared OCI repository identity for GAR.
func GARRepositoryIdentity(registryHost, repository string) (RepositoryIdentity, error) {
	distributionBase, err := garDistributionBaseURL(registryHost)
	if err != nil {
		return RepositoryIdentity{}, err
	}
	repository, err = garRepositoryName(repository)
	if err != nil {
		return RepositoryIdentity{}, err
	}
	return RepositoryIdentity{
		Provider:   ProviderGoogleArtifactRegistry,
		Registry:   distributionBase,
		Repository: repository,
	}, nil
}

// parseGARHost parses a Google Artifact Registry host, which must end with
// -docker.pkg.dev and carry no credentials.
func parseGARHost(raw string) (*url.URL, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return nil, fmt.Errorf("google artifact registry host is required")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse google artifact registry host: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("google artifact registry host must use https and include a host")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("google artifact registry host must not include credentials")
	}
	if !strings.HasSuffix(parsed.Hostname(), "-docker.pkg.dev") {
		return nil, fmt.Errorf("google artifact registry host must end with -docker.pkg.dev")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}
