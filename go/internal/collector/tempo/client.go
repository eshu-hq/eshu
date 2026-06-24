// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

const (
	defaultHTTPTimeout   = 30 * time.Second
	defaultResourceLimit = 100
	defaultTagValueLimit = 20
	defaultLookback      = time.Hour
	maxResourceLimit     = 500
	maxTagValueLimit     = 100
	maxHTTPRetries       = 2
	echoEndpoint         = "/api/echo"
	tagsEndpoint         = "/api/v2/search/tags"
	tagValuesPrefix      = "/api/v2/search/tag/"
)

// HTTPClient reads bounded Tempo metadata through REST APIs.
type HTTPClient struct {
	baseURL    *url.URL
	httpClient HTTPDoer
}

// NewHTTPClient builds a bounded Tempo REST client.
func NewHTTPClient(config HTTPClientConfig) (HTTPClient, error) {
	base, err := sdk.ParseBaseURL("tempo", config.BaseURL)
	if err != nil {
		return HTTPClient{}, err
	}
	client := config.Client
	if client == nil {
		client = sdk.DefaultHTTPClient(defaultHTTPTimeout)
	}
	return HTTPClient{baseURL: base, httpClient: client}, nil
}

// CollectObservedMetadata fetches Tempo tag and tag-value metadata without
// retaining spans, traces, raw trace IDs, TraceQL, tag values, tenant IDs, or
// provider response bodies.
func (c HTTPClient) CollectObservedMetadata(ctx context.Context, target TargetConfig) (CollectionResult, error) {
	observedAt := time.Now().UTC()
	if target.Now != nil {
		observedAt = target.Now().UTC()
	}
	result := CollectionResult{
		Source: SourceInstance{
			Provider:         ProviderTempo,
			SourceInstanceID: strings.TrimSpace(target.InstanceID),
		},
		ObservedAt: observedAt,
	}
	if tenant := strings.TrimSpace(target.TenantID); tenant != "" {
		result.Source.TenantPresent = true
		result.Source.TenantFingerprint = fingerprint(tenant)
		result.Source.TenantRedacted = true
	}
	if target.FreshnessProbeEnable {
		if err := c.collectEcho(ctx, target, &result); err != nil {
			return result, err
		}
	}
	tagKeys, err := c.collectTags(ctx, target, &result)
	if err != nil {
		return result, err
	}
	if err := c.collectAllowedTagValues(ctx, target, tagKeys, &result); err != nil {
		return result, err
	}
	result.Stats.Signals = len(result.Signals)
	result.Stats.Warnings = len(result.Warnings)
	return result, nil
}

func (c HTTPClient) collectEcho(ctx context.Context, target TargetConfig, result *CollectionResult) error {
	ok, err := c.do(ctx, target, echoEndpoint, nil, nil, result, ResourceClassTraceSignal)
	if err != nil || !ok {
		return err
	}
	result.Stats.PagesFetched++
	return nil
}

func (c HTTPClient) collectTags(ctx context.Context, target TargetConfig, result *CollectionResult) ([]string, error) {
	var response tagSearchResponse
	query := withQueryLimit(queryRange(result.ObservedAt, target.Lookback), normalizedResourceLimit(target.ResourceLimit))
	ok, err := c.doJSON(ctx, target, tagsEndpoint, query, &response, result, ResourceClassTraceSignal)
	if err != nil || !ok {
		return nil, err
	}
	result.Stats.PagesFetched++
	signal := normalizeTagSet(response, target)
	if signal.ProviderObjectID == "" {
		return nil, nil
	}
	result.Signals = append(result.Signals, signal)
	return signal.TagKeys, nil
}

func (c HTTPClient) collectAllowedTagValues(
	ctx context.Context,
	target TargetConfig,
	tagKeys []string,
	result *CollectionResult,
) error {
	allowed := cleanStringSlice(target.TagValueNames)
	for _, tagName := range allowed {
		if !tagAvailable(tagName, tagKeys) {
			continue
		}
		var response tagValuesResponse
		endpoint := tagValuesPrefix + url.PathEscape(tagName) + "/values"
		query := withQueryLimit(queryRange(result.ObservedAt, target.Lookback), normalizedTagValueLimit(target.MaxTagValuesPerTag)+1)
		ok, err := c.doJSON(ctx, target, endpoint, query, &response, result, ResourceClassTraceSignal)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		result.Stats.PagesFetched++
		signal := normalizeTagValues(tagName, response, target)
		if signal.ProviderObjectID == "" {
			continue
		}
		if signal.TagValueCount > normalizedTagValueLimit(target.MaxTagValuesPerTag) {
			result.Stats.HighCardinalityRejected++
			result.Warnings = append(result.Warnings, Warning{
				ResourceClass: ResourceClassTraceSignal,
				ResourceID:    "tag:" + tagName,
				Reason:        WarningHighCardinality,
			})
		} else {
			result.Stats.Redactions += len(signal.TagValueHashes)
		}
		result.Signals = append(result.Signals, signal)
	}
	return nil
}

func (c HTTPClient) doJSON(
	ctx context.Context,
	target TargetConfig,
	endpoint string,
	query url.Values,
	out any,
	result *CollectionResult,
	resourceClass string,
) (bool, error) {
	return c.do(ctx, target, endpoint, query, out, result, resourceClass)
}

func (c HTTPClient) do(
	ctx context.Context,
	target TargetConfig,
	endpoint string,
	query url.Values,
	out any,
	result *CollectionResult,
	resourceClass string,
) (bool, error) {
	err := sdk.DoJSON(ctx, sdk.JSONRequest{
		Provider:   "tempo",
		Method:     http.MethodGet,
		BaseURL:    c.baseURL,
		PathPrefix: target.PathPrefix,
		Endpoint:   endpoint,
		Query:      query,
		Out:        out,
		Client:     c.httpClient,
		MaxRetries: maxHTTPRetries,
		Headers: func(req *http.Request) {
			if token := strings.TrimSpace(target.Token); token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			if tenant := strings.TrimSpace(target.TenantID); tenant != "" {
				req.Header.Set("X-Scope-OrgID", tenant)
			}
		},
		OnRetry: func(resp *http.Response, _ int) {
			if resp.StatusCode == http.StatusTooManyRequests {
				result.Stats.RateLimits++
			}
			result.Stats.Retries++
		},
		StatusError: func(resp *http.Response) error {
			ok, err := handleStatus(resp, result, resourceClass)
			if err != nil {
				return err
			}
			if !ok {
				return sdk.ErrStatusHandled
			}
			return nil
		},
	})
	if errors.Is(err, sdk.ErrStatusHandled) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func handleStatus(resp *http.Response, result *CollectionResult, resourceClass string) (bool, error) {
	warning := Warning{ResourceClass: resourceClass, Reason: WarningPartial}
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusUnauthorized:
		warning.Reason = WarningPermissionHidden
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		warning.Reason = WarningUnsupported
	case http.StatusTooManyRequests:
		warning.Reason = WarningRateLimited
		result.Stats.RateLimits++
		result.Warnings = append(result.Warnings, warning)
		result.Stats.Partial = true
		return false, ProviderHTTPError{Provider: "tempo", StatusCode: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
	default:
		return false, ProviderHTTPError{Provider: "tempo", StatusCode: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
	}
	result.Warnings = append(result.Warnings, warning)
	result.Stats.Partial = true
	return false, nil
}
