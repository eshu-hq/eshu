// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package loki

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"

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
	// defaultSeriesLookback bounds the /loki/api/v1/series query window when
	// neither TargetConfig.SeriesLookback nor TargetConfig.StaleAfter is
	// configured. Generous enough to avoid dropping series that are still
	// within a typical observability retention window.
	defaultSeriesLookback = 24 * time.Hour
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

// ProviderAPIError carries a bounded Loki API status failure.
type ProviderAPIError struct {
	Status    string
	ErrorType string
}

func (e ProviderAPIError) Error() string {
	status := strings.TrimSpace(e.Status)
	if status == "" {
		status = "error"
	}
	errorType := strings.TrimSpace(e.ErrorType)
	if errorType == "" {
		return fmt.Sprintf("loki provider returned API status %q", status)
	}
	return fmt.Sprintf("loki provider returned API status %q: %s", status, errorType)
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
	base, err := sdk.ParseBaseURL("loki", config.BaseURL)
	if err != nil {
		return HTTPClient{}, err
	}
	client := config.Client
	if client == nil {
		client = sdk.DefaultHTTPClient(defaultHTTPTimeout)
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
		return result, err
	}
	if err := c.collectSeries(ctx, target, &result); err != nil {
		return result, err
	}
	if err := c.collectRules(ctx, target, &result); err != nil {
		return result, err
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
	truncated := false
	for _, series := range response.Data {
		normalized := normalizeSeries(series, target, result.ObservedAt)
		if normalized.ProviderObjectID == "" {
			continue
		}
		if _, exists := seen[normalized.ProviderObjectID]; exists {
			continue
		}
		seen[normalized.ProviderObjectID] = struct{}{}
		// The cap counts every retained signal, including the label-set signal
		// collectLabels already appended, so detect truncation from the actual
		// cap-skip of a distinct series -- not from len(seen), which would miss
		// a drop whenever a non-series signal has consumed cap budget.
		if len(result.Signals) >= limit {
			truncated = true
			continue
		}
		result.Signals = append(result.Signals, normalized)
		recordObservationStats(normalized.FreshnessState, normalized.SeriesFingerprint != "", result)
	}
	if truncated {
		result.Stats.Truncated = true
		result.Warnings = append(result.Warnings, Warning{
			ResourceClass: seriesResourceClass,
			Reason:        WarningTruncated,
		})
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
	truncated := false
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
				// Detect truncation from the actual cap-skip of a distinct
				// rule, mirroring collectSeries, so the signal stays correct
				// even if result.Rules ever gains non-rule budget consumers.
				if len(result.Rules) >= limit {
					truncated = true
					continue
				}
				result.Rules = append(result.Rules, normalized)
				recordObservationStats(normalized.FreshnessState, normalized.QueryRedacted, result)
			}
		}
	}
	if truncated {
		result.Stats.Truncated = true
		result.Warnings = append(result.Warnings, Warning{
			ResourceClass: rulesResourceClass,
			Reason:        WarningTruncated,
		})
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
	ok, err := c.do(ctx, target, endpoint, query, out, nil, result, resourceClass)
	if err != nil || !ok {
		return ok, err
	}
	if statusReader, ok := out.(apiStatusReader); ok {
		status, errorType := statusReader.apiStatus()
		if status != "" && status != "success" {
			return false, ProviderAPIError{Status: status, ErrorType: errorType}
		}
	}
	return true, nil
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
	return c.do(ctx, target, endpoint, query, nil, func(body io.Reader) error {
		return yaml.NewDecoder(body).Decode(out)
	}, result, resourceClass)
}

func (c HTTPClient) do(
	ctx context.Context,
	target TargetConfig,
	endpoint string,
	query url.Values,
	out any,
	decode func(io.Reader) error,
	result *CollectionResult,
	resourceClass string,
) (bool, error) {
	err := sdk.DoJSON(ctx, sdk.JSONRequest{
		Provider:   "loki",
		Method:     http.MethodGet,
		BaseURL:    c.baseURL,
		PathPrefix: target.PathPrefix,
		Endpoint:   endpoint,
		Query:      query,
		Out:        out,
		Client:     c.httpClient,
		MaxRetries: maxHTTPRetries,
		Decode:     decode,
		Headers: func(req *http.Request) {
			req.Header.Set("Accept", "application/json, application/yaml")
			if token := strings.TrimSpace(target.Token); token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			if tenant := strings.TrimSpace(target.TenantID); tenant != "" {
				req.Header.Set("X-Scope-OrgID", tenant)
			}
		},
		OnRetry: func(resp *http.Response, _ int) {
			result.Stats.Retries++
			if resp.StatusCode == http.StatusTooManyRequests {
				result.Stats.RateLimits++
			}
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
	default:
		return false, ProviderHTTPError{Provider: "loki", StatusCode: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
	}
	result.Warnings = append(result.Warnings, warning)
	result.Stats.Partial = true
	return false, nil
}
