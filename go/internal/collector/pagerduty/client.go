// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

const defaultPagerDutyAPIBaseURL = "https://api.pagerduty.com"

// HTTPClientConfig configures the PagerDuty REST API reader.
type HTTPClientConfig struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

// HTTPClient reads PagerDuty incidents, log entries, and related change
// events through the PagerDuty REST API.
type HTTPClient struct {
	baseURL *url.URL
	token   string
	client  sdk.HTTPDoer
}

// NewHTTPClient validates configuration and returns a PagerDuty REST client.
func NewHTTPClient(config HTTPClientConfig) (*HTTPClient, error) {
	baseURL, err := sdk.ParseBaseURL("pagerduty api", firstNonBlank(config.BaseURL, defaultPagerDutyAPIBaseURL))
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(config.Token)
	if token == "" {
		return nil, fmt.Errorf("pagerduty token is required")
	}
	client := config.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPClient{baseURL: baseURL, token: token, client: client}, nil
}

// CollectIncidentEvidence fetches one bounded PagerDuty incident evidence
// window.
func (c *HTTPClient) CollectIncidentEvidence(
	ctx context.Context,
	target TargetConfig,
	window CollectionWindow,
) (CollectionResult, error) {
	incidents, err := c.listIncidents(ctx, target, window)
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{
		Incidents:           incidents,
		LifecycleEvents:     map[string][]LifecycleEvent{},
		RelatedChangeEvents: map[string][]ChangeEvent{},
		ObservedAt:          window.Until.UTC(),
		PagesFetched:        1,
	}
	for _, incident := range incidents {
		logs, err := c.listLogEntries(ctx, incident.ID, target.LogEntryLimit)
		if err != nil {
			return CollectionResult{}, err
		}
		result.LifecycleEvents[incident.ID] = logs
		changes, err := c.listRelatedChangeEvents(ctx, incident.ID, target.ChangeEventLimit)
		if err != nil {
			if retryableConfigError(err) {
				return CollectionResult{}, err
			}
			if warning, ok := configWarningFromError(ConfigResourceClassRelatedChangeEvent, incident.ID, err); ok {
				result.Warnings = append(result.Warnings, warning)
				continue
			}
			return CollectionResult{}, err
		}
		result.RelatedChangeEvents[incident.ID] = changes
	}
	return result, nil
}

func (c *HTTPClient) listIncidents(ctx context.Context, target TargetConfig, window CollectionWindow) ([]Incident, error) {
	values := url.Values{}
	if !window.Since.IsZero() {
		values.Set("since", window.Since.UTC().Format(time.RFC3339))
	}
	if !window.Until.IsZero() {
		values.Set("until", window.Until.UTC().Format(time.RFC3339))
	}
	if target.IncidentLimit > 0 {
		values.Set("limit", strconv.Itoa(target.IncidentLimit))
	}
	for _, serviceID := range target.AllowedServiceIDs {
		if trimmed := strings.TrimSpace(serviceID); trimmed != "" {
			values.Add("service_ids[]", trimmed)
		}
	}
	var decoded incidentListResponse
	if err := c.getJSON(ctx, "/incidents", values, &decoded); err != nil {
		return nil, err
	}
	return normalizeIncidents(decoded.Incidents), nil
}

func (c *HTTPClient) listLogEntries(ctx context.Context, incidentID string, limit int) ([]LifecycleEvent, error) {
	values := url.Values{}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	var decoded logEntryListResponse
	path := "/incidents/" + url.PathEscape(incidentID) + "/log_entries"
	if err := c.getJSON(ctx, path, values, &decoded); err != nil {
		return nil, err
	}
	return normalizeLifecycleEvents(incidentID, decoded.LogEntries), nil
}

func (c *HTTPClient) listRelatedChangeEvents(ctx context.Context, incidentID string, limit int) ([]ChangeEvent, error) {
	values := url.Values{}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	var decoded changeEventListResponse
	path := "/incidents/" + url.PathEscape(incidentID) + "/related_change_events"
	if err := c.getJSON(ctx, path, values, &decoded); err != nil {
		return nil, err
	}
	return normalizeChangeEvents(decoded.ChangeEvents), nil
}

func (c *HTTPClient) getJSON(ctx context.Context, path string, values url.Values, out any) error {
	return sdk.DoJSON(ctx, sdk.JSONRequest{
		Provider: "pagerduty",
		Method:   http.MethodGet,
		BaseURL:  c.baseURL,
		Endpoint: path,
		Query:    values,
		Out:      out,
		Client:   c.client,
		Headers: func(req *http.Request) {
			req.Header.Set("Authorization", "Token token="+c.token)
		},
	})
}

func defaultClientFactory(target TargetConfig) (EvidenceClient, error) {
	return NewHTTPClient(HTTPClientConfig{
		BaseURL: target.APIBaseURL,
		Token:   target.Token,
	})
}
