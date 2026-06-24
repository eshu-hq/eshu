// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azureruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
)

const (
	liveARMFallbackExtensionSchemaVersion = "azure-arm-fallback-2026-06-16"
	maxLiveARMFallbackExtensionJSONBytes  = 64 * 1024
)

// LiveARMFallbackClient is the read-only ARM transport consumed by
// LiveProviderFactory for optional fallback enrichment. It intentionally exposes
// only GET-by-resource behavior so command wiring cannot reach provider
// registration, create, update, or delete operations through this seam.
type LiveARMFallbackClient interface {
	// GetResource reads one allowlisted ARM resource with a fixed API version.
	GetResource(context.Context, LiveARMFallbackRequest) (LiveARMFallbackResponse, error)
}

// LiveARMFallbackRequest is one bounded ARM fallback GET. ResourceID is used
// only by the transport; callers must not copy it into telemetry labels, spans,
// logs, status, or warning messages.
type LiveARMFallbackRequest struct {
	ResourceID   string
	ResourceType string
	APIVersion   string
}

// LiveARMFallbackResponse carries sanitized candidate extension fields returned
// by an ARM fallback client. LiveProviderFactory applies the configured field
// allowlist and byte bound before the parent azurecloud collector redacts and
// persists the payload.
type LiveARMFallbackResponse struct {
	Extension map[string]any
}

// LiveARMFallbackRule allows one exact resource type to be enriched through a
// read-only ARM GET using a fixed API version and a bounded field list.
type LiveARMFallbackRule struct {
	ResourceType    string
	APIVersion      string
	ExtensionFields []string
}

type liveARMFallbackRule struct {
	resourceType string
	apiVersion   string
	fields       []string
	fieldSet     map[string]struct{}
}

func normalizeLiveARMFallbackRules(
	rules []LiveARMFallbackRule,
) (map[string]liveARMFallbackRule, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	out := make(map[string]liveARMFallbackRule, len(rules))
	for _, rule := range rules {
		resourceType := normalizedARMFallbackResourceType(rule.ResourceType)
		apiVersion := strings.TrimSpace(rule.APIVersion)
		fields := normalizedARMFallbackFields(rule.ExtensionFields)
		switch {
		case resourceType == "":
			return nil, fmt.Errorf("azure live ARM fallback rule requires resource_type")
		case apiVersion == "":
			return nil, fmt.Errorf("azure live ARM fallback rule for %q requires api_version", resourceType)
		case len(fields) == 0:
			return nil, fmt.Errorf("azure live ARM fallback rule for %q requires extension fields", resourceType)
		}
		if _, exists := out[resourceType]; exists {
			return nil, fmt.Errorf("duplicate azure live ARM fallback rule for %q", resourceType)
		}
		fieldSet := make(map[string]struct{}, len(fields))
		for _, field := range fields {
			fieldSet[field] = struct{}{}
		}
		out[resourceType] = liveARMFallbackRule{
			resourceType: resourceType,
			apiVersion:   apiVersion,
			fields:       fields,
			fieldSet:     fieldSet,
		}
	}
	return out, nil
}

func normalizedARMFallbackResourceType(resourceType string) string {
	resourceType = strings.TrimSpace(resourceType)
	resourceType = strings.Trim(resourceType, "/")
	return strings.ToLower(resourceType)
}

