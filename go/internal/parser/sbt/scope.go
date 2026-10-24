// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "strings"

// axisUnknown is the axis value for a `libraryDependencies in (...)` scope the
// scanner cannot read as one configuration. It is not a configuration name, so
// resolveConfigScope reports it unknown.
const axisUnknown = "(unknown axis)"

// configOverlong is the configuration spelling recorded for an identifier
// chain longer than MaxFieldBytes. It is never a configuration name.
const configOverlong = "(overlong)"

// axisConfig maps a `Config /` or `in Config` scope axis to its configuration
// name. Any other axis (ThisBuild, a project reference) yields "".
func axisConfig(name string) string {
	switch name {
	case "Test", "IntegrationTest", "Provided", "Runtime", "Compile":
		return name
	}
	return ""
}

// axisScope maps a `Config /` scope axis before libraryDependencies to a
// configuration name: `Test / libraryDependencies` declares test-scope
// dependencies.
func axisScope(prev2, prev token) string {
	if prev.kind != kindSymbol || prev.text != "/" || prev2.kind != kindIdent {
		return ""
	}
	return axisConfig(prev2.text)
}

// isThisBuildAxis reports a `ThisBuild /` axis before a key.
func isThisBuildAxis(prev2, prev token) bool {
	return prev.kind == kindSymbol && prev.text == "/" && prev2.kind == kindIdent && prev2.text == "ThisBuild"
}

// configPartScope maps one configuration name to the dependency scope
// vocabulary shared with the Gradle and Maven parsers, or reports it unknown.
// A mapping such as `test->default(compile)` is judged by its left side.
func configPartScope(part string) (string, bool) {
	name := strings.ToLower(strings.TrimSpace(part))
	if arrow := strings.Index(name, "->"); arrow >= 0 {
		name = strings.TrimSpace(name[:arrow])
	}
	switch name {
	case "compile", "optional", "compile-internal", "docs", "sources", "plugin", "scala-tool", "scala-doc-tool", "pom":
		return "compile", true
	case "runtime", "runtime-internal":
		return "runtime", true
	case "provided":
		return "provided", true
	case "test", "it", "integrationtest", "test-internal":
		return "test", true
	}
	return "", false
}

// configParts splits an sbt configuration list on ',' and ';' and drops empty
// parts, so `"test,it"` and `"compile->compile;test->test"` read as lists.
func configParts(config string) []string {
	return strings.FieldsFunc(config, func(r rune) bool { return r == ',' || r == ';' })
}

// resolveConfigScope maps an sbt configuration spelling to a dependency scope.
// "" is the default compile configuration. A list resolves to the shared scope
// when every part agrees; otherwise any compile or runtime part means compile,
// and provided with test-like parts means provided. known is false when any
// part is not a recognised configuration (a custom Config value, a variable,
// an empty list); the scope is then compile only as a placeholder and the
// caller must report the row as partial, never as a resolved compile scope.
func resolveConfigScope(config string) (scope string, known bool) {
	if config == "" {
		return "compile", true
	}
	parts := configParts(config)
	if len(parts) == 0 {
		return "compile", false
	}
	seen := map[string]bool{}
	for _, part := range parts {
		partScope, ok := configPartScope(part)
		if !ok {
			return "compile", false
		}
		seen[partScope] = true
	}
	switch {
	case len(seen) == 1:
		for only := range seen {
			return only, true
		}
	case seen["compile"] || seen["runtime"]:
		return "compile", true
	}
	return "provided", true
}

// hasConfigPart reports whether the configuration list names part, ignoring
// case and any `->` mapping.
func hasConfigPart(config, part string) bool {
	for _, candidate := range configParts(config) {
		name := strings.TrimSpace(candidate)
		if arrow := strings.Index(name, "->"); arrow >= 0 {
			name = strings.TrimSpace(name[:arrow])
		}
		if strings.EqualFold(name, part) {
			return true
		}
	}
	return false
}

// setAxis sets the scope a `Config /` or `in Config` axis declares, leaving
// the explicit configuration empty.
func (d *dependency) setAxis(axis string) {
	d.scope, d.scopeKnown = resolveConfigScope(axis)
}

// setConfig records an explicit `% config` and the scope it implies.
func (d *dependency) setConfig(config string) {
	d.config = config
	d.scope, d.scopeKnown = resolveConfigScope(config)
}
