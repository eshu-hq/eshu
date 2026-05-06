package query

import "sort"

func buildServiceEntrypoints(workloadContext map[string]any, evidence ServiceQueryEvidence) []map[string]any {
	entrypoints := make([]map[string]any, 0, len(evidence.DocsRoutes)+len(evidence.Hostnames))
	for _, row := range evidence.DocsRoutes {
		entrypoints = append(entrypoints, map[string]any{
			"type":          "docs_route",
			"target":        row.Route,
			"environment":   inferDocsRouteEnvironment(row.Route, row.RelativePath, workloadContext),
			"visibility":    "internal",
			"relative_path": row.RelativePath,
			"reason":        row.Reason,
		})
	}
	for _, row := range evidence.Hostnames {
		entrypoints = append(entrypoints, map[string]any{
			"type":          "hostname",
			"target":        row.Hostname,
			"environment":   row.Environment,
			"visibility":    "public",
			"relative_path": row.RelativePath,
			"reason":        row.Reason,
		})
	}
	sort.Slice(entrypoints, func(i, j int) bool {
		if StringVal(entrypoints[i], "type") != StringVal(entrypoints[j], "type") {
			return StringVal(entrypoints[i], "type") < StringVal(entrypoints[j], "type")
		}
		return StringVal(entrypoints[i], "target") < StringVal(entrypoints[j], "target")
	})
	return entrypoints
}

func buildServiceNetworkPaths(workloadContext map[string]any, entrypoints []map[string]any) []map[string]any {
	instances, _ := workloadContext["instances"].([]map[string]any)
	if len(entrypoints) == 0 || len(instances) == 0 {
		return nil
	}

	paths := make([]map[string]any, 0, len(entrypoints))
	for _, entrypoint := range entrypoints {
		entryEnv := StringVal(entrypoint, "environment")
		match := matchingRuntimeInstance(instances, entryEnv)
		if len(match) == 0 {
			continue
		}
		pathType := "entrypoint_to_runtime"
		if StringVal(entrypoint, "type") == "hostname" {
			pathType = "hostname_to_runtime"
		}
		if StringVal(entrypoint, "type") == "docs_route" {
			pathType = "docs_route_to_runtime"
		}
		paths = append(paths, map[string]any{
			"path_type":     pathType,
			"from_type":     StringVal(entrypoint, "type"),
			"from":          StringVal(entrypoint, "target"),
			"to_type":       "runtime_platform",
			"to":            StringVal(match, "platform_name"),
			"platform_kind": StringVal(match, "platform_kind"),
			"environment":   StringVal(match, "environment"),
			"reason":        StringVal(entrypoint, "reason"),
			"visibility":    StringVal(entrypoint, "visibility"),
		})
	}
	sort.Slice(paths, func(i, j int) bool {
		if StringVal(paths[i], "path_type") != StringVal(paths[j], "path_type") {
			return StringVal(paths[i], "path_type") < StringVal(paths[j], "path_type")
		}
		return StringVal(paths[i], "from") < StringVal(paths[j], "from")
	})
	return paths
}

func buildGraphDependents(candidates []provisioningRepositoryCandidate) []map[string]any {
	if len(candidates) == 0 {
		return nil
	}
	dependents := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		dependents = append(dependents, map[string]any{
			"repository":           candidate.RepoName,
			"repo_id":              candidate.RepoID,
			"relationship_types":   append([]string(nil), candidate.RelationshipTypes...),
			"relationship_reasons": append([]string(nil), candidate.RelationshipReasons...),
		})
	}
	sort.Slice(dependents, func(i, j int) bool {
		return StringVal(dependents[i], "repository") < StringVal(dependents[j], "repository")
	})
	return dependents
}

func inferDocsRouteEnvironment(route string, relativePath string, workloadContext map[string]any) string {
	values := detectEnvironmentAliases(route + " " + relativePath)
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func matchingRuntimeInstance(instances []map[string]any, environment string) map[string]any {
	if environment == "" {
		return nil
	}
	normalizedEnvironment := canonicalEnvironmentAlias(environment)
	for _, instance := range instances {
		instanceEnvironment := StringVal(instance, "environment")
		if instanceEnvironment == environment {
			return instance
		}
		if normalizedEnvironment != "" && canonicalEnvironmentAlias(instanceEnvironment) == normalizedEnvironment {
			return instance
		}
	}
	return nil
}

func canonicalEnvironmentAlias(environment string) string {
	values := detectEnvironmentAliases(environment)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
