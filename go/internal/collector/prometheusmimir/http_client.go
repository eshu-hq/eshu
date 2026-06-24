// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prometheusmimir

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

const (
	defaultHTTPTimeout   = 30 * time.Second
	defaultResourceLimit = 100
	maxResourceLimit     = 500
	maxHTTPRetries       = 2
	targetsEndpoint      = "/api/v1/targets"
	rulesEndpoint        = "/api/v1/rules"
	prometheusTimeLayout = time.RFC3339Nano
	targetsResourceClass = ResourceClassTarget
	rulesResourceClass   = ResourceClassRule
)

// HTTPClient reads bounded Prometheus-compatible metadata through REST APIs.
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

// ProviderAPIError carries a bounded Prometheus-compatible API status failure.
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
		return fmt.Sprintf("metric provider returned API status %q", status)
	}
	return fmt.Sprintf("metric provider returned API status %q: %s", status, errorType)
}

type apiStatusReader interface {
	apiStatus() (string, string)
}

type targetsData struct {
	ActiveTargets []targetResource `json:"activeTargets"`
}

type targetResource struct {
	DiscoveredLabels map[string]any `json:"discoveredLabels"`
	Labels           map[string]any `json:"labels"`
	ScrapePool       string         `json:"scrapePool"`
	ScrapeURL        string         `json:"scrapeUrl"`
	Health           string         `json:"health"`
	LastScrape       string         `json:"lastScrape"`
	LastError        string         `json:"lastError"`
}

type rulesData struct {
	Groups []ruleGroupResource `json:"groups"`
}

type ruleGroupResource struct {
	Name  string         `json:"name"`
	File  string         `json:"file"`
	Rules []ruleResource `json:"rules"`
}

type ruleResource struct {
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	Health         string         `json:"health"`
	Query          string         `json:"query"`
	LastEvaluation string         `json:"lastEvaluation"`
	Labels         map[string]any `json:"labels"`
	Annotations    map[string]any `json:"annotations"`
}

// NewHTTPClient builds a bounded Prometheus-compatible REST client.
func NewHTTPClient(config HTTPClientConfig) (HTTPClient, error) {
	base, err := sdk.ParseBaseURL("metric", config.BaseURL)
	if err != nil {
		return HTTPClient{}, err
	}
	client := config.Client
	if client == nil {
		client = sdk.DefaultHTTPClient(defaultHTTPTimeout)
	}
	return HTTPClient{baseURL: base, httpClient: client}, nil
}

// CollectObservedMetadata fetches target and rule metadata without retaining
// metric samples, raw PromQL, target addresses, label values, or tenant IDs.
func (c HTTPClient) CollectObservedMetadata(ctx context.Context, target TargetConfig) (CollectionResult, error) {
	provider := normalizedProvider(target.Provider)
	if provider != ProviderPrometheus && provider != ProviderMimir {
		return CollectionResult{}, fmt.Errorf("metric provider %q is not supported", provider)
	}
	result := CollectionResult{
		Source: SourceInstance{
			Provider:         provider,
			SourceInstanceID: strings.TrimSpace(target.InstanceID),
		},
		ObservedAt: time.Now().UTC(),
	}
	if tenant := strings.TrimSpace(target.TenantID); tenant != "" {
		result.Source.TenantPresent = true
		result.Source.TenantFingerprint = fingerprint(tenant)
		result.Source.TenantRedacted = true
	}
	if provider == ProviderPrometheus {
		if err := c.collectTargets(ctx, target, &result); err != nil {
			return result, err
		}
	}
	if err := c.collectRules(ctx, target, &result); err != nil {
		return result, err
	}
	result.Stats.Targets = len(result.Targets)
	result.Stats.Rules = len(result.Rules)
	result.Stats.Warnings = len(result.Warnings)
	return result, nil
}

