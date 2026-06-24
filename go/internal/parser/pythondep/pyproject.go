// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// ParsePyProject parses one pyproject.toml file and emits PEP 621 +
// Poetry + Hatch dependency tables as content_entity dependency rows.
// VCS, path, URL and editable dependency forms surface as separate
// config_kind values so the supply-chain reducer cannot mis-admit them as
// PyPI registry consumption. Tables outside the known dependency shape are
// ignored.
func ParsePyProject(path string) (map[string]any, error) {
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
	for _, section := range sections {
		switch section.Header {
		case "project":
			rows = append(rows, parseProjectArrayTable(section, "project.dependencies", "dependencies", false)...)
		case "":
			// top-level — nothing here yet for PyPI deps.
		}
		if strings.HasPrefix(section.Header, "project.optional-dependencies") {
			rows = append(rows, parseProjectOptionalDependencies(section)...)
		}
		if section.Header == "tool.poetry.dependencies" {
			rows = append(rows, parsePoetryDependencyTable(section, section.Header, false)...)
		}
		if section.Header == "tool.poetry.dev-dependencies" {
			rows = append(rows, parsePoetryDependencyTable(section, section.Header, true)...)
		}
		if strings.HasPrefix(section.Header, "tool.poetry.group.") &&
			strings.HasSuffix(section.Header, ".dependencies") {
			rows = append(rows, parsePoetryDependencyTable(section, section.Header, isPoetryGroupDev(section.Header))...)
		}
		if strings.HasPrefix(section.Header, "tool.hatch.envs.") &&
			strings.HasSuffix(section.Header, ".dependencies") {
			rows = append(rows, parseHatchDependencyTable(section)...)
		}
	}
	payload["variables"] = rows
	return payload, nil
}

// parseProjectArrayTable reads PEP 621-style `dependencies = [...]` arrays.
// The `dev` flag is true when the source table is an optional-dependencies
// group named in a dev-like way.
func parseProjectArrayTable(section *tomlSection, sectionName string, key string, dev bool) []map[string]any {
	value, ok := section.Values[key]
	if !ok || !value.IsArray {
		return nil
	}
	rows := make([]map[string]any, 0, len(value.Array))
	for index, element := range value.Array {
		row := parseRequirementLine(strings.TrimSpace(element), element, sectionName, dev, section.StartLine+index+1)
		row.Lang = LangTOML
		rows = append(rows, row.finish())
	}
	return rows
}

func parseProjectOptionalDependencies(section *tomlSection) []map[string]any {
	rows := []map[string]any{}
	for _, key := range section.Keys {
		value := section.Values[key]
		if !value.IsArray {
			continue
		}
		groupName := key
		sectionName := "project.optional-dependencies." + groupName
		dev := isOptionalGroupDev(groupName)
		for index, element := range value.Array {
			row := parseRequirementLine(strings.TrimSpace(element), element, sectionName, dev, section.StartLine+index+1)
			row.Lang = LangTOML
			rows = append(rows, row.finish())
		}
	}
	return rows
}

// parsePoetryDependencyTable converts one `[tool.poetry...dependencies]`
// table into rows. Poetry dependencies can be string ranges or inline tables
// describing the version, extras, or source (git/path).
func parsePoetryDependencyTable(section *tomlSection, sectionName string, dev bool) []map[string]any {
	rows := []map[string]any{}
	for _, key := range section.Keys {
		if key == "python" {
			// Python interpreter constraint, not a PyPI package. Omitted so
			// vulnerability matching cannot mis-match "python" to an unrelated
			// PyPI advisory; the interpreter version still lives in the raw
			// TOML for any consumer that wants it.
			continue
		}
		value := section.Values[key]
		row := poetryDependencyRow(key, value, sectionName, dev, section.StartLine)
		rows = append(rows, row)
	}
	return rows
}

func poetryDependencyRow(name string, value tomlValue, sectionName string, dev bool, line int) map[string]any {
	builder := rowBuilder{
		Name:           name,
		LineNumber:     line,
		Section:        sectionName,
		PackageManager: PackageManager,
		Lang:           LangTOML,
		DevDependency:  dev,
	}
	switch {
	case value.IsString:
		builder.Value = value.StringValue
		builder.ConfigKind = configKindDependency
	case value.IsInline:
		applyPoetryInlineSource(&builder, value)
	case value.Scalar != "":
		builder.Value = unquoteString(value.Scalar)
		builder.ConfigKind = configKindDependency
	default:
		builder.ConfigKind = configKindMalformed
		builder.Malformed = true
	}
	return builder.finish()
}

