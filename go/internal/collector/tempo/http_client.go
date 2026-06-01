package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
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
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"))
	if err != nil {
		return HTTPClient{}, fmt.Errorf("parse tempo base_url: %w", err)
	}
	if base.Scheme == "" || base.Host == "" {
		return HTTPClient{}, fmt.Errorf("tempo base_url must include scheme and host")
	}
	if base.User != nil {
		return HTTPClient{}, fmt.Errorf("tempo base_url must not include credentials")
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
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
	ok, err := c.do(ctx, target, echoEndpoint, nil, result, ResourceClassTraceSignal, func(resp *http.Response) error {
		if resp.StatusCode != http.StatusOK {
			return ProviderHTTPError{StatusCode: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
		}
		return nil
	})
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
	return c.do(ctx, target, endpoint, query, result, resourceClass, func(resp *http.Response) error {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode tempo response: %w", err)
		}
		return nil
	})
}

func (c HTTPClient) do(
	ctx context.Context,
	target TargetConfig,
	endpoint string,
	query url.Values,
	result *CollectionResult,
	resourceClass string,
	decode func(*http.Response) error,
) (bool, error) {
	for attempt := 0; ; attempt++ {
		req, err := c.buildRequest(ctx, target, endpoint, query)
		if err != nil {
			return false, err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return false, err
		}
		if resp.StatusCode >= 300 {
			if shouldRetryStatus(resp.StatusCode) {
				result.Stats.RateLimits += boolToInt(resp.StatusCode == http.StatusTooManyRequests)
				if attempt < maxHTTPRetries {
					result.Stats.Retries++
					_ = resp.Body.Close()
					continue
				}
			}
			defer func() { _ = resp.Body.Close() }()
			return handleStatus(resp, result, resourceClass)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := decode(resp); err != nil {
			return false, err
		}
		return true, nil
	}
}

func (c HTTPClient) buildRequest(ctx context.Context, target TargetConfig, endpoint string, query url.Values) (*http.Request, error) {
	reqURL := *c.baseURL
	reqURL.Path = path.Join(c.baseURL.Path, strings.Trim(target.PathPrefix, "/"), endpoint)
	reqURL.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build tempo request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if token := strings.TrimSpace(target.Token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if tenant := strings.TrimSpace(target.TenantID); tenant != "" {
		req.Header.Set("X-Scope-OrgID", tenant)
	}
	return req, nil
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
		result.Warnings = append(result.Warnings, warning)
		result.Stats.Partial = true
		return false, ProviderHTTPError{StatusCode: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
	default:
		return false, ProviderHTTPError{StatusCode: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
	}
	result.Warnings = append(result.Warnings, warning)
	result.Stats.Partial = true
	return false, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
