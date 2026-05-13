package harbor

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
)

// Config describes one Harbor repository access boundary.
type Config struct {
	BaseURL     string
	Repository  string
	Username    string
	Password    string
	BearerToken string
	Client      *http.Client
}

// DistributionBaseURL returns Harbor's HTTPS Distribution API base URL.
func DistributionBaseURL(baseURL string) (string, error) {
	parsed, err := parseHTTPSRegistryURL("harbor", baseURL)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

// RepositoryName normalizes Harbor project/repository paths.
func RepositoryName(repository string) (string, error) {
	repository = strings.ToLower(strings.Trim(strings.TrimSpace(repository), "/"))
	if repository == "" {
		return "", fmt.Errorf("harbor repository is required")
	}
	if strings.Contains(repository, "//") || !strings.Contains(repository, "/") {
		return "", fmt.Errorf("harbor repository must include project and image path")
	}
	return repository, nil
}

// RepositoryIdentity builds the shared OCI repository identity for Harbor.
func RepositoryIdentity(baseURL, repository string) (ociregistry.RepositoryIdentity, error) {
	distributionBase, err := DistributionBaseURL(baseURL)
	if err != nil {
		return ociregistry.RepositoryIdentity{}, err
	}
	repository, err = RepositoryName(repository)
	if err != nil {
		return ociregistry.RepositoryIdentity{}, err
	}
	return ociregistry.RepositoryIdentity{
		Provider:   ociregistry.ProviderHarbor,
		Registry:   distributionBase,
		Repository: repository,
	}, nil
}

// NewDistributionClient creates an OCI Distribution client for Harbor.
func NewDistributionClient(config Config) (*distribution.Client, error) {
	baseURL, err := DistributionBaseURL(config.BaseURL)
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