func applyPoetryInlineSource(builder *rowBuilder, value tomlValue) {
	if version, ok := value.InlineTable["version"]; ok && version.IsString {
		builder.Value = version.StringValue
		builder.ConfigKind = configKindDependency
	}
	if extras, ok := value.InlineTable["extras"]; ok && extras.IsArray {
		builder.Extras = append(builder.Extras, extras.Array...)
	}
	if marker, ok := value.InlineTable["markers"]; ok && marker.IsString {
		builder.Marker = marker.StringValue
	}
	if path, ok := value.InlineTable["path"]; ok && path.IsString {
		builder.Value = path.StringValue
		builder.ConfigKind = configKindPath
		builder.SourceKind = "path"
		return
	}
	if url, ok := value.InlineTable["url"]; ok && url.IsString {
		builder.Value = url.StringValue
		builder.ConfigKind = configKindURL
		builder.SourceKind = "url"
		return
	}
	if git, ok := value.InlineTable["git"]; ok && git.IsString {
		builder.Value = git.StringValue
		builder.ConfigKind = configKindVCS
		builder.SourceKind = "vcs"
		builder.SourceURL = git.StringValue
		if rev, ok := value.InlineTable["rev"]; ok && rev.IsString {
			builder.SourceRef = rev.StringValue
		} else if branch, ok := value.InlineTable["branch"]; ok && branch.IsString {
			builder.SourceRef = branch.StringValue
		} else if tag, ok := value.InlineTable["tag"]; ok && tag.IsString {
			builder.SourceRef = tag.StringValue
		}
		return
	}
	if builder.ConfigKind == "" {
		builder.ConfigKind = configKindDependency
	}
}

func parseHatchDependencyTable(section *tomlSection) []map[string]any {
	rows := []map[string]any{}
	dev := isHatchEnvDev(section.Header)
	// Standard Hatch: `[tool.hatch.envs.X]` carries a `dependencies = [...]`
	// array. Treat each element as a PEP 508 requirement.
	if value, ok := section.Values["dependencies"]; ok && value.IsArray {
		for index, element := range value.Array {
			row := parseRequirementLine(strings.TrimSpace(element), element, section.Header, dev, section.StartLine+index+1)
			row.Lang = LangTOML
			rows = append(rows, row.finish())
		}
		return rows
	}
	// Also accept the Poetry-style table shape `[tool.hatch.envs.X.dependencies]`
	// where each key is a package name and the value is either a version
	// string or an inline source table. This keeps Eshu robust to projects
	// that mix Hatch envs with table syntax.
	for _, key := range section.Keys {
		value := section.Values[key]
		row := poetryDependencyRow(key, value, section.Header, dev, section.StartLine)
		rows = append(rows, row)
	}
	return rows
}

func isPoetryGroupDev(header string) bool {
	parts := strings.Split(header, ".")
	if len(parts) < 5 {
		return false
	}
	groupName := strings.ToLower(parts[3])
	switch groupName {
	case "dev", "develop", "test", "tests", "testing", "lint", "ci", "qa":
		return true
	}
	return strings.Contains(groupName, "dev") || strings.Contains(groupName, "test")
}

func isHatchEnvDev(header string) bool {
	parts := strings.Split(header, ".")
	if len(parts) < 5 {
		return false
	}
	envName := strings.ToLower(parts[3])
	switch envName {
	case "dev", "develop", "test", "tests", "testing", "lint", "ci", "qa":
		return true
	}
	return strings.Contains(envName, "dev") || strings.Contains(envName, "test")
}

func isOptionalGroupDev(groupName string) bool {
	lower := strings.ToLower(groupName)
	switch lower {
	case "dev", "develop", "test", "tests", "testing", "lint", "ci", "qa":
		return true
	}
	return strings.Contains(lower, "dev") || strings.Contains(lower, "test")
}
