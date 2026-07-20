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
// evidence for no-IaC fallback and freshness validation. The services and
// service-integrations list endpoints each follow PagerDuty's classic offset
// pagination up to the target's configured page/record bound;
// ConfigCollectionResult.Truncated and a ConfigWarningTruncated coverage
// warning are set only when that bound was hit while the provider still had
// more pages ("more":true), never when pagination exhausted naturally.
func (c *HTTPClient) CollectConfigEvidence(ctx context.Context, target TargetConfig) (ConfigCollectionResult, error) {
	limit := boundedConfigResourceLimit(target.ConfigResourceLimit)
	bounds := paginationBoundsForTarget(target)
	result := ConfigCollectionResult{ObservedAt: time.Now().UTC()}

	if len(target.AllowedServiceIDs) > 0 {
		if err := c.collectAllowedServices(ctx, target.AllowedServiceIDs, limit, bounds, &result); err != nil {
			return ConfigCollectionResult{}, err
		}
		return result, nil
	}
	services, pages, truncated, err := c.listServices(ctx, limit, bounds)
	if err != nil {
		return ConfigCollectionResult{}, err
	}
	result.PagesFetched += pages
	if truncated {
		result.Truncated = true
		result.Warnings = append(result.Warnings, ConfigWarning{
			ResourceClass: ConfigResourceClassService,
			Reason:        ConfigWarningTruncated,
		})
	}
	result.Services = append(result.Services, normalizeConfigServices(services, ConfigMatchStateNotCompared)...)
	for _, service := range result.Services {
		if err := c.collectServiceIntegrations(ctx, service.ID, limit, bounds, ConfigMatchStateNotCompared, &result); err != nil {
			return ConfigCollectionResult{}, err
		}
	}
	return result, nil
}

func (c *HTTPClient) collectAllowedServices(
	ctx context.Context,
	serviceIDs []string,
	limit int,
	bounds paginationBounds,
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
		if err := c.collectServiceIntegrations(ctx, trimmed, limit, bounds, ConfigMatchStateNotCompared, result); err != nil {
			return err
		}
	}
	return nil
}

func (c *HTTPClient) collectServiceIntegrations(
	ctx context.Context,
	serviceID string,
	limit int,
	bounds paginationBounds,
	matchState string,
	result *ConfigCollectionResult,
) error {
	integrations, pages, truncated, err := c.listServiceIntegrations(ctx, serviceID, limit, bounds)
	result.PagesFetched += pages
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
	if truncated {
		result.Truncated = true
		result.Warnings = append(result.Warnings, ConfigWarning{
			ResourceClass: ConfigResourceClassServiceIntegration,
			ResourceID:    serviceID,
			Reason:        ConfigWarningTruncated,
		})
	}
	normalized, redactions := normalizeConfigIntegrations(serviceID, integrations, matchState)
	result.Integrations = append(result.Integrations, normalized...)
	result.Redactions += redactions
	return nil
}

func (c *HTTPClient) listServices(
	ctx context.Context,
	limit int,
	bounds paginationBounds,
) ([]serviceJSON, int, bool, error) {
	var all []serviceJSON
	pages, records, truncated, err := paginateOffset(ctx, bounds, func(ctx context.Context, offset int, requestLimit int) (int, bool, error) {
		values := url.Values{}
		if effective := paginationRequestLimit(limit, requestLimit); effective > 0 {
			values.Set("limit", strconv.Itoa(effective))
		}
		if offset > 0 {
			values.Set("offset", strconv.Itoa(offset))
		}
		var decoded serviceListResponse
		if err := c.getJSON(ctx, "/services", values, &decoded); err != nil {
			return 0, false, err
		}
		all = append(all, decoded.Services...)
		return len(decoded.Services), decoded.More, nil
	})
	if err == nil && records < len(all) {
		all = all[:records]
	}
	return all, pages, truncated, err
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
	bounds paginationBounds,
) ([]integrationJSON, int, bool, error) {
	var all []integrationJSON
	path := "/services/" + url.PathEscape(serviceID) + "/integrations"
	pages, records, truncated, err := paginateOffset(ctx, bounds, func(ctx context.Context, offset int, requestLimit int) (int, bool, error) {
		values := url.Values{}
		if effective := paginationRequestLimit(limit, requestLimit); effective > 0 {
			values.Set("limit", strconv.Itoa(effective))
		}
		if offset > 0 {
			values.Set("offset", strconv.Itoa(offset))
		}
		var decoded integrationListResponse
		if err := c.getJSON(ctx, path, values, &decoded); err != nil {
			return 0, false, err
		}
		all = append(all, decoded.Integrations...)
		return len(decoded.Integrations), decoded.More, nil
	})
	if err == nil && records < len(all) {
		all = all[:records]
	}
	return all, pages, truncated, err
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
