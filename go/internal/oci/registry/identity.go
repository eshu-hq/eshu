// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"fmt"
	"net/url"
	"strings"
)

// Provider identifies the registry provider adapter that reported evidence.
type Provider string

const (
	// ProviderDockerHub identifies Docker Hub registry repositories.
	ProviderDockerHub Provider = "dockerhub"
	// ProviderECR identifies Amazon Elastic Container Registry.
	ProviderECR Provider = "ecr"
	// ProviderGHCR identifies GitHub Container Registry repositories.
	ProviderGHCR Provider = "ghcr"
	// ProviderJFrog identifies JFrog Artifactory Docker/OCI repositories.
	ProviderJFrog Provider = "jfrog"
	// ProviderHarbor identifies Harbor OCI registry repositories.
	ProviderHarbor Provider = "harbor"
	// ProviderGoogleArtifactRegistry identifies Google Artifact Registry Docker repositories.
	ProviderGoogleArtifactRegistry Provider = "google_artifact_registry"
	// ProviderAzureContainerRegistry identifies Azure Container Registry repositories.
	ProviderAzureContainerRegistry Provider = "azure_container_registry"
)

// RepositoryIdentity is the raw registry/repository tuple observed from a
// provider.
type RepositoryIdentity struct {
	Provider   Provider
	Registry   string
	Repository string
}

// NormalizedRepositoryIdentity is the stable repository identity used in OCI
// registry fact keys and scope IDs.
type NormalizedRepositoryIdentity struct {
	Provider     Provider
	Registry     string
	Repository   string
	RepositoryID string
	ScopeID      string
}

// NormalizeRepositoryIdentity applies provider-neutral OCI repository identity
// rules before facts are assigned stable keys.
func NormalizeRepositoryIdentity(identity RepositoryIdentity) (NormalizedRepositoryIdentity, error) {
	provider := Provider(strings.TrimSpace(string(identity.Provider)))
	if provider == "" {
		return NormalizedRepositoryIdentity{}, fmt.Errorf("oci repository provider must not be blank")
	}
	registry := normalizeRegistry(identity.Registry)
	if registry == "" {
		return NormalizedRepositoryIdentity{}, fmt.Errorf("oci repository registry must not be blank")
	}
	repository := strings.ToLower(strings.Trim(strings.TrimSpace(identity.Repository), "/"))
	if repository == "" {
		return NormalizedRepositoryIdentity{}, fmt.Errorf("oci repository name must not be blank")
	}
	repositoryID := fmt.Sprintf("oci-registry://%s/%s", registry, repository)
	return NormalizedRepositoryIdentity{
		Provider:     provider,
		Registry:     registry,
		Repository:   repository,
		RepositoryID: repositoryID,
		ScopeID:      repositoryID,
	}, nil
}

// normalizeRegistry trims a registry host or URL to its canonical host form.
func normalizeRegistry(raw string) string {
	trimmed := strings.Trim(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err == nil && parsed.Host != "" {
		host := strings.ToLower(parsed.Host)
		path := strings.Trim(parsed.EscapedPath(), "/")
		if path == "" {
			return host
		}
		return host + "/" + path
	}
	host, path, ok := strings.Cut(trimmed, "/")
	if !ok {
		return strings.ToLower(host)
	}
	return strings.ToLower(host) + "/" + strings.Trim(path, "/")
}
