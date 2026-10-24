// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget

import "strings"

type propStatus int

const (
	propResolved propStatus = iota
	propAmbiguous
	propUnresolved
)

// isExpression reports whether text still needs MSBuild evaluation: a
// property, item, or metadata reference, or a %XX escape, which MSBuild
// unescapes before it uses the value.
func isExpression(text string) bool {
	return strings.Contains(text, "$(") || strings.Contains(text, "@(") || strings.Contains(text, "%(") ||
		hasMSBuildEscape(text)
}

// hasMSBuildEscape reports whether text holds an MSBuild escape: a percent
// sign followed by two hex digits. A percent sign followed by anything else is
// literal text to MSBuild.
func hasMSBuildEscape(text string) bool {
	for i := strings.IndexByte(text, '%'); i >= 0 && i+2 < len(text); {
		if isHexDigit(text[i+1]) && isHexDigit(text[i+2]) {
			return true
		}
		next := strings.IndexByte(text[i+1:], '%')
		if next < 0 {
			return false
		}
		i += next + 1
	}
	return false
}

func isHexDigit(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

// status is the resolution verdict for a reference to this property. Only a
// property whose every declaration is unconditional, non-empty, free of
// MSBuild expressions, and identical resolves; MSBuild's last-declaration-wins
// override is deliberately not applied.
func (p *propState) status() propStatus {
	switch {
	case p.differ || p.anyCond:
		return propAmbiguous
	case p.bad || p.value == "" || isExpression(p.value):
		return propUnresolved
	}
	return propResolved
}

// buildPropsRows turns the scan into rows: package items first in document
// order, then property declarations. line_number is the emission sequence.
func buildPropsRows(scan *propsScan, limits *propsLimits) []map[string]any {
	rows := make([]map[string]any, 0, len(scan.items)+len(scan.props))
	centralUnproven := centralManagementUnproven(scan.states)
	for index, item := range scan.items {
		row := propsItemRow(item, scan.states, len(rows)+1, limits)
		reasons := scan.tracker.reasons(index, item, centralUnproven)
		if _, scoped := row["dependency_scope"]; !scoped {
			reasons |= reasonUnresolvedAssetMetadata
		}
		if reasons != 0 {
			row["item_ambiguity"] = reasons.String()
			row["partial_evidence"] = true
		}
		rows = append(rows, row)
	}
	for _, decl := range scan.props {
		rows = append(rows, propsPropertyRow(decl, scan.states[decl.stateKey], len(rows)+1))
	}
	return rows
}

func propsItemRow(item itemDecl, states map[string]*propState, lineNumber int, limits *propsLimits) map[string]any {
	raw := item.version
	if raw == "" {
		raw = item.override
	}
	value, metadata := resolvePropsVersion(raw, states, limits)
	scope := propsItemScope(item)
	row := map[string]any{
		"name":              item.name,
		"line_number":       lineNumber,
		"value":             value,
		"requested_version": raw,
		"section":           item.kind,
		"config_kind":       "dependency",
		"package_manager":   "nuget",
		"dependency_scope":  scope,
		"lang":              "nuget_project",
	}
	if scope == "" {
		// Unproven asset metadata: leave the scope absent, not guessed. The
		// key stays in the literal so the map is sized once.
		delete(row, "dependency_scope")
	}
	for key, val := range metadata {
		row[key] = val
	}
	if item.kind == "PackageVersion" {
		// A central version is a policy: it does not prove that any project
		// references the package, so direct_dependency stays absent.
		row["central_version_declaration"] = true
	} else {
		row["direct_dependency"] = true
	}
	if item.override != "" {
		row["version_override"] = item.override
	}
	setNuGetProjectString(row, "private_assets", item.private)
	setNuGetProjectString(row, "include_assets", item.include)
	setNuGetProjectString(row, "exclude_assets", item.exclude)
	setNuGetProjectString(row, "condition", firstNonEmpty(item.condition, item.groupCond))
	setNuGetProjectString(row, "condition_item", item.condition)
	setNuGetProjectString(row, "condition_group", item.groupCond)
	if item.condition != "" || item.groupCond != "" {
		row["conditional_declaration"] = true
		row["partial_evidence"] = true
	}
	if scope == "development" || scope == "test" {
		row["development_dependency"] = true
	}
	if scope == "test" {
		row["test_dependency"] = true
	}
	return row
}

// propsItemScope returns the item's dependency_scope, or "" when the asset
// metadata that decides it is unproven. NuGet.targets turns a
// GlobalPackageReference into a PackageReference whose element sets
// PrivateAssets="All" and the development IncludeAssets; element metadata wins
// over the copied source metadata, so that scope is fixed whatever the item
// declares. Other items use the .csproj heuristic on their own metadata, which
// decides only from values that are unconditional and need no evaluation.
func propsItemScope(item itemDecl) string {
	if item.kind == "GlobalPackageReference" {
		return nugetProjectDependencyScope(item.name, nugetPackageReference{
			PrivateAssets: "All",
			IncludeAssets: "Runtime;Build;Native;contentFiles;Analyzers",
		})
	}
	private, privateKnown := provenAssetValue(item.private, item.privateCond)
	include, includeKnown := provenAssetValue(item.include, item.includeCond)
	scope := nugetProjectDependencyScope(item.name, nugetPackageReference{PrivateAssets: private, IncludeAssets: include})
	if scope == "runtime" && (!privateKnown || !includeKnown) {
		// An unproven value could still say All or none.
		return ""
	}
	return scope
}

// provenAssetValue returns the asset metadata value and whether it is proven:
// unconditional and free of anything MSBuild would still evaluate. An
// unproven value is returned as "" so it cannot decide a scope.
func provenAssetValue(value string, conditional bool) (string, bool) {
	if conditional || isExpression(value) {
		return "", false
	}
	return value, true
}

func propsPropertyRow(decl propDecl, state *propState, lineNumber int) map[string]any {
	evidence := propsValueEvidence(decl, state)
	row := map[string]any{
		"name":           decl.name,
		"line_number":    lineNumber,
		"value":          decl.value,
		"section":        "PropertyGroup",
		"config_kind":    "msbuild_property",
		"lang":           "nuget_project",
		"value_evidence": evidence,
	}
	if evidence != "literal" {
		row["partial_evidence"] = true
	}
	setNuGetProjectString(row, "condition", firstNonEmpty(decl.condition, decl.groupCond))
	setNuGetProjectString(row, "condition_item", decl.condition)
	setNuGetProjectString(row, "condition_group", decl.groupCond)
	return row
}

func propsValueEvidence(decl propDecl, state *propState) string {
	switch {
	case decl.condition != "" || decl.groupCond != "":
		return "conditional"
	case state.differ || state.anyCond:
		return "ambiguous"
	case decl.oversized:
		return "oversized"
	case decl.value == "":
		return "empty"
	case isExpression(decl.value):
		return "msbuild_expression"
	}
	return "literal"
}

// resolvePropsVersion substitutes $(Property) references only from properties
// whose state is clean, and keeps the declared text whenever anything is
// unproven. The version_evidence vocabulary matches the .csproj path.
func resolvePropsVersion(raw string, states map[string]*propState, limits *propsLimits) (string, map[string]any) {
	metadata := map[string]any{}
	if raw == "" {
		metadata["version_evidence"] = "missing_version"
		metadata["partial_evidence"] = true
		return "", metadata
	}
	if strings.Contains(raw, "@(") || strings.Contains(raw, "%(") || hasMSBuildEscape(raw) {
		return unresolvedPropsVersion(raw, metadata, nil)
	}
	matches := msbuildPropertyReferencePattern.FindAllStringSubmatchIndex(raw, -1)
	if len(matches) == 0 {
		if strings.Contains(raw, "$(") {
			return unresolvedPropsVersion(raw, metadata, nil)
		}
		metadata["version_evidence"] = "package_reference"
		return raw, metadata
	}
	var out strings.Builder
	var resolved, unresolved, ambiguous []string
	last := 0
	for _, match := range matches {
		name := raw[match[2]:match[3]]
		state := states[strings.ToLower(name)]
		switch {
		case state == nil:
			unresolved = append(unresolved, name)
		case state.status() == propAmbiguous:
			ambiguous = append(ambiguous, name)
		case state.status() == propUnresolved:
			unresolved = append(unresolved, name)
		default:
			out.WriteString(raw[last:match[0]])
			out.WriteString(state.value)
			last = match[1]
			resolved = append(resolved, name)
		}
	}
	out.WriteString(raw[last:])
	setNuGetProjectVersionPropertyMetadata(metadata, resolved)
	if len(ambiguous) > 0 {
		metadata["ambiguous_msbuild_property"] = strings.Join(uniqueNuGetProjectStrings(ambiguous), ",")
		metadata["version_evidence"] = "ambiguous_msbuild_property"
		metadata["partial_evidence"] = true
		return raw, metadata
	}
	result := out.String()
	if len(unresolved) > 0 || isExpression(result) || len(result) > MaxPropsVersionBytes {
		if len(result) > MaxPropsVersionBytes {
			limits.oversizedFields++
		}
		return unresolvedPropsVersion(raw, metadata, unresolved)
	}
	metadata["version_evidence"] = "project_property"
	return result, metadata
}

func unresolvedPropsVersion(raw string, metadata map[string]any, names []string) (string, map[string]any) {
	delete(metadata, "version_property")
	delete(metadata, "version_properties")
	if len(names) > 0 {
		metadata["unresolved_msbuild_property"] = strings.Join(uniqueNuGetProjectStrings(names), ",")
	}
	metadata["version_evidence"] = "unresolved_msbuild_property"
	metadata["partial_evidence"] = true
	return raw, metadata
}
