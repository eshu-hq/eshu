// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"fmt"
	"net/url"
	"strings"
)

// harborDistributionBaseURL returns Harbor's HTTPS Distribution API base URL.
func harborDistributionBaseURL(baseURL string) (string, error) {
	parsed, err := parseHTTPSRegistryURL("harbor", baseURL)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

// harborRepositoryName normalizes Harbor project/repository paths.
func harborRepositoryName(repository string) (string, error) {
	repository = strings.ToLower(strings.Trim(strings.TrimSpace(repository), "/"))
	if repository == "" {
		return "", fmt.Errorf("harbor repository is required")
	}
	if strings.Contains(repository, "//") || !strings.Contains(repository, "/") {
		return "", fmt.Errorf("harbor repository must include project and image path")
	}
	return repository, nil
}

// HarborRepositoryIdentity builds the shared OCI repository identity for Harbor.
func HarborRepositoryIdentity(baseURL, repository string) (RepositoryIdentity, error) {
	distributionBase, err := harborDistributionBaseURL(baseURL)
	if err != nil {
		return RepositoryIdentity{}, err
	}
	repository, err = harborRepositoryName(repository)
	if err != nil {
		return RepositoryIdentity{}, err
	}
	return RepositoryIdentity{
		Provider:   ProviderHarbor,
		Registry:   distributionBase,
		Repository: repository,
	}, nil
}

// parseHTTPSRegistryURL parses a provider registry URL that must use HTTPS,
// carry a host, and carry no credentials.
func parseHTTPSRegistryURL(provider, raw string) (*url.URL, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return nil, fmt.Errorf("%s registry URL is required", provider)
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse %s registry URL: %w", provider, err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("%s registry URL must use https and include a host", provider)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("%s registry URL must not include credentials", provider)
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}
