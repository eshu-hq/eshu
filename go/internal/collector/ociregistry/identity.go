package ociregistry

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

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

// NormalizeDescriptorIdentity applies digest and media-type validation for a
// descriptor observed in an OCI repository.
func NormalizeDescriptorIdentity(identity DescriptorIdentity) (NormalizedDescriptorIdentity, error) {
	repository, err := NormalizeRepositoryIdentity(identity.Repository)
	if err != nil {
		return NormalizedDescriptorIdentity{}, err
	}
	digest, err := normalizeDigest(identity.Digest)
	if err != nil {
		return NormalizedDescriptorIdentity{}, err
	}
	mediaType := strings.TrimSpace(identity.MediaType)
	if !validMediaType(mediaType) {
		return NormalizedDescriptorIdentity{}, fmt.Errorf("oci descriptor media type %q is invalid", identity.MediaType)
	}
	return NormalizedDescriptorIdentity{
		Repository:   repository,
		Digest:       digest,
		MediaType:    mediaType,
		DescriptorID: fmt.Sprintf("oci-descriptor://%s/%s@%s", repository.Registry, repository.Repository, digest),
	}, nil
}

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
	return strings.Trim(trimmed, "/")
}

func normalizeDigest(raw string) (string, error) {
	digest := strings.ToLower(strings.TrimSpace(raw))
	if !digestPattern.MatchString(digest) {
		return "", fmt.Errorf("oci descriptor digest %q is invalid", raw)
	}
	return digest, nil
}

func validMediaType(raw string) bool {
	mediaType := strings.TrimSpace(raw)
	if mediaType == "" || strings.Contains(mediaType, " ") {
		return false
	}
	kind, subtype, ok := strings.Cut(mediaType, "/")
	return ok && kind != "" && subtype != ""
}
