// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"fmt"
	"sort"
	"strings"
)

// packageLockDependencyVariables converts one package-lock.json document into
// dependency rows keyed by path (npm v2+ lockfile format). Row order stays
// alphabetical by path for deterministic output; line_number carries each
// path's real source line from the "packages" object (issue #5329) rather
// than the row's position in that alphabetical order, and is omitted when
// source is unavailable or the line lookup fails.
func packageLockDependencyVariables(document map[string]any, source []byte, lang string) []map[string]any {
	packages, ok := document["packages"].(map[string]any)
	if !ok {
		return packageLockV1DependencyVariables(document, source, lang)
	}
	chains := packageLockDependencyChains(packages)
	rootScopes := packageLockRootDependencyScopes(packages[""])

	paths := make([]string, 0, len(packages))
	for path := range packages {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	lines := lockfileSectionLines(source, "packages")

	rows := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		name := packageLockPackageName(path)
		if name == "" {
			continue
		}
		entry, ok := packages[path].(map[string]any)
		if !ok {
			continue
		}
		version := strings.TrimSpace(fmt.Sprint(entry["version"]))
		if version == "" || version == "<nil>" {
			continue
		}
		scope := packageLockDependencyScope(entry, chains[path], rootScopes)
		row := packageLockDependencyRow(name, version, lang, chains[path], scope)
		if line, ok := lines[path]; ok {
			row["line_number"] = line
		}
		rows = append(rows, row)
	}
	return rows
}

// packageLockV1DependencyVariables handles the legacy npm v1 lockfile shape,
// a flat "dependencies" object keyed by package name. line_number is that
// key's real source line, omitted when unavailable.
func packageLockV1DependencyVariables(document map[string]any, source []byte, lang string) []map[string]any {
	dependencies, ok := document["dependencies"].(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(dependencies))
	for name := range dependencies {
		names = append(names, name)
	}
	sort.Strings(names)

	lines := lockfileSectionLines(source, "dependencies")

	rows := make([]map[string]any, 0, len(names))
	for _, name := range names {
		entry, ok := dependencies[name].(map[string]any)
		if !ok {
			continue
		}
		version := strings.TrimSpace(fmt.Sprint(entry["version"]))
		if version == "" || version == "<nil>" {
			continue
		}
		row := packageLockDependencyRow(name, version, lang, nil, packageLockLegacyDependencyScope(entry))
		if line, ok := lines[name]; ok {
			row["line_number"] = line
		}
		rows = append(rows, row)
	}
	return rows
}

func packageLockPackageName(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return ""
	}
	const marker = "node_modules/"
	index := strings.LastIndex(path, marker)
	if index < 0 {
		return ""
	}
	name := path[index+len(marker):]
	if strings.HasPrefix(name, "@") {
		parts := strings.Split(name, "/")
		if len(parts) < 2 {
			return ""
		}
		return parts[0] + "/" + parts[1]
	}
	name, _, _ = strings.Cut(name, "/")
	return strings.TrimSpace(name)
}

func packageLockDependencyRow(
	name string,
	version string,
	lang string,
	dependencyPath []string,
	dependencyScope string,
) map[string]any {
	row := map[string]any{
		"name":            strings.TrimSpace(name),
		"value":           strings.TrimSpace(version),
		"section":         "package-lock",
		"config_kind":     "dependency",
		"package_manager": "npm",
		"lockfile":        true,
		"lang":            lang,
	}
	if len(dependencyPath) > 0 {
		row["dependency_path"] = dependencyPath
		row["dependency_depth"] = len(dependencyPath)
		row["direct_dependency"] = len(dependencyPath) == 1
	}
	if dependencyScope != "" {
		row["dependency_scope"] = dependencyScope
		row["development_dependency"] = dependencyScope == "dev" || dependencyScope == "dev_optional"
	}
	return row
}

