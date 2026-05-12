package ecr

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
)

// PublicRegistryHost is the hostname for Amazon ECR Public.
const PublicRegistryHost = "public.ecr.aws"

// PrivateRegistryHost returns the AWS account and region scoped ECR registry
// host.
func PrivateRegistryHost(accountID, region string) (string, error) {
	accountID = strings.TrimSpace(accountID)
	region = strings.TrimSpace(region)
	if accountID == "" {
		return "", fmt.Errorf("ecr account id is required")
	}
	if region == "" {
		return "", fmt.Errorf("ecr region is required")
	}
	return accountID + ".dkr.ecr." + region + ".amazonaws.com", nil
}

// RepositoryIdentity builds the shared OCI identity for an ECR repository.
func RepositoryIdentity(registryHost, repository string) ociregistry.RepositoryIdentity {
	return ociregistry.RepositoryIdentity{
		Provider:   ociregistry.ProviderECR,
		Registry:   registryHost,
		Repository: repository,
	}
}

// DistributionBaseURL returns the HTTPS Distribution API base URL for an ECR
// registry host.
func DistributionBaseURL(registryHost string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(registryHost), "/")
	if trimmed == "" {
		return "", fmt.Errorf("ecr registry host is required")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse ecr registry host: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("ecr registry host must use https and include a host")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("ecr registry host must not include credentials")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

// NewDistributionClient creates an OCI Distribution client for an ECR registry.
func NewDistributionClient(registryHost, username, password string, client *http.Client) (*distribution.Client, error) {
	baseURL, err := DistributionBaseURL(registryHost)
	if err != nil {
		return nil, err
	}
	return distribution.NewClient(distribution.ClientConfig{
		BaseURL:  baseURL,
		Username: username,
		Password: password,
		Client:   client,
	})
}

// BasicAuthFromAuthorizationToken decodes the ECR authorization token returned
// by GetAuthorizationToken into Distribution basic-auth credentials.
func BasicAuthFromAuthorizationToken(token string) (string, string, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return "", "", fmt.Errorf("decode ecr authorization token: %w", err)
	}
	username, password, ok := strings.Cut(string(decoded), ":")
	if !ok || username == "" || password == "" {
		return "", "", fmt.Errorf("ecr authorization token must decode to username:password")
	}
	return username, password, nil
}
