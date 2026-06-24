// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"sort"
	"strings"
)

// pipfileLockDependencyVariables converts one parsed Pipfile.lock JSON
// document into content_entity dependency rows. Pipfile.lock pins each
// installed package under either the "default" or "develop" object; the
// reducer needs `value` to carry the exact installed version (with the
// leading `==` stripped) so PyPI advisory ranges can be evaluated. Inline
// git/path/url entries surface as non-`dependency` config_kind rows so the
// reducer cannot mis-admit them as PyPI registry consumption.
func pipfileLockDependencyVariables(document map[string]any) []map[string]any {
	rows := make([]map[string]any, 0)
	rows = append(rows, pipfileLockSectionRows(document, "default", false)...)
	rows = append(rows, pipfileLockSectionRows(document, "develop", true)...)
	return rows
}

func pipfileLockSectionRows(document map[string]any, section string, dev bool) []map[string]any {
	raw, ok := document[section].(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]map[string]any, 0, len(names))
	for index, name := range names {
		entry, ok := raw[name].(map[string]any)
		if !ok {
			continue
		}
		row := pipfileLockDependencyRow(name, entry, section, dev, index+1)
		if row == nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func pipfileLockDependencyRow(
	name string,
	entry map[string]any,
	section string,
	dev bool,
	lineNumber int,
) map[string]any {
	row := map[string]any{
		"name":            strings.TrimSpace(name),
		"line_number":     lineNumber,
		"section":         section,
		"package_manager": "pypi",
		"lang":            "json",
		"lockfile":        true,
	}
	if dev {
		row["dev_dependency"] = true
	}
	// VCS form: `{"git": "https://...", "ref": "v1.0"}`. Treat as
	// vcs_dependency so the supply-chain reducer cannot infer a PyPI version.
	if gitURL, ok := entry["git"].(string); ok && strings.TrimSpace(gitURL) != "" {
		row["config_kind"] = "vcs_dependency"
		row["source_kind"] = "vcs"
		row["source_url"] = strings.TrimSpace(gitURL)
		if ref, ok := entry["ref"].(string); ok && strings.TrimSpace(ref) != "" {
			row["source_ref"] = strings.TrimSpace(ref)
		}
		row["value"] = strings.TrimSpace(gitURL)
		return row
	}
	if pathValue, ok := entry["path"].(string); ok && strings.TrimSpace(pathValue) != "" {
		row["config_kind"] = "path_dependency"
		row["source_kind"] = "path"
		row["value"] = strings.TrimSpace(pathValue)
		return row
	}
	if urlValue, ok := entry["file"].(string); ok && strings.TrimSpace(urlValue) != "" {
		row["config_kind"] = "url_dependency"
		row["source_kind"] = "url"
		row["value"] = strings.TrimSpace(urlValue)
		return row
	}
	version, ok := entry["version"].(string)
	if !ok || strings.TrimSpace(version) == "" {
		return nil
	}
	row["config_kind"] = "dependency"
	row["value"] = strings.TrimPrefix(strings.TrimSpace(version), "==")
	return row
}
