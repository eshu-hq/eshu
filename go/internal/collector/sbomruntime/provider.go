package sbomruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

const defaultHTTPTimeout = 30 * time.Second

// ReferrerClient is the bounded OCI Distribution surface the provider needs to
// fetch an OCI referrer document. *distribution.Client satisfies it.
type ReferrerClient interface {
	// GetManifest returns a manifest or image index by tag or digest reference.
	GetManifest(ctx context.Context, repository, reference string) (distribution.ManifestResponse, error)
	// GetBlob returns a content blob by digest reference.
	GetBlob(ctx context.Context, repository, digest string) (distribution.BlobResponse, error)
}

// ReferrerClientFactory builds a provider-authenticated ReferrerClient for one
// oci_referrer target. A nil client return means the factory does not handle the
// target's provider, so the HTTPProvider falls back to its static-credential
// client. This is the seam that lets provider=ecr targets mint short-lived
// credentials from the AWS GetAuthorizationToken exchange.
type ReferrerClientFactory interface {
	// ReferrerClient returns an authenticated client for the target, or a nil
	// client when the factory does not serve the target's provider.
	ReferrerClient(ctx context.Context, target TargetConfig) (ReferrerClient, error)
}

// HTTPProvider fetches configured URLs and OCI referrer document payloads.
type HTTPProvider struct {
	HTTPClient *http.Client
	Now        func() time.Time
	// ClientFactory supplies provider-authenticated OCI Distribution clients for
	// oci_referrer targets (for example the ECR GetAuthorizationToken exchange).
	// A nil factory keeps the static-credential path for every target.
	ClientFactory ReferrerClientFactory
}

// FetchDocument fetches one configured source or OCI referrer document.
func (p HTTPProvider) FetchDocument(ctx context.Context, target TargetConfig) (Document, error) {
	switch target.SourceType {
	case SourceTypeConfigured:
		return p.fetchURL(ctx, target, target.DocumentURL)
	case SourceTypeOCIReferrer:
		if target.DocumentURL != "" {
			return p.fetchURL(ctx, target, target.DocumentURL)
		}
		return p.fetchOCIReferrer(ctx, target)
	default:
		return Document{}, fmt.Errorf("unsupported source_type %q", target.SourceType)
	}
}

func (p HTTPProvider) fetchURL(ctx context.Context, target TargetConfig, rawURL string) (Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(rawURL), nil)
	if err != nil {
		return Document{}, fmt.Errorf("build SBOM document request: %w", err)
	}
	applyAuth(req, target)
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return Document{}, collector.RegistryTransportFailure(
			"sbom_attestation",
			"",
			"fetch_document",
			sdk.HTTPError{
				Provider: "sbom_attestation",
				Message:  "request failed",
				Cause:    err,
			},
		)
	}
	defer closeBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Document{}, collector.RegistryHTTPFailure(
			"sbom_attestation",
			"",
			"fetch_document",
			resp.StatusCode,
			sdk.HTTPError{
				Provider:   "sbom_attestation",
				StatusCode: resp.StatusCode,
				Message:    http.StatusText(resp.StatusCode),
				RetryAfter: sdk.ParseRetryAfterHeader(resp.Header.Get("Retry-After")),
			},
		)
	}
	body, err := readBounded(resp.Body, target.MaxBytes)
	if err != nil {
		return Document{}, err
	}
	return Document{
		Body:           body,
		SourceURI:      rawURL,
		SourceRecordID: firstNonBlank(target.SourceRecordID, target.ReferrerDigest, rawURL),
		MediaType:      resp.Header.Get("Content-Type"),
		ObservedAt:     p.now().UTC(),
	}, nil
}

