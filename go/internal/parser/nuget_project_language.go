// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var msbuildPropertyReferencePattern = regexp.MustCompile(`\$\(([A-Za-z_][A-Za-z0-9_.-]*)\)`)

type nugetProjectFile struct {
	PropertyGroups []nugetPropertyGroup `xml:"PropertyGroup"`
	ItemGroups     []nugetItemGroup     `xml:"ItemGroup"`
}

type nugetPropertyGroup struct {
	Condition  string            `xml:"Condition,attr"`
	Properties []nugetNamedValue `xml:",any"`
}

type nugetItemGroup struct {
	Condition         string                  `xml:"Condition,attr"`
	PackageReferences []nugetPackageReference `xml:"PackageReference"`
}

type nugetPackageReference struct {
	Include       string            `xml:"Include,attr"`
	Update        string            `xml:"Update,attr"`
	Version       string            `xml:"Version,attr"`
	PrivateAssets string            `xml:"PrivateAssets,attr"`
	IncludeAssets string            `xml:"IncludeAssets,attr"`
	ExcludeAssets string            `xml:"ExcludeAssets,attr"`
	Condition     string            `xml:"Condition,attr"`
	Children      []nugetNamedValue `xml:",any"`
}

type nugetNamedValue struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

type nugetProjectProperty struct {
	value     string
	ambiguous bool
}

func parseNuGetProject(path string, isDependency bool, options Options) (map[string]any, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}

	var project nugetProjectFile
	decoder := xml.NewDecoder(bytes.NewReader(source))
	if err := decoder.Decode(&project); err != nil {
		return nil, fmt.Errorf("parse nuget project file %q: %w", path, err)
	}

	payload := shared.BasePayload(path, "nuget_project", isDependency)
	payload["variables"] = nugetProjectDependencyRows(project)
	if options.IndexSource {
		payload["source"] = string(source)
	}
	return payload, nil
}

func nugetProjectDependencyRows(project nugetProjectFile) []map[string]any {
	properties := nugetProjectProperties(project.PropertyGroups)
	rows := make([]map[string]any, 0)
	for _, group := range project.ItemGroups {
		for _, reference := range group.PackageReferences {
			name := strings.TrimSpace(firstNonEmpty(reference.Include, reference.Update))
			if name == "" {
				continue
			}
			row := nugetProjectDependencyRow(name, reference, group.Condition, properties, len(rows)+1)
			rows = append(rows, row)
		}
	}
	return rows
}

func nugetProjectDependencyRow(
	name string,
	reference nugetPackageReference,
	groupCondition string,
	properties map[string]nugetProjectProperty,
	lineNumber int,
) map[string]any {
	rawVersion := strings.TrimSpace(firstNonEmpty(
		reference.Version,
		nugetChildValue(reference.Children, "Version"),
	))
	resolvedVersion, versionMetadata := resolveNuGetVersion(rawVersion, properties)
	scope := nugetProjectDependencyScope(name, reference)
	row := map[string]any{
		"name":              name,
		"line_number":       lineNumber,
		"value":             resolvedVersion,
		"requested_version": rawVersion,
		"section":           "PackageReference",
		"config_kind":       "dependency",
		"package_manager":   "nuget",
		"dependency_scope":  scope,
		"direct_dependency": true,
		"lang":              "nuget_project",
	}
	for key, value := range versionMetadata {
		row[key] = value
	}
	setNuGetProjectString(row, "private_assets", firstNonEmpty(
		reference.PrivateAssets,
		nugetChildValue(reference.Children, "PrivateAssets"),
	))
	setNuGetProjectString(row, "include_assets", firstNonEmpty(
		reference.IncludeAssets,
		nugetChildValue(reference.Children, "IncludeAssets"),
	))
	setNuGetProjectString(row, "exclude_assets", firstNonEmpty(
		reference.ExcludeAssets,
		nugetChildValue(reference.Children, "ExcludeAssets"),
	))
	setNuGetProjectString(row, "condition", firstNonEmpty(reference.Condition, groupCondition))
	if scope == "development" || scope == "test" {
		row["development_dependency"] = true
	}
	if scope == "test" {
		row["test_dependency"] = true
	}
	return row
}

