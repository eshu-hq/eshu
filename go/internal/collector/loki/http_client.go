package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultHTTPTimeout     = 30 * time.Second
	defaultResourceLimit   = 100
	defaultLabelValueLimit = 20
	maxResourceLimit       = 500
	maxLabelValueLimit     = 100
	maxHTTPRetries         = 2
	labelsEndpoint         = "/loki/api/v1/labels"
	seriesEndpoint         = "/loki/api/v1/series"
	rulesEndpoint          = "/loki/api/v1/rules"
	defaultSeriesMatcher   = `{job=~".+"}`
	labelsResourceClass    = ResourceClassLogSignal
	seriesResourceClass    = ResourceClassLogSignal
	rulesResourceClass     = ResourceClassRule
)

// HTTPClient reads bounded Loki metadata through REST APIs.
type HTTPClient struct {
	baseURL    *url.URL
	httpClient HTTPDoer
}

type apiResponse[T any] struct {
	Status    string `json:"status"`
	Data      T      `json:"data"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
}

func (r *apiResponse[T]) apiStatus() (string, string) {
	if r == nil {
		return "", ""
	}
	return strings.TrimSpace(r.Status), strings.TrimSpace(r.ErrorType)
}

type apiStatusReader interface {
	apiStatus() (string, string)
}

type rulesNamespace map[string][]ruleGroupResource

type ruleGroupResource struct {
	Name  string         `yaml:"name"`
	Rules []ruleResource `yaml:"rules"`
}

type ruleResource struct {
	Alert       string         `yaml:"alert"`
	Record      string         `yaml:"record"`
	Expr        string         `yaml:"expr"`
	Labels      map[string]any `yaml:"labels"`
	Annotations map[string]any `yaml:"annotations"`
}

// NewHTTPClient builds a bounded Loki REST client.
func NewHTTPClient(config HTTPClientConfig) (HTTPClient, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"))
	if err != nil {
		return HTTPClient{}, fmt.Errorf("parse loki base_url: %w", err)
	}
	if base.Scheme == "" || base.Host == "" {
		return HTTPClient{}, fmt.Errorf("loki base_url must include scheme and host")
	}
	if base.User != nil {
		return HTTPClient{}, fmt.Errorf("loki base_url must not include credentials")
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return HTTPClient{baseURL: base, httpClient: client}, nil
}

// CollectObservedMetadata fetches Loki label, series, and ruler metadata
// without retaining log lines, raw LogQL, label values, tenant IDs, or payload
// bodies.
func (c HTTPClient) CollectObservedMetadata(ctx context.Context, target TargetConfig) (CollectionResult, error) {
	result := CollectionResult{
		Source: SourceInstance{
			Provider:         ProviderLoki,
			SourceInstanceID: strings.TrimSpace(target.InstanceID),
		},
		ObservedAt: time.Now().UTC(),
	}
	if tenant := strings.TrimSpace(target.TenantID); tenant != "" {
		result.Source.TenantPresent = true
		result.Source.TenantFingerprint = fingerprint(tenant)
		result.Source.TenantRedacted = true
	}
	if err := c.collectLabels(ctx, target, &result); err != nil {
		return CollectionResult{}, err
	}
	if err := c.collectSeries(ctx, target, &result); err != nil {
		return CollectionResult{}, err
	}
	if err := c.collectRules(ctx, target, &result); err != nil {
		return CollectionResult{}, err
	}
	result.Stats.Signals = len(result.Signals)
	result.Stats.Rules = len(result.Rules)
	result.Stats.Warnings = len(result.Warnings)
	return result, nil
}

func (c HTTPClient) collectLabels(ctx context.Context, target TargetConfig, result *CollectionResult) error {
	var response apiResponse[[]string]
	ok, err := c.doJSON(ctx, target, labelsEndpoint, timeQuery(result.ObservedAt), &response, result, labelsResourceClass)
	if err != nil || !ok {
		return err
	}
	result.Stats.PagesFetched++
	labelKeys := cleanStringSlice(response.Data)
	values, err := c.collectAllowedLabelValues(ctx, target, labelKeys, result)
	if err != nil {
		return err
	}
	if len(labelKeys) == 0 && len(values.counts) == 0 {
		return nil
	}
	signal := LogSignal{
		ProviderObjectID: strings.TrimSpace(fingerprintJoined("labels", strings.Join(labelKeys, ","))),
		SignalKind:       SignalKindLabelSet,
		LabelKeys:        labelKeys,
		LabelValueCounts: values.counts,
		LabelValueHashes: values.hashes,
	}
	result.Signals = append(result.Signals, signal)
	recordObservationStats(signal.FreshnessState, len(values.hashes) > 0, result)
	return nil
}

type labelValueMetadata struct {
	counts map[string]int
	hashes map[string][]string
}

func (c HTTPClient) collectAllowedLabelValues(
	ctx context.Context,
	target TargetConfig,
	labelKeys []string,
	result *CollectionResult,
) (labelValueMetadata, error) {
	allowed := allowedLabelValueSet(target.LabelValueNames, labelKeys)
	out := labelValueMetadata{counts: map[string]int{}, hashes: map[string][]string{}}
	for _, label := range allowed {
		var response apiResponse[[]string]
		endpoint := "/loki/api/v1/label/" + url.PathEscape(label) + "/values"
		ok, err := c.doJSON(ctx, target, endpoint, timeQuery(result.ObservedAt), &response, result, labelsResourceClass)
		if err != nil {
			return labelValueMetadata{}, err
		}
		if !ok {
			continue
		}
		result.Stats.PagesFetched++
		values := cleanStringSlice(response.Data)
		out.counts[label] = len(values)
		if len(values) > normalizedLabelValueLimit(target.MaxLabelValuesPerLabel) {
			result.Stats.HighCardinalityRejected++
			result.Warnings = append(result.Warnings, Warning{
				ResourceClass: ResourceClassLogSignal,
				ResourceID:    "label:" + label,
				Reason:        WarningHighCardinality,
			})
			continue
		}
		for _, value := range values {
			if fp := fingerprint(value); fp != "" {
				out.hashes[label] = append(out.hashes[label], fp)
			}
		}
		if len(out.hashes[label]) > 0 {
			result.Stats.Redactions += len(out.hashes[label])
		}
	}
	return out, nil
}

func (c HTTPClient) collectSeries(ctx context.Context, target TargetConfig, result *CollectionResult) error {
	var response apiResponse[[]map[string]string]
	ok, err := c.doJSON(ctx, target, seriesEndpoint, seriesQuery(target, result.ObservedAt), &response, result, seriesResourceClass)
	if err != nil || !ok {
		return err
	}
	result.Stats.PagesFetched++
	limit := normalizedResourceLimit(target.ResourceLimit)
	seen := map[string]struct{}{}
	for _, series := range response.Data {
		normalized := normalizeSeries(series, target, result.ObservedAt)
		if normalized.ProviderObjectID == "" {
			continue
		}
		if _, exists := seen[normalized.ProviderObjectID]; exists {
			continue
		}
		seen[normalized.ProviderObjectID] = struct{}{}
		if len(result.Signals) >= limit {
			continue
		}
		result.Signals = append(result.Signals, normalized)
		recordObservationStats(normalized.FreshnessState, normalized.SeriesFingerprint != "", result)
	}
	return nil
}

func (c HTTPClient) collectRules(ctx context.Context, target TargetConfig, result *CollectionResult) error {
	var response rulesNamespace
	ok, err := c.doYAML(ctx, target, rulesEndpoint, nil, &response, result, rulesResourceClass)
	if err != nil || !ok {
		return err
	}
	result.Stats.PagesFetched++
	limit := normalizedResourceLimit(target.ResourceLimit)
	seen := map[string]struct{}{}
	for namespace, groups := range response {
		for _, group := range groups {
			for _, raw := range group.Rules {
				normalized := normalizeRule(namespace, group, raw, target, result.ObservedAt)
				if normalized.ProviderObjectID == "" {
					continue
				}
				if _, exists := seen[normalized.ProviderObjectID]; exists {
					continue
				}
				seen[normalized.ProviderObjectID] = struct{}{}
				if len(result.Rules) >= limit {
					continue
				}
				result.Rules = append(result.Rules, normalized)
				recordObservationStats(normalized.FreshnessState, normalized.QueryRedacted, result)
			}
		}
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
			return fmt.Errorf("decode loki response: %w", err)
		}
		if statusReader, ok := out.(apiStatusReader); ok {
			status, errorType := statusReader.apiStatus()
			if status != "" && status != "success" {
				return ProviderAPIError{Status: status, ErrorType: errorType}
			}
		}
		return nil
	})
}

func (c HTTPClient) doYAML(
	ctx context.Context,
	target TargetConfig,
	endpoint string,
	query url.Values,
	out any,
	result *CollectionResult,
	resourceClass string,
) (bool, error) {
	return c.do(ctx, target, endpoint, query, result, resourceClass, func(resp *http.Response) error {
		if err := yaml.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode loki rules response: %w", err)
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
		reqURL := *c.baseURL
		reqURL.Path = path.Join(c.baseURL.Path, strings.Trim(target.PathPrefix, "/"), endpoint)
		reqURL.RawQuery = query.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
		if err != nil {
			return false, fmt.Errorf("build loki request: %w", err)
		}
		req.Header.Set("Accept", "application/json, application/yaml")
		if token := strings.TrimSpace(target.Token); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if tenant := strings.TrimSpace(target.TenantID); tenant != "" {
			req.Header.Set("X-Scope-OrgID", tenant)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return false, err
		}
		if resp.StatusCode >= 300 {
			if shouldRetryStatus(resp.StatusCode) && attempt < maxHTTPRetries {
				result.Stats.Retries++
				if resp.StatusCode == http.StatusTooManyRequests {
					result.Stats.RateLimits++
				}
				_ = resp.Body.Close()
				continue
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
	default:
		return false, ProviderHTTPError{StatusCode: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
	}
	result.Warnings = append(result.Warnings, warning)
	result.Stats.Partial = true
	return false, nil
}