func normalizedARMFallbackFields(fields []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func normalizedLiveARMFallbackMaxExtensionBytes(value int) int {
	if value <= 0 {
		return maxLiveARMFallbackExtensionJSONBytes
	}
	return value
}

func (p *liveResourceGraphProvider) applyARMFallbacks(
	ctx context.Context,
	page *azurecloud.ResourceGraphPage,
) error {
	if p.armClient == nil || len(p.armRules) == 0 || page == nil {
		return nil
	}
	for index := range page.Rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		row := &page.Rows[index]
		rule, ok := p.armRules[normalizedARMFallbackResourceType(row.Type)]
		if !ok {
			p.mergeAccess(azurecloud.ScopeAccess{
				Partial: true,
				Reason:  azurecloud.WarningFallbackSkipped,
				Message: "arm fallback skipped for a non-allowlisted resource family",
			})
			continue
		}
		resourceID := strings.TrimSpace(row.ID)
		if _, err := azurecloud.ParseARMIdentity(resourceID); err != nil {
			continue
		}
		response, err := p.queryARMFallback(ctx, LiveARMFallbackRequest{
			ResourceID:   resourceID,
			ResourceType: rule.resourceType,
			APIVersion:   rule.apiVersion,
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			p.mergeAccess(liveARMFallbackAccess(err))
			continue
		}
		fallback, ok := liveARMFallbackExtension(
			response.Extension,
			rule,
			p.armMaxExtensionBytes,
		)
		if !ok {
			p.mergeAccess(azurecloud.ScopeAccess{
				Partial: true,
				Reason:  azurecloud.WarningRedaction,
				Message: "arm fallback extension exceeded the configured persistence bound",
			})
			continue
		}
		if row.Properties == nil {
			row.Properties = map[string]any{}
		}
		row.Properties["armFallback"] = fallback
	}
	return nil
}

func (p *liveResourceGraphProvider) queryARMFallback(
	ctx context.Context,
	request LiveARMFallbackRequest,
) (LiveARMFallbackResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, p.requestTimeout)
	defer cancel()
	return p.armClient.GetResource(callCtx, request)
}

func liveARMFallbackExtension(
	extension map[string]any,
	rule liveARMFallbackRule,
	maxBytes int,
) (map[string]any, bool) {
	selected := make(map[string]any, len(rule.fieldSet))
	for _, field := range rule.fields {
		if value, ok := extension[field]; ok {
			selected[field] = value
		}
	}
	wrapper := map[string]any{
		"schema_version": liveARMFallbackExtensionSchemaVersion,
		"api_version":    rule.apiVersion,
		"data":           selected,
	}
	raw, err := json.Marshal(wrapper)
	if err != nil {
		return nil, false
	}
	return wrapper, len(raw) <= maxBytes
}

func liveARMFallbackAccess(err error) azurecloud.ScopeAccess {
	liveErr, ok := classifyLiveProviderError(err)
	if !ok {
		if errors.Is(err, context.DeadlineExceeded) {
			return azurecloud.ScopeAccess{
				Partial: true,
				Reason:  azurecloud.WarningStale,
				Message: "arm fallback timed out; fallback evidence may be stale",
			}
		}
		return azurecloud.ScopeAccess{
			Partial: true,
			Reason:  azurecloud.WarningUnsupported,
			Message: "arm fallback failed for an unsupported resource family",
		}
	}
	switch liveErr.kind {
	case liveProviderErrorThrottled:
		return azurecloud.ScopeAccess{
			Partial: true,
			Reason:  azurecloud.WarningThrottled,
			Message: "arm fallback throttled; fallback evidence may be partial",
		}
	case liveProviderErrorPermissionHidden:
		return azurecloud.ScopeAccess{
			Partial: true,
			Reason:  azurecloud.WarningPermissionHidden,
			Message: "arm fallback resource was hidden from the read-only identity",
		}
	case liveProviderErrorSkipTokenExpired, liveProviderErrorTokenExpired:
		return azurecloud.ScopeAccess{
			Partial: true,
			Reason:  azurecloud.WarningStale,
			Message: "arm fallback token expired; rerun the scan",
		}
	case liveProviderErrorUnsupported:
		return azurecloud.ScopeAccess{
			Partial: true,
			Reason:  azurecloud.WarningUnsupported,
			Message: "arm fallback does not expose the requested resource family",
		}
	default:
		return azurecloud.ScopeAccess{
			Partial: true,
			Reason:  azurecloud.WarningUnsupported,
			Message: "arm fallback failed for an unsupported resource family",
		}
	}
}
