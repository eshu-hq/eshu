// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

const (
	defaultHTTPTimeout    = 30 * time.Second
	defaultResourceLimit  = 100
	maxResourceLimit      = 500
	maxSearchPages        = 10
	maxHTTPRetries        = 2
	searchEndpoint        = "/api/search"
	datasourcesEndpoint   = "/api/datasources"
	alertRulesEndpoint    = "/api/v1/provisioning/alert-rules"
	grafanaTimeLayoutNano = time.RFC3339Nano
)

// HTTPClient reads bounded Grafana metadata through read-only REST API calls.
type HTTPClient struct {
	baseURL    *url.URL
	token      string
	httpClient HTTPDoer
}

type searchResource struct {
	ID        int64  `json:"id"`
	UID       string `json:"uid"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	FolderUID string `json:"folderUid"`
	URL       string `json:"url"`
}

type datasourceResource struct {
	ID   int64  `json:"id"`
	UID  string `json:"uid"`
	Name string `json:"name"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

type alertRuleResource struct {
	UID                     string           `json:"uid"`
	Title                   string           `json:"title"`
	RuleGroup               string           `json:"ruleGroup"`
	FolderUID               string           `json:"folderUID"`
	Condition               string           `json:"condition"`
	Data                    []alertRuleQuery `json:"data"`
	Updated                 string           `json:"updated"`
	For                     string           `json:"for"`
	NoDataState             string           `json:"noDataState"`
	ExecErrState            string           `json:"execErrState"`
	ContactPoint            string           `json:"contactPoint"`
	NotificationURL         string           `json:"notification"`
	DatasourceUID           string           `json:"datasourceUid"`
	NotificationSettingsURL string           `json:"notificationUrl"`
}

type alertRuleQuery struct {
	DatasourceUID string         `json:"datasourceUid"`
	Model         map[string]any `json:"model"`
}

// NewHTTPClient builds a bounded Grafana REST client. Credentials are checked
// only for local shape; the provider validates them on request.
func NewHTTPClient(config HTTPClientConfig) (HTTPClient, error) {
	base, err := sdk.ParseBaseURL("grafana", config.BaseURL)
	if err != nil {
		return HTTPClient{}, err
	}
	if strings.TrimSpace(config.Token) == "" {
		return HTTPClient{}, fmt.Errorf("grafana token is required")
	}
	client := config.Client
	if client == nil {
		client = sdk.DefaultHTTPClient(defaultHTTPTimeout)
	}
	return HTTPClient{
		baseURL:    base,
		token:      strings.TrimSpace(config.Token),
		httpClient: client,
	}, nil
}

// CollectObservedMetadata fetches folders, dashboards, datasources, and
// alert-rule metadata without retaining dashboard JSON, queries, URLs, or
// contact destinations.
func (c HTTPClient) CollectObservedMetadata(ctx context.Context, target TargetConfig) (CollectionResult, error) {
	result := CollectionResult{ObservedAt: time.Now().UTC()}
	if err := c.collectSearchResources(ctx, target, &result); err != nil {
		return CollectionResult{}, err
	}
	if err := c.collectDatasources(ctx, target, &result); err != nil {
		return CollectionResult{}, err
	}
	if err := c.collectAlertRules(ctx, target, &result); err != nil {
		return CollectionResult{}, err
	}
	result.Stats.Resources = len(result.Resources)
	result.Stats.Rules = len(result.Rules)
	result.Stats.Warnings = len(result.Warnings)
	return result, nil
}

func (c HTTPClient) collectSearchResources(ctx context.Context, target TargetConfig, result *CollectionResult) error {
	limit := normalizedResourceLimit(target.ResourceLimit)
	perPage := min(limit, maxResourceLimit)
	seen := map[string]struct{}{}
	page := 1
	for page <= maxSearchPages {
		query := url.Values{}
		query.Set("limit", strconv.Itoa(perPage))
		query.Set("page", strconv.Itoa(page))
		var response []searchResource
		ok, err := c.doJSON(ctx, http.MethodGet, searchEndpoint, query, nil, &response, result, ResourceClassDashboard)
		if err != nil || !ok {
			return err
		}
		result.Stats.PagesFetched++
		for _, raw := range response {
			resource, ok := normalizeSearchResource(raw, target)
			if !ok {
				continue
			}
			key := resource.Class + ":" + resource.UID
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			if len(result.Resources) >= limit {
				continue
			}
			result.Resources = append(result.Resources, resource)
		}
		if len(response) < perPage {
			break
		}
		page++
	}
	return nil
}

func (c HTTPClient) collectDatasources(ctx context.Context, target TargetConfig, result *CollectionResult) error {
	var response []datasourceResource
	ok, err := c.doJSON(ctx, http.MethodGet, datasourcesEndpoint, nil, nil, &response, result, ResourceClassDatasource)
	if err != nil || !ok {
		return err
	}
	result.Stats.PagesFetched++
	for _, raw := range response {
		resource := Resource{
			Class:              ResourceClassDatasource,
			ID:                 raw.ID,
			UID:                strings.TrimSpace(raw.UID),
			Name:               strings.TrimSpace(raw.Name),
			DatasourceType:     strings.TrimSpace(raw.Type),
			URLRedacted:        strings.TrimSpace(raw.URL) != "",
			DeclaredMatchState: declaredMatchState(target, firstNonBlank(raw.UID, raw.Name)),
		}
		if resource.UID == "" {
			resource.UID = resource.Name
		}
		result.Resources = append(result.Resources, resource)
		if resource.URLRedacted {
			result.Stats.Redactions++
		}
	}
	return nil
}

func (c HTTPClient) collectAlertRules(ctx context.Context, target TargetConfig, result *CollectionResult) error {
	var response []alertRuleResource
	ok, err := c.doJSON(ctx, http.MethodGet, alertRulesEndpoint, nil, nil, &response, result, ResourceClassAlertRule)
	if err != nil || !ok {
		return err
	}
	result.Stats.PagesFetched++
	for _, raw := range response {
		rule := AlertRule{
			UID:                     strings.TrimSpace(raw.UID),
			Title:                   strings.TrimSpace(raw.Title),
			RuleGroup:               strings.TrimSpace(raw.RuleGroup),
			FolderUID:               strings.TrimSpace(raw.FolderUID),
			Condition:               strings.TrimSpace(raw.Condition),
			UpdatedAt:               parseGrafanaTime(raw.Updated),
			For:                     strings.TrimSpace(raw.For),
			NoDataState:             strings.TrimSpace(raw.NoDataState),
			ExecErrState:            strings.TrimSpace(raw.ExecErrState),
			DatasourceUID:           firstNonBlank(raw.DatasourceUID, datasourceUID(raw.Data)),
			QueryModelRedacted:      queryModelPresent(raw.Data),
			ContactPointRedacted:    strings.TrimSpace(raw.ContactPoint) != "",
			NotificationURLRedacted: strings.TrimSpace(firstNonBlank(raw.NotificationURL, raw.NotificationSettingsURL)) != "",
			DeclaredMatchState:      declaredMatchState(target, raw.UID),
		}
		if isStale(rule.UpdatedAt, result.ObservedAt, target.StaleAfter) {
			rule.FreshnessState = FreshnessStale
			rule.Outcome = OutcomeStale
			result.Warnings = append(result.Warnings, Warning{
				ResourceClass: ResourceClassAlertRule,
				ResourceID:    rule.UID,
				Reason:        WarningStale,
			})
		}
		result.Rules = append(result.Rules, rule)
		if rule.QueryModelRedacted {
			result.Stats.Redactions++
		}
		if rule.ContactPointRedacted {
			result.Stats.Redactions++
		}
		if rule.NotificationURLRedacted {
			result.Stats.Redactions++
		}
	}
	return nil
}

func (c HTTPClient) doJSON(
	ctx context.Context,
	method string,
	endpoint string,
	query url.Values,
	body any,
	out any,
	result *CollectionResult,
	resourceClass string,
) (bool, error) {
	if body != nil {
		return false, fmt.Errorf("grafana request body is not supported")
	}
	err := sdk.DoJSON(ctx, sdk.JSONRequest{
		Provider:   "grafana",
		Method:     method,
		BaseURL:    c.baseURL,
		Endpoint:   endpoint,
		Query:      query,
		Out:        out,
		Client:     c.httpClient,
		MaxRetries: maxHTTPRetries,
		Headers: func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+c.token)
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
	return true, nil
}

func (c HTTPClient) handleStatus(resp *http.Response, result *CollectionResult, resourceClass string) (bool, error) {
	warning := Warning{ResourceClass: resourceClass, Reason: WarningPartial}
	switch resp.StatusCode {
	case http.StatusForbidden:
		warning.Reason = WarningPermissionHidden
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		warning.Reason = WarningUnsupported
	case http.StatusTooManyRequests:
		warning.Reason = WarningRateLimited
		result.Stats.RateLimits++
	default:
		if resp.StatusCode >= 500 {
			return false, GrafanaError{Provider: "grafana", StatusCode: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
		}
		return false, GrafanaError{Provider: "grafana", StatusCode: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
	}
	result.Warnings = append(result.Warnings, warning)
	result.Stats.Partial = true
	return false, nil
}

func normalizeSearchResource(raw searchResource, target TargetConfig) (Resource, bool) {
	resourceClass := ""
	switch strings.TrimSpace(raw.Type) {
	case "dash-folder":
		resourceClass = ResourceClassFolder
	case "dash-db":
		resourceClass = ResourceClassDashboard
	default:
		return Resource{}, false
	}
	uid := strings.TrimSpace(raw.UID)
	if uid == "" {
		return Resource{}, false
	}
	resource := Resource{
		Class:              resourceClass,
		ID:                 raw.ID,
		UID:                uid,
		Title:              strings.TrimSpace(raw.Title),
		FolderUID:          strings.TrimSpace(raw.FolderUID),
		URLRedacted:        strings.TrimSpace(raw.URL) != "",
		DeclaredMatchState: declaredMatchState(target, uid),
	}
	if target.ObservedOnlyHint && resource.DeclaredMatchState != MatchStateMatchedDeclared {
		resource.ManuallyCreated = true
		resource.DriftReason = DriftReasonManualProviderResource
	}
	return resource, true
}

func declaredMatchState(target TargetConfig, uid string) string {
	if _, ok := target.DeclaredUIDs[strings.TrimSpace(uid)]; ok {
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

func parseGrafanaTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(grafanaTimeLayoutNano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func queryModelPresent(values []alertRuleQuery) bool {
	for _, value := range values {
		if len(value.Model) > 0 {
			return true
		}
	}
	return false
}

func datasourceUID(values []alertRuleQuery) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value.DatasourceUID); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func isStale(updatedAt time.Time, observedAt time.Time, staleAfter time.Duration) bool {
	if staleAfter <= 0 || updatedAt.IsZero() || observedAt.IsZero() {
		return false
	}
	return updatedAt.UTC().Before(observedAt.UTC().Add(-staleAfter))
}
