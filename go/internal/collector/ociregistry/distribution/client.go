package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

const defaultHTTPTimeout = 30 * time.Second
const maxBlobReadBytes int64 = 1 << 20

// ClientConfig configures the OCI Distribution HTTP client.
type ClientConfig struct {
	BaseURL     string
	Username    string
	Password    string
	BearerToken string
	Client      *http.Client
}

// Client performs bounded OCI Distribution API calls.
type Client struct {
	baseURL     *url.URL
	username    string
	password    string
	bearerToken string
	client      *http.Client
}

// ManifestResponse is the raw manifest response plus registry-reported
// descriptor metadata.
type ManifestResponse struct {
	Body      []byte
	Digest    string
	MediaType string
	SizeBytes int64
}

// BlobResponse is the raw blob response plus registry-reported media type.
type BlobResponse struct {
	Body      []byte
	Digest    string
	MediaType string
	SizeBytes int64
}

// ReferrersResponse is the descriptor list returned by the Referrers API.
type ReferrersResponse struct {
	Referrers []ociregistry.Descriptor
}

// NewClient creates a provider-neutral OCI Distribution client.
func NewClient(config ClientConfig) (*Client, error) {
	baseURL, err := sdk.ParseBaseURL("oci distribution", config.BaseURL)
	if err != nil {
		return nil, err
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("oci distribution base_url scheme must be http or https")
	}
	client := config.Client
	if client == nil {
		client = sdk.DefaultHTTPClient(defaultHTTPTimeout)
	}
	c := &Client{
		baseURL:     baseURL,
		username:    config.Username,
		password:    config.Password,
		bearerToken: config.BearerToken,
		client:      client,
	}
	installRedirectCredentialPolicy(client, baseURL.Hostname(), c)
	return c, nil
}

// installRedirectCredentialPolicy makes per-hop credential handling explicit and
// deterministic instead of relying on the net/http default redirect heuristic.
//
// Registries such as ECR answer manifest and blob fetches with a redirect to a
// presigned object-store URL on a different host. The original credential must
// follow redirects that stay on the registry host so same-host hops still
// authenticate, and it must never reach a different host because presigned
// stores reject an extra Authorization header and forwarding the credential
// would disclose it. net/http already enforces this in current releases, but it
// is a version-dependent default; owning the policy here keeps the behavior
// locked under the package's regression tests. The policy is installed only when
// the caller did not supply its own CheckRedirect, so an injected client keeps
// control of its redirect handling.
func installRedirectCredentialPolicy(client *http.Client, registryHost string, c *Client) {
	if client.CheckRedirect != nil {
		return
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		// Start each hop with no inherited credential, then re-apply the
		// registry credential only when the hop stays on the registry host.
		// Same-host hops (ECR keeps manifest/blob auth on the registry host)
		// must re-authenticate; cross-host hops (presigned object stores)
		// must never receive the credential.
		req.Header.Del("Authorization")
		if strings.EqualFold(req.URL.Hostname(), registryHost) {
			c.applyAuth(req)
		}
		return nil
	}
}

// Ping validates that the base endpoint speaks the OCI Distribution API or
// returns a Distribution-compatible auth challenge.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.do(ctx, "ping", http.MethodGet, "/v2/", nil)
	if err != nil {
		return err
	}
	defer closeBody(resp.Body)
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized &&
		(resp.Header.Get("Docker-Distribution-Api-Version") != "" || resp.Header.Get("WWW-Authenticate") != "") {
		return nil
	}
	return statusError("ping", resp)
}

// ListTags returns the registry-reported tags for one repository.
func (c *Client) ListTags(ctx context.Context, repository string) ([]string, error) {
	endpoint := "/v2/" + repositoryPath(repository) + "/tags/list"
	resp, err := c.do(ctx, "list_tags", http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, statusError("list_tags", resp)
	}
	var decoded struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode OCI tag list: %w", err)
	}
	return decoded.Tags, nil
}

// GetManifest returns a manifest or image index by tag or digest reference.
func (c *Client) GetManifest(ctx context.Context, repository, reference string) (ManifestResponse, error) {
	endpoint := "/v2/" + repositoryPath(repository) + "/manifests/" + url.PathEscape(strings.TrimSpace(reference))
	resp, err := c.do(ctx, "get_manifest", http.MethodGet, endpoint, map[string]string{
		"Accept": strings.Join([]string{
			ociregistry.MediaTypeOCIImageManifest,
			ociregistry.MediaTypeOCIImageIndex,
			"application/vnd.oci.artifact.manifest.v1+json",
			"application/vnd.cyclonedx+json",
			"application/spdx+json",
			"application/vnd.in-toto+json",
			"application/vnd.docker.distribution.manifest.v2+json",
			"application/vnd.docker.distribution.manifest.list.v2+json",
		}, ", "),
	})
	if err != nil {
		return ManifestResponse{}, err
	}
	defer closeBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ManifestResponse{}, statusError("get_manifest", resp)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ManifestResponse{}, fmt.Errorf("read OCI manifest body: %w", err)
	}
	return ManifestResponse{
		Body:      body,
		Digest:    resp.Header.Get("Docker-Content-Digest"),
		MediaType: resp.Header.Get("Content-Type"),
		SizeBytes: int64(len(body)),
	}, nil
}

