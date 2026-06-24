// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

const (
	maxMetadataDocumentBytes     = 20 << 20
	defaultMetadataFetchTimeout  = 30 * time.Second
	npmMetadataAcceptHeader      = "application/vnd.npm.install-v1+json; q=1.0, application/json; q=0.8, */*;q=0.1"
	jsonMetadataAcceptHeader     = "application/json, application/xml;q=0.5, text/xml;q=0.4, */*;q=0.1"
	xmlMetadataAcceptHeader      = "application/xml, text/xml;q=0.9, application/json;q=0.5, */*;q=0.1"
	failureClassMetadataTooLarge = "registry_metadata_too_large"
)

// ErrRateLimited marks provider responses that should be visible as rate limit
// telemetry without leaking feed URLs or credentials.
var ErrRateLimited = errors.New("package registry metadata request rate limited")

type metadataTooLargeError struct {
	limitBytes int
}

func newMetadataTooLargeError(limitBytes int) error {
	return metadataTooLargeError{limitBytes: limitBytes}
}

func (e metadataTooLargeError) Error() string {
	return fmt.Sprintf("package registry metadata exceeds configured byte limit %d bytes", e.limitBytes)
}

func (e metadataTooLargeError) FailureClass() string {
	return failureClassMetadataTooLarge
}

func (e metadataTooLargeError) FailureDetails() string {
	return fmt.Sprintf("configured_limit_bytes=%d", e.limitBytes)
}

func (e metadataTooLargeError) TerminalFailure() bool {
	return true
}

func isMetadataTooLarge(err error) bool {
	var tooLarge metadataTooLargeError
	return errors.As(err, &tooLarge)
}

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
		return MetadataDocument{}, collector.RegistryTransportFailure(
			target.Base.Provider,
			string(target.Base.Ecosystem),
			"fetch_metadata",
			sdk.HTTPError{
				Provider: target.Base.Provider,
				Message:  "request failed",
				Cause:    err,
			},
		)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var cause error
		if response.StatusCode == http.StatusTooManyRequests {
			cause = ErrRateLimited
		}
		httpErr := sdk.HTTPError{
			Provider:   target.Base.Provider,
			StatusCode: response.StatusCode,
			Message:    http.StatusText(response.StatusCode),
			RetryAfter: sdk.ParseRetryAfterHeader(response.Header.Get("Retry-After")),
			Cause:      cause,
		}
		return MetadataDocument{}, collector.RegistryHTTPFailure(
			target.Base.Provider,
			string(target.Base.Ecosystem),
			"fetch_metadata",
			response.StatusCode,
			httpErr,
		)
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
	return sdk.DefaultHTTPClient(defaultMetadataFetchTimeout)
}

func metadataAcceptHeader(ecosystem packageregistry.Ecosystem) string {
	switch ecosystem {
	case packageregistry.EcosystemNPM:
		return npmMetadataAcceptHeader
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
		return nil, newMetadataTooLargeError(maxMetadataDocumentBytes)
	}
	return body, nil
}