func packageLockDependencyScope(
	entry map[string]any,
	dependencyPath []string,
	rootScopes map[string]string,
) string {
	if packageLockBool(entry, "devOptional") {
		return "dev_optional"
	}
	if packageLockBool(entry, "dev") {
		return "dev"
	}
	if packageLockBool(entry, "optional") {
		return "optional"
	}
	if packageLockBool(entry, "peer") {
		return "peer"
	}
	if len(dependencyPath) > 0 {
		if scope := rootScopes[dependencyPath[0]]; scope != "" {
			return scope
		}
	}
	return "runtime"
}

func packageLockLegacyDependencyScope(entry map[string]any) string {
	if packageLockBool(entry, "dev") {
		return "dev"
	}
	if packageLockBool(entry, "optional") {
		return "optional"
	}
	if packageLockBool(entry, "peer") {
		return "peer"
	}
	return "runtime"
}

func packageLockRootDependencyScopes(raw any) map[string]string {
	entry, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string)
	for _, section := range []struct {
		key   string
		scope string
	}{
		{key: "dependencies", scope: "runtime"},
		{key: "optionalDependencies", scope: "optional"},
		{key: "peerDependencies", scope: "peer"},
		{key: "devDependencies", scope: "dev"},
	} {
		deps, ok := entry[section.key].(map[string]any)
		if !ok {
			continue
		}
		for name := range deps {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if existing, ok := out[name]; ok && packageLockScopePriority(existing) <= packageLockScopePriority(section.scope) {
				continue
			}
			out[name] = section.scope
		}
	}
	return out
}

func packageLockScopePriority(scope string) int {
	switch scope {
	case "runtime":
		return 0
	case "optional":
		return 1
	case "peer":
		return 2
	case "dev":
		return 3
	default:
		return 4
	}
}

func packageLockBool(payload map[string]any, key string) bool {
	raw, ok := payload[key]
	if !ok {
		return false
	}
	switch typed := raw.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func packageLockDependencyChains(packages map[string]any) map[string][]string {
	pathsByName := packageLockPathsByName(packages)
	chains := map[string][]string{}
	rootDeps := packageLockEntryDependencies(packages[""])
	for _, name := range sortedMapKeys(rootDeps) {
		path := packageLockDependencyChildPath("", name, pathsByName)
		if path == "" {
			continue
		}
		packageLockWalkDependencyChains(path, []string{name}, packages, pathsByName, chains)
	}
	return chains
}

func packageLockWalkDependencyChains(
	path string,
	chain []string,
	packages map[string]any,
	pathsByName map[string][]string,
	chains map[string][]string,
) {
	if path == "" || len(chain) == 0 {
		return
	}
	if existing, ok := chains[path]; ok && len(existing) <= len(chain) {
		return
	}
	chains[path] = append([]string(nil), chain...)
	for _, name := range sortedMapKeys(packageLockEntryDependencies(packages[path])) {
		childPath := packageLockDependencyChildPath(path, name, pathsByName)
		if childPath == "" {
			continue
		}
		childChain := append(append([]string(nil), chain...), name)
		packageLockWalkDependencyChains(childPath, childChain, packages, pathsByName, chains)
	}
}

func packageLockPathsByName(packages map[string]any) map[string][]string {
	out := make(map[string][]string, len(packages))
	for path := range packages {
		name := packageLockPackageName(path)
		if name == "" {
			continue
		}
		out[name] = append(out[name], path)
	}
	for name := range out {
		sort.Strings(out[name])
	}
	return out
}

func packageLockDependencyChildPath(parentPath string, name string, pathsByName map[string][]string) string {
	paths := pathsByName[name]
	if len(paths) == 0 {
		return ""
	}
	if parentPath != "" {
		nestedPrefix := strings.TrimRight(parentPath, "/") + "/node_modules/"
		for _, path := range paths {
			if strings.HasPrefix(path, nestedPrefix) {
				return path
			}
		}
	}
	topLevelPath := "node_modules/" + name
	for _, path := range paths {
		if path == topLevelPath {
			return path
		}
	}
	return paths[0]
}

func packageLockEntryDependencies(raw any) map[string]any {
	entry, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"} {
		deps, ok := entry[key].(map[string]any)
		if !ok {
			continue
		}
		for name, value := range deps {
			name = strings.TrimSpace(name)
			if name != "" {
				out[name] = value
			}
		}
	}
	return out
}
