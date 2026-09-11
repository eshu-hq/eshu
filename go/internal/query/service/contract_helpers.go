// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func buildServiceEntrypoints(workloadContext map[string]any, evidence QueryEvidence) []map[string]any {
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
		if querycontract.StringVal(entrypoints[i], "type") != querycontract.StringVal(entrypoints[j], "type") {
			return querycontract.StringVal(entrypoints[i], "type") < querycontract.StringVal(entrypoints[j], "type")
		}
		return querycontract.StringVal(entrypoints[i], "target") < querycontract.StringVal(entrypoints[j], "target")
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
		entryEnv := querycontract.StringVal(entrypoint, "environment")
		match := matchingRuntimeInstance(instances, entryEnv)
		if len(match) == 0 {
			continue
		}
		pathType := "entrypoint_to_runtime"
		if querycontract.StringVal(entrypoint, "type") == "hostname" {
			pathType = "hostname_to_runtime"
		}
		if querycontract.StringVal(entrypoint, "type") == "docs_route" {
			pathType = "docs_route_to_runtime"
		}
		paths = append(paths, map[string]any{
			"path_type":     pathType,
			"from_type":     querycontract.StringVal(entrypoint, "type"),
			"from":          querycontract.StringVal(entrypoint, "target"),
			"to_type":       "runtime_platform",
			"to":            querycontract.StringVal(match, "platform_name"),
			"platform_kind": querycontract.StringVal(match, "platform_kind"),
			"environment":   querycontract.StringVal(match, "environment"),
			"reason":        querycontract.StringVal(entrypoint, "reason"),
			"visibility":    querycontract.StringVal(entrypoint, "visibility"),
		})
	}
	sort.Slice(paths, func(i, j int) bool {
		if querycontract.StringVal(paths[i], "path_type") != querycontract.StringVal(paths[j], "path_type") {
			return querycontract.StringVal(paths[i], "path_type") < querycontract.StringVal(paths[j], "path_type")
		}
		return querycontract.StringVal(paths[i], "from") < querycontract.StringVal(paths[j], "from")
	})
	return paths
}

func BuildGraphDependents(candidates []impacttrace.ProvisioningRepositoryCandidate) []map[string]any {
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
	// Two distinct repositories can share a display name (#5720, same class
	// as #5644), so a comparator that leaves that tie unresolved lets
	// repeated calls over unchanged data return those tied entries in a
	// different relative order. repo_id is unique per candidate, so it is
	// the final tiebreaker that makes this a total order.
	sort.Slice(dependents, func(i, j int) bool {
		if left, right := querycontract.StringVal(dependents[i], "repository"), querycontract.StringVal(dependents[j], "repository"); left != right {
			return left < right
		}
		return querycontract.StringVal(dependents[i], "repo_id") < querycontract.StringVal(dependents[j], "repo_id")
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
		instanceEnvironment := querycontract.StringVal(instance, "environment")
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
