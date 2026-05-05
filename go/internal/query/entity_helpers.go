package query

import (
	"fmt"
	"strings"
)

// extractRelationships converts the Neo4j relationships collection to typed structs.
func extractRelationships(row map[string]any) []map[string]any {
	return extractCollection(row, "relationships", func(m map[string]any) (map[string]any, bool) {
		if relType := StringVal(m, "type"); relType != "" {
			return map[string]any{
				"type":        relType,
				"target_name": StringVal(m, "target_name"),
				"target_id":   StringVal(m, "target_id"),
			}, true
		}
		return nil, false
	})
}

// extractInstances converts the Neo4j instances collection to typed structs.
func extractInstances(row map[string]any) []map[string]any {
	return extractCollection(row, "instances", func(m map[string]any) (map[string]any, bool) {
		if instID := StringVal(m, "instance_id"); instID != "" {
			instance := map[string]any{
				"instance_id":                instID,
				"platform_name":              StringVal(m, "platform_name"),
				"platform_kind":              StringVal(m, "platform_kind"),
				"environment":                StringVal(m, "environment"),
				"materialization_confidence": floatVal(m, "materialization_confidence"),
				"materialization_provenance": StringSliceVal(m, "materialization_provenance"),
				"platform_confidence":        floatVal(m, "platform_confidence"),
				"platform_reason":            StringVal(m, "platform_reason"),
			}
			if platforms := mapSliceValue(m, "platforms"); len(platforms) > 0 {
				instance["platforms"] = platforms
			} else if platformName := StringVal(m, "platform_name"); platformName != "" {
				instance["platforms"] = []map[string]any{{
					"platform_name":       platformName,
					"platform_kind":       StringVal(m, "platform_kind"),
					"platform_confidence": floatVal(m, "platform_confidence"),
					"platform_reason":     StringVal(m, "platform_reason"),
				}}
			}
			return instance, true
		}
		return nil, false
	})
}

// extractCollection converts list-valued graph columns into typed API maps.
func extractCollection(row map[string]any, key string, transform func(map[string]any) (map[string]any, bool)) []map[string]any {
	raw, ok := row[key]
	if !ok || raw == nil {
		return []map[string]any{}
	}
	items, ok := raw.([]any)
	if !ok {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			if transformed, valid := transform(m); valid {
				result = append(result, transformed)
			}
		}
	}
	return result
}

// buildWorkloadStory creates a narrative summary of a workload's deployment.
func buildWorkloadStory(ctx map[string]any) string {
	if ctx == nil {
		return ""
	}
	name, kind, repoName := safeStr(ctx, "name"), safeStr(ctx, "kind"), safeStr(ctx, "repo_name")
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
		if hostnames := hostnameLabels(mapSliceValue(ctx, "hostnames")); len(hostnames) > 0 {
			story += " Public entrypoints: " + strings.Join(hostnames, ", ") + "."
		}
		if apiSurface := mapValue(ctx, "api_surface"); len(apiSurface) > 0 {
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
		env := safeStr(inst, "environment")
		platforms := platformTargetLabels(platformTargets(inst))
		if len(platforms) == 0 {
			story += " " + env
		} else {
			story += " " + env + " on " + strings.Join(platforms, ", ")
		}
		story += ";"
	}
	if hostnames := hostnameLabels(mapSliceValue(ctx, "hostnames")); len(hostnames) > 0 {
		story += " Public entrypoints: " + strings.Join(hostnames, ", ") + "."
	}
	if apiSurface := mapValue(ctx, "api_surface"); len(apiSurface) > 0 {
		story += fmt.Sprintf(
			" API surface exposes %d endpoint(s) across %d spec file(s).",
			IntVal(apiSurface, "endpoint_count"),
			IntVal(apiSurface, "spec_count"),
		)
		if docsRoutes := StringSliceVal(apiSurface, "docs_routes"); len(docsRoutes) > 0 {
			story += " Docs routes: " + strings.Join(docsRoutes, ", ") + "."
		}
	}
	if consumers := mapSliceValue(ctx, "consumer_repositories"); len(consumers) > 0 {
		story += fmt.Sprintf(" Observed %d consumer repos from graph and content evidence.", len(consumers))
	}
	if provisioningChains := mapSliceValue(ctx, "provisioning_source_chains"); len(provisioningChains) > 0 {
		story += fmt.Sprintf(" Provisioning chains span %d repo(s).", len(provisioningChains))
	}
	return story
}

// safeStr extracts a string from a map while filtering empty and nil values.
func safeStr(m map[string]any, key string) string {
	v := fmt.Sprintf("%v", m[key])
	if v == "" || v == "<nil>" {
		return ""
	}
	return v
}

func platformTargets(instance map[string]any) []map[string]any {
	platforms := mapSliceValue(instance, "platforms")
	if len(platforms) > 0 {
		return platforms
	}
	if platformName := StringVal(instance, "platform_name"); platformName != "" {
		return []map[string]any{{
			"platform_name":       platformName,
			"platform_kind":       StringVal(instance, "platform_kind"),
			"platform_confidence": floatVal(instance, "platform_confidence"),
			"platform_reason":     StringVal(instance, "platform_reason"),
		}}
	}
	return nil
}

func platformTargetLabels(platforms []map[string]any) []string {
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
