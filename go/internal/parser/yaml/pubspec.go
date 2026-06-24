// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func isPubspecDependencyFile(filename string) bool {
	switch strings.ToLower(strings.TrimSpace(filename)) {
	case "pubspec.yaml", "pubspec.yml", "pubspec.lock":
		return true
	default:
		return false
	}
}

func appendPubspecDependencyRows(
	payload map[string]any,
	filename string,
	document map[string]any,
	path string,
	lineNumber int,
) {
	rows := pubspecDependencyRows(filename, document, path, lineNumber)
	for _, row := range rows {
		shared.AppendBucket(payload, "variables", row)
	}
	shared.SortNamedBucket(payload, "variables")
}

func pubspecDependencyRows(filename string, document map[string]any, path string, lineNumber int) []map[string]any {
	switch strings.ToLower(strings.TrimSpace(filename)) {
	case "pubspec.lock":
		return pubspecLockRows(document, path, lineNumber)
	case "pubspec.yaml", "pubspec.yml":
		return pubspecManifestRows(document, path, lineNumber)
	default:
		return nil
	}
}

func pubspecLockRows(document map[string]any, path string, lineNumber int) []map[string]any {
	packages, ok := document["packages"].(map[string]any)
	if !ok {
		return nil
	}
	names := sortedMapKeys(packages)
	rows := make([]map[string]any, 0, len(names))
	for _, name := range names {
		entry, ok := packages[name].(map[string]any)
		if !ok {
			continue
		}
		description, _ := entry["description"].(map[string]any)
		packageName := strings.TrimSpace(firstNonEmptyString(description["name"], name))
		if packageName == "" || packageName != name {
			continue
		}
		source := strings.TrimSpace(fmt.Sprint(entry["source"]))
		sourceURL := normalizedPubHostedURL(description["url"])
		if source != "hosted" || sourceURL == "" {
			continue
		}
		version := strings.TrimSpace(fmt.Sprint(entry["version"]))
		if version == "" || version == "<nil>" {
			continue
		}
		dependencyKind := strings.TrimSpace(fmt.Sprint(entry["dependency"]))
		row := pubDependencyRow(packageName, version, path, "pubspec.lock", lineNumber)
		row["lockfile"] = true
		row["source_location"] = sourceURL
		row["dependency_scope"] = pubLockDependencyScope(dependencyKind)
		row["pub_dependency_kind"] = dependencyKind
		row["direct_dependency"] = strings.HasPrefix(dependencyKind, "direct ")
		if sha := strings.TrimSpace(fmt.Sprint(description["sha256"])); sha != "" && sha != "<nil>" {
			row["sha256"] = sha
		}
		rows = append(rows, row)
	}
	return rows
}

func pubspecManifestRows(document map[string]any, path string, lineNumber int) []map[string]any {
	if _, hasOverrides := document["dependency_overrides"]; hasOverrides {
		return nil
	}
	rows := append(
		pubspecManifestSectionRows(document, "dependencies", "runtime", false, path, lineNumber),
		pubspecManifestSectionRows(document, "dev_dependencies", "dev", true, path, lineNumber)...,
	)
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		if fmt.Sprint(left["name"]) == fmt.Sprint(right["name"]) {
			return fmt.Sprint(left["section"]) < fmt.Sprint(right["section"])
		}
		return fmt.Sprint(left["name"]) < fmt.Sprint(right["name"])
	})
	return rows
}

func pubspecManifestSectionRows(
	document map[string]any,
	section string,
	scope string,
	development bool,
	path string,
	lineNumber int,
) []map[string]any {
	dependencies, ok := document[section].(map[string]any)
	if !ok {
		return nil
	}
	names := sortedMapKeys(dependencies)
	rows := make([]map[string]any, 0, len(names))
	for _, name := range names {
		version, sourceURL, ok := pubspecManifestDependency(dependencies[name])
		if !ok {
			continue
		}
		row := pubDependencyRow(name, version, path, section, lineNumber)
		row["dependency_scope"] = scope
		if development {
			row["development_dependency"] = true
		}
		if sourceURL != "" {
			row["source_location"] = sourceURL
		}
		rows = append(rows, row)
	}
	return rows
}

func pubspecManifestDependency(value any) (version string, sourceURL string, ok bool) {
	switch typed := value.(type) {
	case string:
		version = strings.TrimSpace(typed)
	case map[string]any:
		if _, hasGit := typed["git"]; hasGit {
			return "", "", false
		}
		if _, hasPath := typed["path"]; hasPath {
			return "", "", false
		}
		hostedValue, hasHosted := typed["hosted"]
		hosted, hostedOK := hostedValue.(map[string]any)
		if hasHosted && !hostedOK {
			sourceURL = normalizedPubHostedURL(hostedValue)
			if sourceURL == "" {
				return "", "", false
			}
		}
		if hostedOK {
			sourceURL = normalizedPubHostedURL(hosted["url"])
		}
		version = strings.TrimSpace(fmt.Sprint(typed["version"]))
		if hasHosted && sourceURL == "" {
			return "", "", false
		}
	default:
		return "", "", false
	}
	if version == "" || version == "<nil>" {
		return "", "", false
	}
	return version, sourceURL, true
}

func pubDependencyRow(name string, value string, path string, section string, lineNumber int) map[string]any {
	return map[string]any{
		"name":            strings.TrimSpace(name),
		"value":           strings.TrimSpace(value),
		"section":         section,
		"line_number":     lineNumber,
		"config_kind":     "dependency",
		"package_manager": "pub",
		"path":            path,
		"lang":            "yaml",
	}
}

func pubLockDependencyScope(kind string) string {
	switch strings.TrimSpace(kind) {
	case "direct dev":
		return "dev"
	case "transitive":
		return "transitive"
	default:
		return "runtime"
	}
}

func normalizedPubHostedURL(value any) string {
	raw := strings.TrimRight(strings.TrimSpace(fmt.Sprint(value)), "/")
	switch strings.ToLower(raw) {
	case "https://pub.dev", "http://pub.dev", "https://pub.dartlang.org", "http://pub.dartlang.org":
		return "https://pub.dev"
	default:
		return ""
	}
}

func sortedMapKeys(values map[string]any) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		key = strings.TrimSpace(key)
		if key != "" {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		candidate := strings.TrimSpace(fmt.Sprint(value))
		if candidate != "" && candidate != "<nil>" {
			return candidate
		}
	}
	return ""
}
