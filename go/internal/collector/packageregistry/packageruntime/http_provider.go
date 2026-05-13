package packageruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
)

const (
	maxMetadataDocumentBytes    = 20 << 20
	defaultMetadataFetchTimeout = 30 * time.Second
	jsonMetadataAcceptHeader    = "application/json, application/xml;q=0.5, text/xml;q=0.4, */*;q=0.1"
	xmlMetadataAcceptHeader     = "application/xml, text/xml;q=0.9, application/json;q=0.5, */*;q=0.1"
)

// ErrRateLimited marks provider responses that should be visible as rate limit
// telemetry without leaking feed URLs or credentials.
var ErrRateLimited = errors.New("package registry metadata request rate limited")

// HTTPMetadataProvider fetches parser-ready package metadata from an explicit
// package feed endpoint.
type HTTPMetadataProvider struct {
	Client *http.Client
}

// FetchMetadata retrieves a bounded metadata document and never includes
// credentials in returned errors.
func (p HTTPMetadataProvider) FetchMetadata(ctx context.Context, target TargetConfig) (MetadataDocument, error) {
	metadataURL := strings.TrimSpace(target.MetadataURL)
	if metadataURL == "" {
		return MetadataDocument{}, fmt.Errorf("metadata_url is required")
	}
	client := p.httpClient()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return MetadataDocument{}, fmt.Errorf("build package metadata request: %w", err)
	}
	request.Header.Set("Accept", metadataAcceptHeader(target.Base.Ecosystem))
	if strings.TrimSpace(target.BearerToken) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(target.BearerToken))
	} else if strings.TrimSpace(target.Username) != "" || target.Password != "" {
		request.SetBasicAuth(strings.TrimSpace(target.Username), target.Password)
	}

	startedAt := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return MetadataDocument{}, fmt.Errorf("request package metadata: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if response.StatusCode == http.StatusTooManyRequests {
			return MetadataDocument{}, ErrRateLimited
		}
		return MetadataDocument{}, fmt.Errorf("request package metadata returned status %d", response.StatusCode)
	}
	body, err := readBoundedMetadata(response.Body)
	if err != nil {
		return MetadataDocument{}, err
	}
	return MetadataDocument{
		Body:         body,
		SourceURI:    safeSourceURI(metadataURL),
		DocumentType: string(target.Base.Ecosystem),
		ObservedAt:   startedAt.UTC(),
	}, nil
}

func (p HTTPMetadataProvider) httpClient() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: defaultMetadataFetchTimeout}
}

func metadataAcceptHeader(ecosystem packageregistry.Ecosystem) string {
	switch ecosystem {
	case packageregistry.EcosystemMaven, packageregistry.EcosystemNuGet:
		return xmlMetadataAcceptHeader
	default:
		return jsonMetadataAcceptHeader
	}
}

func readBoundedMetadata(reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, maxMetadataDocumentBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read package metadata: %w", err)
	}
	if len(body) > maxMetadataDocumentBytes {
		return nil, fmt.Errorf("package metadata exceeds %d bytes", maxMetadataDocumentBytes)
	}
	return body, nil
}
