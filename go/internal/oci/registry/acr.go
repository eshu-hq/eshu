// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"fmt"
	"net/url"
	"strings"
)

// acrDistributionBaseURL returns the HTTPS Distribution API base URL for ACR.
func acrDistributionBaseURL(registryHost string) (string, error) {
	parsed, err := parseACRHost(registryHost)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

// acrRepositoryName normalizes ACR repository paths.
func acrRepositoryName(repository string) (string, error) {
	repository = strings.ToLower(strings.Trim(strings.TrimSpace(repository), "/"))
	if repository == "" {
		return "", fmt.Errorf("azure container registry repository is required")
	}
	if strings.Contains(repository, "//") {
		return "", fmt.Errorf("azure container registry repository must not contain empty path segments")
	}
	return repository, nil
}

// ACRRepositoryIdentity builds the shared OCI repository identity for ACR.
func ACRRepositoryIdentity(registryHost, repository string) (RepositoryIdentity, error) {
	distributionBase, err := acrDistributionBaseURL(registryHost)
	if err != nil {
		return RepositoryIdentity{}, err
	}
	repository, err = acrRepositoryName(repository)
	if err != nil {
		return RepositoryIdentity{}, err
	}
	return RepositoryIdentity{
		Provider:   ProviderAzureContainerRegistry,
		Registry:   distributionBase,
		Repository: repository,
	}, nil
}

// parseACRHost parses an Azure Container Registry host, which must end with
// .azurecr.io and carry no credentials.
func parseACRHost(raw string) (*url.URL, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return nil, fmt.Errorf("azure container registry host is required")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse azure container registry host: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("azure container registry host must use https and include a host")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("azure container registry host must not include credentials")
	}
	if !strings.HasSuffix(parsed.Hostname(), ".azurecr.io") {
		return nil, fmt.Errorf("azure container registry host must end with .azurecr.io")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}
