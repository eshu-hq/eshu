package jfrog

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
)

// Config describes one JFrog Artifactory Docker/OCI repository access boundary.
type Config struct {
	BaseURL       string
	RepositoryKey string
	Username      string
	Password      string
	BearerToken   string
	Client        *http.Client
}

// DistributionBaseURL returns the Artifactory Docker API base URL for a
// repository key.
func DistributionBaseURL(baseURL, repositoryKey string) (string, error) {
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

// NewDistributionClient creates an OCI Distribution client for one Artifactory
// Docker/OCI repository key.
func NewDistributionClient(config Config) (*distribution.Client, error) {
	baseURL, err := DistributionBaseURL(config.BaseURL, config.RepositoryKey)
	if err != nil {
		return nil, err
	}
	return distribution.NewClient(distribution.ClientConfig{
		BaseURL:     baseURL,
		Username:    config.Username,
		Password:    config.Password,
		BearerToken: config.BearerToken,
		Client:      config.Client,
	})
}

// RepositoryIdentity builds the shared OCI repository identity for one image
// repository served by Artifactory.
func RepositoryIdentity(baseURL, repositoryKey, imageRepository string) (ociregistry.RepositoryIdentity, error) {
	distributionBase, err := DistributionBaseURL(baseURL, repositoryKey)
	if err != nil {
		return ociregistry.RepositoryIdentity{}, err
	}
	return ociregistry.RepositoryIdentity{
		Provider:   ociregistry.ProviderJFrog,
		Registry:   distributionBase,
		Repository: imageRepository,
	}, nil
}