func (p HTTPProvider) fetchOCIReferrer(ctx context.Context, target TargetConfig) (Document, error) {
	client, err := p.referrerClient(ctx, target)
	if err != nil {
		return Document{}, err
	}
	manifest, err := client.GetManifest(ctx, target.Repository, target.ReferrerDigest)
	if err != nil {
		return Document{}, err
	}
	if looksLikeSourceDocument(manifest.Body) {
		return Document{
			Body:           manifest.Body,
			SourceURI:      ociSourceURI(target, target.ReferrerDigest),
			SourceRecordID: target.ReferrerDigest,
			MediaType:      manifest.MediaType,
			ObservedAt:     p.now().UTC(),
		}, nil
	}
	blobDigest, err := selectArtifactBlobDigest(manifest.Body, target.DocumentFormat)
	if err != nil {
		return Document{}, err
	}
	blob, err := client.GetBlob(ctx, target.Repository, blobDigest)
	if err != nil {
		return Document{}, err
	}
	body, err := readBounded(bytes.NewReader(blob.Body), target.MaxBytes)
	if err != nil {
		return Document{}, err
	}
	return Document{
		Body:           body,
		SourceURI:      ociSourceURI(target, blobDigest),
		SourceRecordID: firstNonBlank(target.SourceRecordID, target.ReferrerDigest),
		MediaType:      blob.MediaType,
		ObservedAt:     p.now().UTC(),
	}, nil
}

// referrerClient resolves the OCI Distribution client for an oci_referrer
// target. When a ClientFactory serves the target's provider (for example
// provider=ecr) its authenticated client is used; otherwise the provider builds
// a static-credential client from the target's configured credentials.
func (p HTTPProvider) referrerClient(ctx context.Context, target TargetConfig) (ReferrerClient, error) {
	if p.ClientFactory != nil {
		client, err := p.ClientFactory.ReferrerClient(ctx, target)
		if err != nil {
			return nil, err
		}
		if client != nil {
			return client, nil
		}
	}
	return distribution.NewClient(distribution.ClientConfig{
		BaseURL:     target.Registry,
		Username:    target.Username,
		Password:    target.Password,
		BearerToken: target.BearerToken,
		Client:      p.HTTPClient,
	})
}

type ociArtifactManifest struct {
	Blobs  []ociArtifactDescriptor `json:"blobs"`
	Layers []ociArtifactDescriptor `json:"layers"`
}

type ociArtifactDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
}

func selectArtifactBlobDigest(raw []byte, format DocumentFormat) (string, error) {
	var manifest ociArtifactManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return "", fmt.Errorf("decode OCI referrer artifact manifest: %w", err)
	}
	descriptors := append([]ociArtifactDescriptor{}, manifest.Blobs...)
	descriptors = append(descriptors, manifest.Layers...)
	for _, descriptor := range descriptors {
		if descriptor.Digest == "" {
			continue
		}
		if mediaTypeMatchesFormat(descriptor.MediaType, format) {
			return descriptor.Digest, nil
		}
	}
	if len(descriptors) == 1 && descriptors[0].Digest != "" {
		return descriptors[0].Digest, nil
	}
	return "", fmt.Errorf("OCI referrer manifest has no document blob for format %q", format)
}

func mediaTypeMatchesFormat(mediaType string, format DocumentFormat) bool {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	switch format {
	case DocumentFormatCycloneDX:
		return strings.Contains(mediaType, "cyclonedx")
	case DocumentFormatSPDX:
		return strings.Contains(mediaType, "spdx")
	case DocumentFormatInToto:
		return strings.Contains(mediaType, "in-toto") || strings.Contains(mediaType, "intoto")
	default:
		return false
	}
}

func looksLikeSourceDocument(raw []byte) bool {
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return false
	}
	if _, ok := decoded["bomFormat"]; ok {
		return true
	}
	if _, ok := decoded["spdxVersion"]; ok {
		return true
	}
	if _, ok := decoded["_type"]; ok {
		return true
	}
	return false
}

func applyAuth(req *http.Request, target TargetConfig) {
	if target.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+target.BearerToken)
		return
	}
	if target.Username != "" || target.Password != "" {
		req.SetBasicAuth(target.Username, target.Password)
	}
}

func (p HTTPProvider) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return sdk.DefaultHTTPClient(defaultHTTPTimeout)
}

func (p HTTPProvider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func readBounded(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = defaultMaxDocumentBytes
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read SBOM attestation document body: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("SBOM attestation document exceeds max_bytes")
	}
	return body, nil
}

func ociSourceURI(target TargetConfig, digest string) string {
	registry := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(target.Registry), "https://"), "http://")
	return "oci://" + strings.Trim(registry+"/"+strings.Trim(target.Repository, "/")+"@"+strings.TrimSpace(digest), "/")
}

func closeBody(body io.Closer) {
	_ = body.Close()
}