// GetBlob returns a content blob by digest reference.
func (c *Client) GetBlob(ctx context.Context, repository, digest string) (BlobResponse, error) {
	endpoint := "/v2/" + repositoryPath(repository) + "/blobs/" + url.PathEscape(strings.TrimSpace(digest))
	resp, err := c.do(ctx, "get_blob", http.MethodGet, endpoint, map[string]string{"Accept": "*/*"})
	if err != nil {
		return BlobResponse{}, err
	}
	defer closeBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return BlobResponse{}, statusError("get_blob", resp)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBlobReadBytes+1))
	if err != nil {
		return BlobResponse{}, fmt.Errorf("read OCI blob body: %w", err)
	}
	return BlobResponse{
		Body:      body,
		Digest:    strings.TrimSpace(digest),
		MediaType: resp.Header.Get("Content-Type"),
		SizeBytes: int64(len(body)),
	}, nil
}

// ListReferrers returns descriptors attached to one subject digest.
func (c *Client) ListReferrers(ctx context.Context, repository, digest string) (ReferrersResponse, error) {
	endpoint := "/v2/" + repositoryPath(repository) + "/referrers/" + url.PathEscape(strings.TrimSpace(digest))
	resp, err := c.do(ctx, "list_referrers", http.MethodGet, endpoint, map[string]string{"Accept": ociregistry.MediaTypeOCIImageIndex})
	if err != nil {
		return ReferrersResponse{}, err
	}
	defer closeBody(resp.Body)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return ReferrersResponse{}, statusError("list_referrers", resp)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ReferrersResponse{}, statusError("list_referrers", resp)
	}
	var decoded struct {
		Manifests []struct {
			MediaType    string            `json:"mediaType"`
			Digest       string            `json:"digest"`
			Size         int64             `json:"size"`
			ArtifactType string            `json:"artifactType"`
			Annotations  map[string]string `json:"annotations"`
		} `json:"manifests"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return ReferrersResponse{}, fmt.Errorf("decode OCI referrers: %w", err)
	}
	referrers := make([]ociregistry.Descriptor, 0, len(decoded.Manifests))
	for _, manifest := range decoded.Manifests {
		referrers = append(referrers, ociregistry.Descriptor{
			Digest:       manifest.Digest,
			MediaType:    manifest.MediaType,
			SizeBytes:    manifest.Size,
			ArtifactType: manifest.ArtifactType,
			Annotations:  manifest.Annotations,
		})
	}
	return ReferrersResponse{Referrers: referrers}, nil
}

func (c *Client) do(
	ctx context.Context,
	operation string,
	method string,
	endpoint string,
	headers map[string]string,
) (*http.Response, error) {
	requestURL := c.resolve(endpoint)
	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build OCI request: %w", err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	c.applyAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, collector.RegistryTransportFailure("oci", "", operation, sdk.HTTPError{
			Provider: "oci",
			Message:  "request failed",
			Cause:    err,
		})
	}
	return resp, nil
}

// applyAuth sets the configured credential on a request. A bearer token wins
// over basic credentials. It is called for the initial request and re-applied on
// same-host redirects so multi-hop registry fetches keep authenticating.
func (c *Client) applyAuth(req *http.Request) {
	if c.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
		return
	}
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
}

func (c *Client) resolve(endpoint string) url.URL {
	resolved := *c.baseURL
	basePath := strings.TrimRight(resolved.Path, "/")
	resolved.Path = path.Join(basePath, endpoint)
	if strings.HasSuffix(endpoint, "/") && !strings.HasSuffix(resolved.Path, "/") {
		resolved.Path += "/"
	}
	return resolved
}

func repositoryPath(repository string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(repository), "/"), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func closeBody(body io.Closer) {
	_ = body.Close()
}

func statusError(operation string, response *http.Response) error {
	return collector.RegistryHTTPFailure("oci", "", operation, response.StatusCode, sdk.HTTPError{
		Provider:   "oci",
		StatusCode: response.StatusCode,
		Message:    http.StatusText(response.StatusCode),
		RetryAfter: sdk.ParseRetryAfterHeader(response.Header.Get("Retry-After")),
	})
}