func nugetProjectProperties(groups []nugetPropertyGroup) map[string]nugetProjectProperty {
	out := make(map[string]nugetProjectProperty)
	for _, group := range groups {
		for _, property := range group.Properties {
			name := strings.TrimSpace(property.XMLName.Local)
			value := strings.TrimSpace(property.Value)
			if name == "" || value == "" {
				continue
			}
			existing, ok := out[name]
			if !ok {
				out[name] = nugetProjectProperty{value: value}
				continue
			}
			if existing.value != value {
				existing.ambiguous = true
				out[name] = existing
			}
		}
	}
	return out
}

func resolveNuGetVersion(raw string, properties map[string]nugetProjectProperty) (string, map[string]any) {
	metadata := map[string]any{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		metadata["version_evidence"] = "missing_version"
		metadata["partial_evidence"] = true
		return "", metadata
	}
	matches := msbuildPropertyReferencePattern.FindAllStringSubmatch(raw, -1)
	if len(matches) > 0 {
		resolved := raw
		resolvedProperties := make([]string, 0, len(matches))
		unresolvedProperties := make([]string, 0)
		ambiguousProperties := make([]string, 0)
		for _, match := range matches {
			if len(match) != 2 {
				continue
			}
			property := match[1]
			resolution, ok := properties[property]
			if ok && resolution.ambiguous {
				ambiguousProperties = append(ambiguousProperties, property)
				continue
			}
			if ok && resolution.value != "" {
				value := strings.TrimSpace(resolution.value)
				resolved = strings.ReplaceAll(resolved, match[0], value)
				resolvedProperties = append(resolvedProperties, property)
				continue
			}
			unresolvedProperties = append(unresolvedProperties, property)
		}
		setNuGetProjectVersionPropertyMetadata(metadata, resolvedProperties)
		if len(ambiguousProperties) > 0 {
			metadata["ambiguous_msbuild_property"] = strings.Join(uniqueNuGetProjectStrings(ambiguousProperties), ",")
			metadata["version_evidence"] = "ambiguous_msbuild_property"
			metadata["partial_evidence"] = true
			return raw, metadata
		}
		if len(unresolvedProperties) > 0 {
			metadata["unresolved_msbuild_property"] = strings.Join(uniqueNuGetProjectStrings(unresolvedProperties), ",")
			metadata["version_evidence"] = "unresolved_msbuild_property"
			metadata["partial_evidence"] = true
			return raw, metadata
		}
		metadata["version_evidence"] = "project_property"
		return resolved, metadata
	}
	if strings.Contains(raw, "$(") {
		metadata["version_evidence"] = "unresolved_msbuild_property"
		metadata["partial_evidence"] = true
		return raw, metadata
	}
	metadata["version_evidence"] = "package_reference"
	return raw, metadata
}

func setNuGetProjectVersionPropertyMetadata(metadata map[string]any, properties []string) {
	properties = uniqueNuGetProjectStrings(properties)
	switch len(properties) {
	case 0:
		return
	case 1:
		metadata["version_property"] = properties[0]
	default:
		metadata["version_properties"] = properties
	}
}

func nugetProjectDependencyScope(name string, reference nugetPackageReference) string {
	privateAssets := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		reference.PrivateAssets,
		nugetChildValue(reference.Children, "PrivateAssets"),
	)))
	includeAssets := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		reference.IncludeAssets,
		nugetChildValue(reference.Children, "IncludeAssets"),
	)))
	if isNuGetTestDependency(name) {
		return "test"
	}
	if assetListContains(privateAssets, "all") || assetListContains(includeAssets, "none") {
		return "development"
	}
	return "runtime"
}

func isNuGetTestDependency(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "xunit", "nunit", "mstest.testframework", "microsoft.net.test.sdk", "coverlet.collector":
		return true
	}
	return strings.Contains(normalized, "test")
}

func assetListContains(raw string, want string) bool {
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ';' || r == ',' || r == ' '
	}) {
		if strings.EqualFold(strings.TrimSpace(part), want) {
			return true
		}
	}
	return false
}

func nugetChildValue(values []nugetNamedValue, name string) string {
	for _, value := range values {
		if strings.EqualFold(value.XMLName.Local, name) {
			return strings.TrimSpace(value.Value)
		}
	}
	return ""
}

func setNuGetProjectString(row map[string]any, key string, value string) {
	if value = strings.TrimSpace(value); value != "" {
		row[key] = value
	}
}

func uniqueNuGetProjectStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
