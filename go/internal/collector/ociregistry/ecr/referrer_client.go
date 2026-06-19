package ecr

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
)

// ReferrerClientOptions configures an ECR-authenticated OCI Distribution client
// for referrer document fetches.
//
// The options keep AWS credential-chain wiring in the caller: the caller loads
// AWS configuration and supplies an AuthorizationTokenAPI. This package owns
// only the GetAuthorizationToken-to-Distribution-credentials conversion and the
// registry host resolution.
type ReferrerClientOptions struct {
	// AuthorizationClient mints ECR Distribution credentials through the AWS
	// GetAuthorizationToken exchange. It is required.
	AuthorizationClient AuthorizationTokenAPI
	// RegistryHost is the ECR registry host or HTTPS base URL. When blank the
	// proxy endpoint returned by the token exchange seeds the host.
	RegistryHost string
	// HTTPClient is the bounded HTTP client used for Distribution calls. A nil
	// client uses the Distribution package default.
	HTTPClient *http.Client
}

// NewReferrerClient builds an OCI Distribution client whose basic-auth
// credentials come from a fresh ECR GetAuthorizationToken exchange.
//
// Minting the token at client-build time, rather than relying on static
// credentials, is the supported ECR auth path: ECR authorization tokens are
// short lived, so the AWS default credential chain must produce a current token
// for each collection. The decoded token is used only as request credentials
// and is never logged or returned in errors.
func NewReferrerClient(ctx context.Context, options ReferrerClientOptions) (*distribution.Client, error) {
	if options.AuthorizationClient == nil {
		return nil, fmt.Errorf("ecr authorization client is required")
	}
	credentials, err := GetDistributionCredentials(ctx, options.AuthorizationClient)
	if err != nil {
		return nil, err
	}
	registryHost := strings.TrimSpace(options.RegistryHost)
	if registryHost == "" {
		registryHost = credentials.ProxyEndpoint
	}
	if registryHost == "" {
		return nil, fmt.Errorf("ecr registry host is required")
	}
	baseURL, err := referrerBaseURL(registryHost)
	if err != nil {
		return nil, err
	}
	return distribution.NewClient(distribution.ClientConfig{
		BaseURL:  baseURL,
		Username: credentials.Username,
		Password: credentials.Password,
		Client:   options.HTTPClient,
	})
}

// referrerBaseURL resolves an ECR registry host or base URL into a Distribution
// base URL. A bare host (the production ECR shape) is promoted to HTTPS through
// the shared DistributionBaseURL helper. An already-schemed URL is passed
// through so a configured HTTPS base URL keeps its scheme; the Distribution
// client still rejects any scheme other than http or https.
func referrerBaseURL(registryHost string) (string, error) {
	if strings.Contains(registryHost, "://") {
		return strings.TrimRight(strings.TrimSpace(registryHost), "/"), nil
	}
	return DistributionBaseURL(registryHost)
}
