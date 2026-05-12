package ghcr

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
)

const (
	// RegistryHost is the canonical GitHub Container Registry host.
	RegistryHost = "ghcr.io"
	// DistributionBaseURL is GHCR's Distribution API endpoint.
	DistributionBaseURL = "https://ghcr.io"
	tokenRealm          = "https://ghcr.io/token"
	tokenService        = "ghcr.io"
)

// Config describes GHCR repository access.
type Config struct {
	Repository string
	Username   string
	Password   string
	Client     *http.Client
}

// RepositoryName normalizes GHCR owner/image repository names.
func RepositoryName(repository string) (string, error) {
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

// RepositoryIdentity builds the shared OCI identity for a GHCR repository.
func RepositoryIdentity(repository string) (ociregistry.RepositoryIdentity, error) {
	repository, err := RepositoryName(repository)
	if err != nil {
		return ociregistry.RepositoryIdentity{}, err
	}
	return ociregistry.RepositoryIdentity{
		Provider:   ociregistry.ProviderGHCR,
		Registry:   RegistryHost,
		Repository: repository,
	}, nil
}

// NewDistributionClient creates a GHCR Distribution client with a pull token
// for one repository.
func NewDistributionClient(ctx context.Context, config Config) (*distribution.Client, error) {
	repository, err := RepositoryName(config.Repository)
	if err != nil {
		return nil, err
	}
	token, err := distribution.FetchBearerToken(ctx, distribution.TokenConfig{
		Realm:    tokenRealm,
		Service:  tokenService,
		Scope:    "repository:" + repository + ":pull",
		Username: config.Username,
		Password: config.Password,
		Client:   config.Client,
	})
	if err != nil {
		return nil, err
	}
	return distribution.NewClient(distribution.ClientConfig{
		BaseURL:     DistributionBaseURL,
		BearerToken: token,
		Client:      config.Client,
	})
}
