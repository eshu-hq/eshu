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
// window. Each list endpoint (incidents, and per-incident log entries and
// related change events) follows PagerDuty's classic offset pagination up to
// the target's configured page/record bound; CollectionResult.Truncated and a
// ConfigWarningTruncated coverage warning are set only when that bound was hit
// while the provider still had more pages ("more":true), never when
// pagination exhausted naturally.
func (c *HTTPClient) CollectIncidentEvidence(
	ctx context.Context,
	target TargetConfig,
	window CollectionWindow,
) (CollectionResult, error) {
	bounds := paginationBoundsForTarget(target)
	incidents, pages, truncated, err := c.listIncidents(ctx, target, window, bounds)
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{
		Incidents:           incidents,
		LifecycleEvents:     map[string][]LifecycleEvent{},
		RelatedChangeEvents: map[string][]ChangeEvent{},
		ObservedAt:          window.Until.UTC(),
		PagesFetched:        pages,
	}
	if truncated {
		result.Truncated = true
		result.Warnings = append(result.Warnings, ConfigWarning{
			ResourceClass: ConfigResourceClassIncident,
			Reason:        ConfigWarningTruncated,
		})
	}
	for _, incident := range incidents {
		logs, logPages, logTruncated, err := c.listLogEntries(ctx, incident.ID, target.LogEntryLimit, bounds)
		if err != nil {
			return CollectionResult{}, err
		}
		result.LifecycleEvents[incident.ID] = logs
		result.PagesFetched += logPages
		if logTruncated {
			result.Truncated = true
			result.Warnings = append(result.Warnings, ConfigWarning{
				ResourceClass: ConfigResourceClassLogEntry,
				ResourceID:    incident.ID,
				Reason:        ConfigWarningTruncated,
			})
		}
		changes, changePages, changeTruncated, err := c.listRelatedChangeEvents(ctx, incident.ID, target.ChangeEventLimit, bounds)
		result.PagesFetched += changePages
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
		if changeTruncated {
			result.Truncated = true
			result.Warnings = append(result.Warnings, ConfigWarning{
				ResourceClass: ConfigResourceClassRelatedChangeEvent,
				ResourceID:    incident.ID,
				Reason:        ConfigWarningTruncated,
			})
		}
	}
	return result, nil
}

func (c *HTTPClient) listIncidents(
	ctx context.Context,
	target TargetConfig,
	window CollectionWindow,
	bounds paginationBounds,
) ([]Incident, int, bool, error) {
	var all []incidentJSON
	pages, _, truncated, err := paginateOffset(ctx, bounds, func(ctx context.Context, offset int) (int, bool, error) {
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
		if offset > 0 {
			values.Set("offset", strconv.Itoa(offset))
		}
		var decoded incidentListResponse
		if err := c.getJSON(ctx, "/incidents", values, &decoded); err != nil {
			return 0, false, err
		}
		all = append(all, decoded.Incidents...)
		return len(decoded.Incidents), decoded.More, nil
	})
	if err != nil {
		return nil, pages, truncated, err
	}
	return normalizeIncidents(all), pages, truncated, nil
}

func (c *HTTPClient) listLogEntries(
	ctx context.Context,
	incidentID string,
	limit int,
	bounds paginationBounds,
) ([]LifecycleEvent, int, bool, error) {
	var all []logEntryJSON
	path := "/incidents/" + url.PathEscape(incidentID) + "/log_entries"
	pages, _, truncated, err := paginateOffset(ctx, bounds, func(ctx context.Context, offset int) (int, bool, error) {
		values := url.Values{}
		if limit > 0 {
			values.Set("limit", strconv.Itoa(limit))
		}
		if offset > 0 {
			values.Set("offset", strconv.Itoa(offset))
		}
		var decoded logEntryListResponse
		if err := c.getJSON(ctx, path, values, &decoded); err != nil {
			return 0, false, err
		}
		all = append(all, decoded.LogEntries...)
		return len(decoded.LogEntries), decoded.More, nil
	})
	if err != nil {
		return nil, pages, truncated, err
	}
	return normalizeLifecycleEvents(incidentID, all), pages, truncated, nil
}

func (c *HTTPClient) listRelatedChangeEvents(
	ctx context.Context,
	incidentID string,
	limit int,
	bounds paginationBounds,
) ([]ChangeEvent, int, bool, error) {
	var all []changeEventJSON
	path := "/incidents/" + url.PathEscape(incidentID) + "/related_change_events"
	pages, _, truncated, err := paginateOffset(ctx, bounds, func(ctx context.Context, offset int) (int, bool, error) {
		values := url.Values{}
		if limit > 0 {
			values.Set("limit", strconv.Itoa(limit))
		}
		if offset > 0 {
			values.Set("offset", strconv.Itoa(offset))
		}
		var decoded changeEventListResponse
		if err := c.getJSON(ctx, path, values, &decoded); err != nil {
			return 0, false, err
		}
		all = append(all, decoded.ChangeEvents...)
		return len(decoded.ChangeEvents), decoded.More, nil
	})
	if err != nil {
		return nil, pages, truncated, err
	}
	return normalizeChangeEvents(all), pages, truncated, nil
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
