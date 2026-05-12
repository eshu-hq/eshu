package dockerhub

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
)

const (
	// RegistryHost is the canonical Docker Hub host used in image references.
	RegistryHost = "docker.io"
	// DistributionBaseURL is Docker Hub's Distribution API endpoint.
	DistributionBaseURL = "https://registry-1.docker.io"
	tokenRealm          = "https://auth.docker.io/token"
	tokenService        = "registry.docker.io"
)

// Config describes Docker Hub repository access.
type Config struct {
	Repository string
	Username   string
	Password   string
	Client     *http.Client
}

// RepositoryName normalizes Docker Hub repository names for Distribution calls.
func RepositoryName(repository string) (string, error) {
	repository = strings.ToLower(strings.Trim(strings.TrimSpace(repository), "/"))
	if repository == "" {
		return "", fmt.Errorf("dockerhub repository is required")
	}
	if strings.Contains(repository, "//") {
		return "", fmt.Errorf("dockerhub repository must not contain empty path segments")
	}
	if !strings.Contains(repository, "/") {
		repository = "library/" + repository
	}
	return repository, nil
}

// RepositoryIdentity builds the shared OCI identity for a Docker Hub
// repository.
func RepositoryIdentity(repository string) (ociregistry.RepositoryIdentity, error) {
	repository, err := RepositoryName(repository)
	if err != nil {
		return ociregistry.RepositoryIdentity{}, err
	}
	return ociregistry.RepositoryIdentity{
		Provider:   ociregistry.ProviderDockerHub,
		Registry:   RegistryHost,
		Repository: repository,
	}, nil
}

// NewDistributionClient creates a Docker Hub Distribution client with a pull
// token for one repository.
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
