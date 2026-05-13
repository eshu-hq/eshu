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
)

const defaultHTTPTimeout = 30 * time.Second

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

// ReferrersResponse is the descriptor list returned by the Referrers API.
type ReferrersResponse struct {
	Referrers []ociregistry.Descriptor
}

// NewClient creates a provider-neutral OCI Distribution client.
func NewClient(config ClientConfig) (*Client, error) {
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, fmt.Errorf("oci distribution base URL is required")
	}
	baseURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"))
	if err != nil {
		return nil, fmt.Errorf("parse oci distribution base URL: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("oci distribution base URL must include scheme and host")
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Client{
		baseURL:     baseURL,
		username:    config.Username,
		password:    config.Password,
		bearerToken: config.BearerToken,
		client:      client,
	}, nil
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
	return statusError("ping", resp.StatusCode)
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
		return nil, statusError("list_tags", resp.StatusCode)
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
			"application/vnd.docker.distribution.manifest.v2+json",
			"application/vnd.docker.distribution.manifest.list.v2+json",
		}, ", "),
	})
	if err != nil {
		return ManifestResponse{}, err
	}
	defer closeBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ManifestResponse{}, statusError("get_manifest", resp.StatusCode)
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

// ListReferrers returns descriptors attached to one subject digest.
func (c *Client) ListReferrers(ctx context.Context, repository, digest string) (ReferrersResponse, error) {
	endpoint := "/v2/" + repositoryPath(repository) + "/referrers/" + url.PathEscape(strings.TrimSpace(digest))
	resp, err := c.do(ctx, "list_referrers", http.MethodGet, endpoint, map[string]string{"Accept": ociregistry.MediaTypeOCIImageIndex})
	if err != nil {
		return ReferrersResponse{}, err
	}
	defer closeBody(resp.Body)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return ReferrersResponse{}, statusError("list_referrers", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ReferrersResponse{}, statusError("list_referrers", resp.StatusCode)
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
	if c.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	} else if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, collector.RegistryTransportFailure("oci", "", operation, err)
	}
	return resp, nil
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

func statusError(operation string, statusCode int) error {
	return collector.RegistryHTTPFailure("oci", "", operation, statusCode, nil)
}
