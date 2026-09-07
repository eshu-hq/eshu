// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"fmt"
	"sort"
	"strings"
)

// Story collection helpers shared by the query root and the handler-family
// subpackages (#6060 lane B2): bounded-collection metadata, search-limit
// consts, and the workload-story builders. Split from story_row_helpers.go
// to honor the 500-line file cap; the scalar row helpers stay there.
// BoundedCollectionMetadata builds the standard limit/returned/observed/
// truncation metadata block for a bounded collection read.
func BoundedCollectionMetadata(limit, queryLimit, returned, observed int, truncated bool, ordering []string) map[string]any {
	return map[string]any{
		"limit":                         limit,
		"query_sentinel_limit":          queryLimit,
		"returned_count":                returned,
		"observed_count":                observed,
		"observed_count_is_lower_bound": truncated,
		"truncated":                     truncated,
		"ordering":                      ordering,
	}
}

// RepositorySemanticEntityLimit caps semantic entity reads per repository.
const RepositorySemanticEntityLimit = 5000

// DefaultIndirectEvidenceSearchLimit is the default indirect-evidence search
// depth callers get when the request leaves max_depth unset. MaxIndirectEvidenceSearchLimit
// is the hard ceiling the handler clamps into. Both live here because the
// impact/ deployment-trace tests pin them from outside the query root;
// previously package-level consts in deployment_trace_support_helpers.go.
// See #6060.
const DefaultIndirectEvidenceSearchLimit = 25

// MaxIndirectEvidenceSearchLimit caps indirect-evidence search depth. See
// DefaultIndirectEvidenceSearchLimit.
const MaxIndirectEvidenceSearchLimit = 100

// CanonicalServiceName returns the workload context's canonical name,
// falling back to the requested service name.
func CanonicalServiceName(requestedServiceName string, workloadContext map[string]any) string {
	if canonicalName := SafeStr(workloadContext, "name"); canonicalName != "" {
		return canonicalName
	}
	return strings.TrimSpace(requestedServiceName)
}

// FirstNonEmpty returns the first non-blank value unchanged (unlike
// FirstNonEmptyString, it does not trim the returned value).
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// IsK8sResourceKind reports whether entity is a K8sResource of the given
// kind (case-insensitive, whitespace-tolerant).
func IsK8sResourceKind(entity EntityContent, kind string) bool {
	if entity.EntityType != "K8sResource" {
		return false
	}
	value, _ := entity.Metadata["kind"].(string)
	return strings.EqualFold(strings.TrimSpace(value), kind)
}

// PlatformTargets returns an instance's platform targets: the projected
// platforms list when present, else a single target synthesized from the
// platform_name/kind/confidence/reason columns.
func PlatformTargets(instance map[string]any) []map[string]any {
	platforms := MapSliceValue(instance, "platforms")
	if len(platforms) > 0 {
		return platforms
	}
	if platformName := StringVal(instance, "platform_name"); platformName != "" {
		return []map[string]any{{
			"platform_name":       platformName,
			"platform_kind":       StringVal(instance, "platform_kind"),
			"platform_confidence": FloatVal(instance, "platform_confidence"),
			"platform_reason":     StringVal(instance, "platform_reason"),
		}}
	}
	return nil
}

// HostnameLabels returns the sorted distinct hostnames across rows.
func HostnameLabels(rows []map[string]any) []string {
	if len(rows) == 0 {
		return nil
	}
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		hostname := StringVal(row, "hostname")
		if hostname == "" {
			continue
		}
		values = append(values, hostname)
	}
	return UniqueSortedStrings(values)
}

// PlatformTargetLabels renders one "name (kind)" label per platform.
func PlatformTargetLabels(platforms []map[string]any) []string {
	labels := make([]string, 0, len(platforms))
	for _, platform := range platforms {
		name := StringVal(platform, "platform_name")
		if name == "" {
			continue
		}
		if kind := StringVal(platform, "platform_kind"); kind != "" {
			name += " (" + kind + ")"
		}
		labels = append(labels, name)
	}
	return labels
}

// BuildWorkloadStory creates a narrative summary of a workload's deployment.
func BuildWorkloadStory(ctx map[string]any) string {
	apiSurface := MapValue(ctx, "api_surface")
	return BuildWorkloadStoryWithAPISurface(ctx, apiSurface, len(apiSurface) > 0)
}