func (c HTTPClient) collectTargets(ctx context.Context, target TargetConfig, result *CollectionResult) error {
	var response apiResponse[targetsData]
	query := url.Values{}
	query.Set("state", "active")
	ok, err := c.doJSON(ctx, target, targetsEndpoint, query, &response, result, targetsResourceClass)
	if err != nil || !ok {
		return err
	}
	result.Stats.PagesFetched++
	limit := normalizedResourceLimit(target.ResourceLimit)
	for _, raw := range response.Data.ActiveTargets {
		normalized := normalizeTarget(raw, target, result.ObservedAt)
		if normalized.ProviderObjectID == "" {
			continue
		}
		if len(result.Targets) >= limit {
			continue
		}
		result.Targets = append(result.Targets, normalized)
		recordObservationStats(normalized.FreshnessState, normalized.ScrapeURLRedacted || normalized.LastErrorRedacted, result)
		if normalized.FreshnessState == FreshnessStale {
			result.Warnings = append(result.Warnings, Warning{
				ResourceClass: ResourceClassTarget,
				ResourceID:    normalized.ProviderObjectID,
				Reason:        WarningStale,
			})
		}
	}
	return nil
}

func (c HTTPClient) collectRules(ctx context.Context, target TargetConfig, result *CollectionResult) error {
	var response apiResponse[rulesData]
	ok, err := c.doJSON(ctx, target, rulesEndpoint, nil, &response, result, rulesResourceClass)
	if err != nil || !ok {
		return err
	}
	result.Stats.PagesFetched++
	limit := normalizedResourceLimit(target.ResourceLimit)
	for _, group := range response.Data.Groups {
		for _, raw := range group.Rules {
			normalized := normalizeRule(group, raw, target, result.ObservedAt)
			if normalized.ProviderObjectID == "" {
				continue
			}
			if len(result.Rules) >= limit {
				continue
			}
			result.Rules = append(result.Rules, normalized)
			recordObservationStats(normalized.FreshnessState, normalized.QueryRedacted, result)
			if normalized.FreshnessState == FreshnessStale {
				result.Warnings = append(result.Warnings, Warning{
					ResourceClass: ResourceClassRule,
					ResourceID:    normalized.ProviderObjectID,
					Reason:        WarningStale,
				})
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
	err := sdk.DoJSON(ctx, sdk.JSONRequest{
		Provider:   "metric",
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
			result.Stats.Retries++
			if resp.StatusCode == http.StatusTooManyRequests {
				result.Stats.RateLimits++
			}
		},
		StatusError: func(resp *http.Response) error {
			ok, err := c.handleStatus(resp, result, resourceClass)
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
	if statusReader, ok := out.(apiStatusReader); ok {
		status, errorType := statusReader.apiStatus()
		if status != "" && status != "success" {
			return false, ProviderAPIError{Status: status, ErrorType: errorType}
		}
	}
	return true, nil
}

func (c HTTPClient) handleStatus(resp *http.Response, result *CollectionResult, resourceClass string) (bool, error) {
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
		return false, ProviderHTTPError{Provider: "metric", StatusCode: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
	}
	result.Warnings = append(result.Warnings, warning)
	result.Stats.Partial = true
	return false, nil
}

func normalizeTarget(raw targetResource, target TargetConfig, observedAt time.Time) Target {
	scrapeURL := strings.TrimSpace(raw.ScrapeURL)
	labelKeys := sortedKeys(raw.Labels)
	discoveredKeys := sortedKeys(raw.DiscoveredLabels)
	identity := targetIdentity(raw, labelKeys, discoveredKeys)
	out := Target{
		ProviderObjectID:   identity,
		ScrapePool:         strings.TrimSpace(raw.ScrapePool),
		Health:             strings.TrimSpace(raw.Health),
		ScrapeURLRedacted:  scrapeURL != "",
		LabelKeys:          labelKeys,
		DiscoveredKeys:     discoveredKeys,
		LastScrapeAt:       parseProviderTime(raw.LastScrape),
		LastErrorRedacted:  strings.TrimSpace(raw.LastError) != "",
		DeclaredMatchState: declaredMatchState(target, identity),
	}
	if isStale(out.LastScrapeAt, observedAt, target.StaleAfter) {
		out.FreshnessState = FreshnessStale
		out.Outcome = OutcomeStale
	}
	if target.ObservedOnlyHint && out.DeclaredMatchState != MatchStateMatchedDeclared {
		out.ManuallyCreated = true
	}
	return out
}

func normalizeRule(group ruleGroupResource, raw ruleResource, target TargetConfig, observedAt time.Time) Rule {
	groupName := strings.TrimSpace(group.Name)
	ruleName := strings.TrimSpace(raw.Name)
	identity := ruleIdentity(groupName, ruleName, raw.Query)
	out := Rule{
		ProviderObjectID:   identity,
		GroupName:          groupName,
		RuleName:           ruleName,
		RuleType:           strings.TrimSpace(raw.Type),
		Health:             strings.TrimSpace(raw.Health),
		QueryRedacted:      strings.TrimSpace(raw.Query) != "",
		LabelKeys:          sortedKeys(raw.Labels),
		AnnotationKeys:     sortedKeys(raw.Annotations),
		LastEvaluationAt:   parseProviderTime(raw.LastEvaluation),
		DeclaredMatchState: declaredMatchState(target, identity),
	}
	if isStale(out.LastEvaluationAt, observedAt, target.StaleAfter) {
		out.FreshnessState = FreshnessStale
		out.Outcome = OutcomeStale
	}
	if target.ObservedOnlyHint && out.DeclaredMatchState != MatchStateMatchedDeclared {
		out.ManuallyCreated = true
	}
	return out
}

func recordObservationStats(freshness string, redacted bool, result *CollectionResult) {
	if freshness == FreshnessStale {
		result.Stats.Stale++
	}
	if redacted {
		result.Stats.Redactions++
	}
}

func targetIdentity(raw targetResource, labelKeys []string, discoveredKeys []string) string {
	scrapePool := strings.TrimSpace(raw.ScrapePool)
	scrapeURL := strings.TrimSpace(raw.ScrapeURL)
	if scrapePool == "" && scrapeURL == "" && len(labelKeys) == 0 && len(discoveredKeys) == 0 {
		return ""
	}
	return fingerprintJoined(scrapePool, scrapeURL, strings.Join(labelKeys, ","), strings.Join(discoveredKeys, ","))
}

func ruleIdentity(groupName string, ruleName string, query string) string {
	switch {
	case groupName != "" && ruleName != "":
		return groupName + ":" + ruleName
	case groupName != "":
		return "group:" + groupName
	case ruleName != "":
		return "rule:" + ruleName
	case strings.TrimSpace(query) != "":
		return fingerprint(query)
	default:
		return ""
	}
}

func declaredMatchState(target TargetConfig, id string) string {
	if _, ok := target.DeclaredIDs[strings.TrimSpace(id)]; ok {
		return MatchStateMatchedDeclared
	}
	return MatchStateNotCompared
}

func normalizedResourceLimit(value int) int {
	switch {
	case value <= 0:
		return defaultResourceLimit
	case value > maxResourceLimit:
		return maxResourceLimit
	default:
		return value
	}
}

func parseProviderTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(prometheusTimeLayout, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func normalizedProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", ProviderPrometheus:
		return ProviderPrometheus
	case ProviderMimir:
		return ProviderMimir
	default:
		return strings.TrimSpace(provider)
	}
}

func isStale(updatedAt time.Time, observedAt time.Time, staleAfter time.Duration) bool {
	if staleAfter <= 0 || updatedAt.IsZero() || observedAt.IsZero() {
		return false
	}
	return updatedAt.UTC().Before(observedAt.UTC().Add(-staleAfter))
}
