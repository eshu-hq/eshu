// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"fmt"
	"strings"
)

func nugetPackagesLockDependencyVariables(document map[string]any, lang string) []map[string]any {
	targets, ok := document["dependencies"].(map[string]any)
	if !ok {
		return nil
	}
	rows := make([]map[string]any, 0)
	for _, targetFramework := range sortedMapKeys(targets) {
		dependencies, ok := targets[targetFramework].(map[string]any)
		if !ok {
			continue
		}
		chains := nugetLockDependencyChains(dependencies)
		for _, packageName := range sortedMapKeys(dependencies) {
			entry, ok := dependencies[packageName].(map[string]any)
			if !ok || strings.EqualFold(nugetLockString(entry, "type"), "Project") {
				continue
			}
			resolved := nugetLockString(entry, "resolved")
			if resolved == "" {
				continue
			}
			row := nugetLockDependencyRow(
				packageName,
				resolved,
				targetFramework,
				nugetLockString(entry, "type"),
				nugetLockString(entry, "requested"),
				len(rows)+1,
				lang,
				chains[packageName],
			)
			rows = append(rows, row)
		}
	}
	return rows
}

func nugetLockDependencyRow(
	name string,
	resolved string,
	targetFramework string,
	dependencyType string,
	requestedRange string,
	lineNumber int,
	lang string,
	dependencyPath []string,
) map[string]any {
	row := map[string]any{
		"name":             strings.TrimSpace(name),
		"line_number":      lineNumber,
		"value":            strings.TrimSpace(resolved),
		"section":          "packages.lock.json:" + strings.TrimSpace(targetFramework),
		"target_framework": strings.TrimSpace(targetFramework),
		"config_kind":      "dependency",
		"package_manager":  "nuget",
		"lockfile":         true,
		"lang":             lang,
	}
	if requestedRange != "" {
		row["requested_range"] = requestedRange
	}
	if dependencyType != "" {
		normalizedType := strings.ToLower(strings.TrimSpace(dependencyType))
		row["dependency_type"] = normalizedType
		switch normalizedType {
		case "direct":
			row["direct_dependency"] = true
		case "transitive", "centraltransitive":
			row["direct_dependency"] = false
		}
	}
	if len(dependencyPath) > 0 {
		row["dependency_path"] = append([]string(nil), dependencyPath...)
		row["dependency_depth"] = len(dependencyPath)
		row["direct_dependency"] = len(dependencyPath) == 1
	}
	return row
}

func nugetLockDependencyChains(dependencies map[string]any) map[string][]string {
	chains := make(map[string][]string, len(dependencies))
	for _, packageName := range sortedMapKeys(dependencies) {
		entry, ok := dependencies[packageName].(map[string]any)
		if !ok || !strings.EqualFold(nugetLockString(entry, "type"), "Direct") {
			continue
		}
		chain := []string{packageName}
		chains[packageName] = chain
		nugetLockWalkDependencyChains(packageName, chain, dependencies, chains, map[string]struct{}{packageName: {}})
	}
	return chains
}

func nugetLockWalkDependencyChains(
	packageName string,
	chain []string,
	dependencies map[string]any,
	chains map[string][]string,
	seen map[string]struct{},
) {
	entry, ok := dependencies[packageName].(map[string]any)
	if !ok {
		return
	}
	children, ok := entry["dependencies"].(map[string]any)
	if !ok {
		return
	}
	for _, childName := range sortedMapKeys(children) {
		if _, ok := seen[childName]; ok {
			continue
		}
		if _, ok := dependencies[childName].(map[string]any); !ok {
			continue
		}
		childChain := append(append([]string(nil), chain...), childName)
		if existing, ok := chains[childName]; !ok || len(childChain) < len(existing) {
			chains[childName] = childChain
		}
		seen[childName] = struct{}{}
		nugetLockWalkDependencyChains(childName, childChain, dependencies, chains, seen)
		delete(seen, childName)
	}
}

func nugetLockString(entry map[string]any, key string) string {
	raw, ok := entry[key]
	if !ok {
		return ""
	}
	value := strings.TrimSpace(fmt.Sprint(raw))
	if value == "" || value == "<nil>" {
		return ""
	}
	return value
}
