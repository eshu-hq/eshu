// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// ParsePoetryLock reads one Poetry lockfile and emits exact-version
// content_entity dependency rows. Each [[package]] entry produces one row;
// any attached [package.source] subtable with `type = "git" | "directory" |
// "url"` swaps the row's config_kind to vcs/path/url so the supply-chain
// reducer does not treat git/path lock entries as PyPI registry versions.
func ParsePoetryLock(path string) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	payload := basePayload(path, LangTOML)
	sections, err := scanTOML(string(source))
	if err != nil {
		return nil, err
	}

	rows := make([]map[string]any, 0)
	var current *rowBuilder
	for _, section := range sections {
		switch {
		case section.Header == "package" && section.IsArray:
			if current != nil {
				rows = append(rows, current.finish())
			}
			current = poetryLockBuilderFromSection(section)
		case section.Header == "package.source":
			if current != nil {
				applyPoetrySourceSubtable(current, section)
			}
		default:
			if current != nil {
				rows = append(rows, current.finish())
				current = nil
			}
		}
	}
	if current != nil {
		rows = append(rows, current.finish())
	}
	payload["variables"] = rows
	return payload, nil
}

func poetryLockBuilderFromSection(section *tomlSection) *rowBuilder {
	builder := &rowBuilder{
		LineNumber:     section.StartLine,
		Section:        "package",
		PackageManager: PackageManager,
		Lang:           LangTOML,
		Lockfile:       true,
		ConfigKind:     configKindDependency,
	}
	if value, ok := section.Values["name"]; ok && value.IsString {
		builder.Name = value.StringValue
	}
	if value, ok := section.Values["version"]; ok && value.IsString {
		builder.Value = value.StringValue
	}
	if value, ok := section.Values["category"]; ok && value.IsString {
		if isLockfileDevCategory(value.StringValue) {
			builder.DevDependency = true
		}
	}
	// optional packages are still recorded — the reducer can decide later
	// whether to bound impact to required deps — so we do not branch on the
	// `optional` key here. Left as a documentation seam for that future
	// reducer change.
	if value, ok := section.Values["python-versions"]; ok && value.IsString {
		builder.Marker = "python_versions " + value.StringValue
	}
	if builder.Name == "" || builder.Value == "" {
		builder.ConfigKind = configKindMalformed
		builder.Malformed = true
	}
	return builder
}

func applyPoetrySourceSubtable(builder *rowBuilder, section *tomlSection) {
	kind := ""
	if value, ok := section.Values["type"]; ok && value.IsString {
		kind = strings.ToLower(value.StringValue)
	}
	var url string
	if value, ok := section.Values["url"]; ok && value.IsString {
		url = value.StringValue
	}
	// Prefer resolved_reference (commit SHA) over reference (branch/tag) so
	// downstream consumers get the most specific provenance the lockfile
	// proved. Poetry writes both keys when it can resolve the requested
	// reference to a commit.
	var reference string
	if value, ok := section.Values["resolved_reference"]; ok && value.IsString && value.StringValue != "" {
		reference = value.StringValue
	}
	if reference == "" {
		if value, ok := section.Values["reference"]; ok && value.IsString {
			reference = value.StringValue
		}
	}
	switch kind {
	case "git":
		builder.ConfigKind = configKindVCS
		builder.SourceKind = "vcs"
		builder.SourceURL = url
		builder.SourceRef = reference
	case "directory", "file":
		builder.ConfigKind = configKindPath
		builder.SourceKind = "path"
		builder.SourceURL = url
	case "url":
		builder.ConfigKind = configKindURL
		builder.SourceKind = "url"
		builder.SourceURL = url
	}
}

func isLockfileDevCategory(category string) bool {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "dev", "develop", "test", "tests", "testing", "lint", "ci", "qa":
		return true
	}
	return false
}