// BuildWorkloadStoryWithAPISurface keeps narrative counts aligned with the
// normalized service-story API surface used by structured response fields.
func BuildWorkloadStoryWithAPISurface(ctx map[string]any, apiSurface map[string]any, hasAPISurface bool) string {
	if ctx == nil {
		return ""
	}
	name, kind, repoName := SafeStr(ctx, "name"), SafeStr(ctx, "kind"), SafeStr(ctx, "repo_name")
	story := "Workload " + name
	if kind != "" {
		story += " (kind: " + kind + ")"
	}
	if repoName != "" {
		story += " is defined in repository " + repoName + "."
	} else {
		story += " has no linked repository."
	}
	instances, ok := ctx["instances"].([]map[string]any)
	if !ok || len(instances) == 0 {
		story += " No materialized workload instances found."
		if observedEnvironments := StringSliceVal(ctx, "observed_config_environments"); len(observedEnvironments) > 0 {
			story += " Observed config environments: " + strings.Join(observedEnvironments, ", ") + "."
		}
		if hostnames := HostnameLabels(MapSliceValue(ctx, "hostnames")); len(hostnames) > 0 {
			story += " Public entrypoints: " + strings.Join(hostnames, ", ") + "."
		}
		if hasAPISurface {
			story += fmt.Sprintf(
				" API surface exposes %d endpoint(s) across %d spec file(s).",
				IntVal(apiSurface, "endpoint_count"),
				IntVal(apiSurface, "spec_count"),
			)
			if docsRoutes := StringSliceVal(apiSurface, "docs_routes"); len(docsRoutes) > 0 {
				story += " Docs routes: " + strings.Join(docsRoutes, ", ") + "."
			}
		}
		return story
	}
	instCount := "1 instance"
	if len(instances) > 1 {
		instCount = fmt.Sprintf("%d instances", len(instances))
	}
	story += " It is deployed as " + instCount + ":"
	for _, inst := range instances {
		env := SafeStr(inst, "environment")
		platforms := PlatformTargetLabels(PlatformTargets(inst))
		if len(platforms) == 0 {
			story += " " + env
		} else {
			story += " " + env + " on " + strings.Join(platforms, ", ")
		}
		story += ";"
	}
	if hostnames := HostnameLabels(MapSliceValue(ctx, "hostnames")); len(hostnames) > 0 {
		story += " Public entrypoints: " + strings.Join(hostnames, ", ") + "."
	}
	if hasAPISurface {
		story += fmt.Sprintf(
			" API surface exposes %d endpoint(s) across %d spec file(s).",
			IntVal(apiSurface, "endpoint_count"),
			IntVal(apiSurface, "spec_count"),
		)
		if docsRoutes := StringSliceVal(apiSurface, "docs_routes"); len(docsRoutes) > 0 {
			story += " Docs routes: " + strings.Join(docsRoutes, ", ") + "."
		}
	}
	if consumers := MapSliceValue(ctx, "consumer_repositories"); len(consumers) > 0 {
		story += fmt.Sprintf(" Observed %d consumer repos from graph and content evidence.", len(consumers))
	}
	if provisioningChains := MapSliceValue(ctx, "provisioning_source_chains"); len(provisioningChains) > 0 {
		story += fmt.Sprintf(" Provisioning chains span %d repo(s).", len(provisioningChains))
	}
	return story
}

// CompactStringMap drops empty string and empty []string values from value
// in place and returns it. It backs entity-map and investigation response
// shaping in the impact/ subpackage.
func CompactStringMap(value map[string]any) map[string]any {
	for key, raw := range value {
		switch typed := raw.(type) {
		case string:
			if typed == "" {
				delete(value, key)
			}
		case []string:
			if len(typed) == 0 {
				delete(value, key)
			}
		}
	}
	return value
}

// FilterEmptyStrings drops blank values (trimming the survivors),
// returning nil for an empty input.
func FilterEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	items := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

// StringSliceFromAny extracts a trimmed []string from a []string, a []any of
// strings, or anything else (nil).
func StringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return FilterEmptyStrings(typed)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				continue
			}
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				items = append(items, trimmed)
			}
		}
		return items
	default:
		return nil
	}
}

// NonEmptyStrings drops empty values, preserving order.
func NonEmptyStrings(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

// StringSliceMapValue extracts a []string from a map value, accepting a
// []string (blank entries dropped) or a []any of non-empty strings; anything
// else yields nil.
func StringSliceMapValue(value map[string]any, key string) []string {
	if len(value) == 0 {
		return nil
	}
	raw, ok := value[key]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return NonEmptyStrings(typed)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if ok && text != "" {
				items = append(items, text)
			}
		}
		return items
	default:
		return nil
	}
}

// CloneAnyMap returns a shallow copy of src (empty, never nil).
func CloneAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

// DistinctSortedInstanceField returns the sorted distinct values of key
// across instances, expanding platform_name/platform_kind through the
// instance's platform targets.
func DistinctSortedInstanceField(instances []map[string]any, key string) []string {
	values := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		if key == "platform_name" || key == "platform_kind" {
			for _, platform := range PlatformTargets(instance) {
				value := SafeStr(platform, key)
				if value == "" {
					continue
				}
				values[value] = struct{}{}
			}
			continue
		}
		value := SafeStr(instance, key)
		if value == "" {
			continue
		}
		values[value] = struct{}{}
	}

	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// MetadataStringSlice extracts a cleaned []string from a metadata map,
// accepting a []string, a []any of strings, or a single comma-separated
// string; anything else yields nil.
func MetadataStringSlice(metadata map[string]any, key string) []string {
	values, ok := metadata[key]
	if !ok {
		return nil
	}

	switch typed := values.(type) {
	case []string:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := CleanMetadataString(item); value != "" {
				items = append(items, value)
			}
		}
		return items
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			raw, ok := item.(string)
			if !ok {
				continue
			}
			if value := CleanMetadataString(raw); value != "" {
				items = append(items, value)
			}
		}
		return items
	case string:
		items := strings.Split(typed, ",")
		result := make([]string, 0, len(items))
		for _, item := range items {
			if value := CleanMetadataString(item); value != "" {
				result = append(result, value)
			}
		}
		return result
	default:
		return nil
	}
}
