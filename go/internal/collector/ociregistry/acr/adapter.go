package acr

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
)

// Config describes one Azure Container Registry repository access boundary.
type Config struct {
	RegistryHost string
	Repository   string
	Username     string
	Password     string
	BearerToken  string
	Client       *http.Client
}

// DistributionBaseURL returns the HTTPS Distribution API base URL for ACR.
func DistributionBaseURL(registryHost string) (string, error) {
	parsed, err := parseACRHost(registryHost)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

// RepositoryName normalizes ACR repository paths.
func RepositoryName(repository string) (string, error) {
	repository = strings.ToLower(strings.Trim(strings.TrimSpace(repository), "/"))
	if repository == "" {
		return "", fmt.Errorf("azure container registry repository is required")
	}
	if strings.Contains(repository, "//") {
		return "", fmt.Errorf("azure container registry repository must not contain empty path segments")
	}
	return repository, nil
}

// RepositoryIdentity builds the shared OCI repository identity for ACR.
func RepositoryIdentity(registryHost, repository string) (ociregistry.RepositoryIdentity, error) {
	distributionBase, err := DistributionBaseURL(registryHost)
	if err != nil {
		return ociregistry.RepositoryIdentity{}, err
	}
	repository, err = RepositoryName(repository)
	if err != nil {
		return ociregistry.RepositoryIdentity{}, err
	}
	return ociregistry.RepositoryIdentity{
		Provider:   ociregistry.ProviderAzureContainerRegistry,
		Registry:   distributionBase,
		Repository: repository,
	}, nil
}

// NewDistributionClient creates an OCI Distribution client for ACR.
func NewDistributionClient(config Config) (*distribution.Client, error) {
	baseURL, err := DistributionBaseURL(config.RegistryHost)
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
