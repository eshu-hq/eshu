// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultConfigResourceLimit = 100

// CollectConfigEvidence fetches optional live PagerDuty configuration
// evidence for no-IaC fallback and freshness validation.
func (c *HTTPClient) CollectConfigEvidence(ctx context.Context, target TargetConfig) (ConfigCollectionResult, error) {
	limit := boundedConfigResourceLimit(target.ConfigResourceLimit)
	result := ConfigCollectionResult{ObservedAt: time.Now().UTC()}

	if len(target.AllowedServiceIDs) > 0 {
		if err := c.collectAllowedServices(ctx, target.AllowedServiceIDs, limit, &result); err != nil {
			return ConfigCollectionResult{}, err
		}
		return result, nil
	}
	services, err := c.listServices(ctx, limit)
	if err != nil {
		return ConfigCollectionResult{}, err
	}
	result.PagesFetched++
	result.Services = append(result.Services, normalizeConfigServices(services, ConfigMatchStateNotCompared)...)
	for _, service := range result.Services {
		if err := c.collectServiceIntegrations(ctx, service.ID, limit, ConfigMatchStateNotCompared, &result); err != nil {
			return ConfigCollectionResult{}, err
		}
	}
	return result, nil
}

func (c *HTTPClient) collectAllowedServices(
	ctx context.Context,
	serviceIDs []string,
	limit int,
	result *ConfigCollectionResult,
) error {
	for _, serviceID := range serviceIDs {
		trimmed := strings.TrimSpace(serviceID)
		if trimmed == "" {
			continue
		}
		service, err := c.getService(ctx, trimmed)
		result.PagesFetched++
		if err != nil {
			if retryableConfigError(err) {
				return err
			}
			if warning, ok := configWarningFromError(ConfigResourceClassService, trimmed, err); ok {
				result.Warnings = append(result.Warnings, warning)
				result.Partial = true
				continue
			}
			return err
		}
		normalized := normalizeConfigService(service, ConfigMatchStateNotCompared)
		if normalized.ID != "" {
			result.Services = append(result.Services, normalized)
		}
		if err := c.collectServiceIntegrations(ctx, trimmed, limit, ConfigMatchStateNotCompared, result); err != nil {
			return err
		}
	}
	return nil
}

func (c *HTTPClient) collectServiceIntegrations(
	ctx context.Context,
	serviceID string,
	limit int,
	matchState string,
	result *ConfigCollectionResult,
) error {
	integrations, err := c.listServiceIntegrations(ctx, serviceID, limit)
	result.PagesFetched++
	if err != nil {
		if retryableConfigError(err) {
			return err
		}
		if warning, ok := configWarningFromError(ConfigResourceClassServiceIntegration, serviceID, err); ok {
			result.Warnings = append(result.Warnings, warning)
			result.Partial = true
			return nil
		}
		return err
	}
	normalized, redactions := normalizeConfigIntegrations(serviceID, integrations, matchState)
	result.Integrations = append(result.Integrations, normalized...)
	result.Redactions += redactions
	return nil
}

func (c *HTTPClient) listServices(ctx context.Context, limit int) ([]serviceJSON, error) {
	values := url.Values{}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	var decoded serviceListResponse
	if err := c.getJSON(ctx, "/services", values, &decoded); err != nil {
		return nil, err
	}
	return decoded.Services, nil
}

func (c *HTTPClient) getService(ctx context.Context, serviceID string) (serviceJSON, error) {
	var decoded serviceResponse
	path := "/services/" + url.PathEscape(serviceID)
	if err := c.getJSON(ctx, path, nil, &decoded); err != nil {
		return serviceJSON{}, err
	}
	return decoded.Service, nil
}

func (c *HTTPClient) listServiceIntegrations(
	ctx context.Context,
	serviceID string,
	limit int,
) ([]integrationJSON, error) {
	values := url.Values{}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	var decoded integrationListResponse
	path := "/services/" + url.PathEscape(serviceID) + "/integrations"
	if err := c.getJSON(ctx, path, values, &decoded); err != nil {
		return nil, err
	}
	return decoded.Integrations, nil
}

func boundedConfigResourceLimit(limit int) int {
	if limit <= 0 || limit > defaultConfigResourceLimit {
		return defaultConfigResourceLimit
	}
	return limit
}

func configWarningFromError(resourceClass string, resourceID string, err error) (ConfigWarning, bool) {
	var pdErr PagerDutyError
	if !errors.As(err, &pdErr) {
		return ConfigWarning{}, false
	}
	reason := ""
	switch pdErr.StatusCode {
	case http.StatusForbidden, http.StatusUnauthorized:
		reason = ConfigWarningPermissionHidden
	case http.StatusNotFound:
		reason = ConfigWarningMissing
	case http.StatusTooManyRequests:
		reason = FailureRateLimited
	default:
		if pdErr.StatusCode >= 400 && pdErr.StatusCode < 500 {
			reason = ConfigWarningUnsupported
		}
	}
	if reason == "" {
		return ConfigWarning{}, false
	}
	return ConfigWarning{
		ResourceClass: resourceClass,
		ResourceID:    resourceID,
		Reason:        reason,
	}, true
}

func retryableConfigError(err error) bool {
	var pdErr PagerDutyError
	if !errors.As(err, &pdErr) {
		return true
	}
	if pdErr.StatusCode == http.StatusTooManyRequests ||
		pdErr.StatusCode == http.StatusRequestTimeout ||
		pdErr.StatusCode == http.StatusConflict ||
		pdErr.StatusCode == http.StatusTooEarly {
		return true
	}
	return pdErr.StatusCode >= 500
}
