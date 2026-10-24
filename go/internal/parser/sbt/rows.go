// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "strings"

// axisScope maps a `Config /` scope axis before libraryDependencies to a
// configuration name: `Test / libraryDependencies` declares test-scope
// dependencies. Any other axis (ThisBuild, a project reference) yields "".
func axisScope(prev2, prev token) string {
	if prev.kind != kindSymbol || prev.text != "/" || prev2.kind != kindIdent {
		return ""
	}
	switch prev2.text {
	case "Test", "IntegrationTest", "Provided", "Runtime", "Compile":
		return prev2.text
	}
	return ""
}

// isThisBuildAxis reports a `ThisBuild /` axis before a key.
func isThisBuildAxis(prev2, prev token) bool {
	return prev.kind == kindSymbol && prev.text == "/" && prev2.kind == kindIdent && prev2.text == "ThisBuild"
}

// scopeForConfig maps an sbt configuration to the dependency scope
// vocabulary shared with the Gradle and Maven parsers. Unknown custom
// configurations map to compile; the raw spelling stays in sbt_configuration.
func scopeForConfig(config string) string {
	name := strings.ToLower(strings.TrimSpace(config))
	if arrow := strings.Index(name, "->"); arrow >= 0 {
		name = strings.TrimSpace(name[:arrow])
	}
	switch name {
	case "test", "it", "integrationtest", "test-internal":
		return "test"
	case "provided":
		return "provided"
	case "runtime":
		return "runtime"
	default:
		return "compile"
	}
}

// scalaBinaryVersion returns the artifact suffix sbt appends for `%%`:
// "2.13" for any 2.13.x, "3" for any 3.x, and "" when the version is not a
// recognisable Scala release.
func scalaBinaryVersion(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "3":
		return "3"
	case "2":
		minor := parts[1]
		if minor == "" {
			return ""
		}
		for index := 0; index < len(minor); index++ {
			if minor[index] < '0' || minor[index] > '9' {
				return ""
			}
		}
		return "2." + minor
	}
	return ""
}

// isDynamicRevision reports an Ivy dynamic revision (`1.+`, `latest.release`,
// `[1.0,2.0)`) that names no single version.
func isDynamicRevision(version string) bool {
	return strings.HasSuffix(version, "+") ||
		strings.HasPrefix(version, "latest.") ||
		strings.HasPrefix(version, "[") ||
		strings.HasPrefix(version, "(")
}

// row builds the content_entity-shaped dependency row, mirroring the Gradle
// row contract. binary is the single known Scala binary version, or "".
func (d dependency) row(vals map[string]string, binary string) map[string]any {
	value := d.version
	state := "resolved"
	var unresolvedKeys []string
	switch d.kind {
	case versionLiteral:
		switch {
		case value == "":
			state = "partial"
		case isDynamicRevision(value):
			state = "partial"
		}
	case versionIdent:
		if resolved, ok := vals[d.version]; ok && !strings.Contains(d.version, ".") {
			value = resolved
		} else {
			state = "unresolved"
			unresolvedKeys = []string{d.version}
		}
	case versionOpaque:
		state = "unresolved"
		unresolvedKeys = []string{d.version}
	}

	section := "libraryDependencies"
	if d.config != "" {
		// The configuration rides in the section so the dev/runtime split is
		// visible the same way Gradle exposes testImplementation.
		section += ":" + d.config
	}
	row := map[string]any{
		"name":                        d.org + ":" + d.name,
		"line_number":                 d.line,
		"value":                       value,
		"section":                     section,
		"config_kind":                 "dependency",
		"package_manager":             LanguageName,
		"lang":                        LanguageName,
		"dependency_scope":            d.scope,
		"dependency_resolution_state": state,
		"direct_dependency":           true,
		"dependency_path_kind":        "manifest",
		"dependency_optional":         strings.EqualFold(d.config, "optional"),
		"sbt_cross_version":           d.cross,
	}
	if d.config != "" {
		row["sbt_configuration"] = d.config
	}
	if len(unresolvedKeys) > 0 {
		row["dependency_unresolved_keys"] = unresolvedKeys
	}
	if d.cross == crossBinary && binary != "" {
		row["sbt_binary_artifact"] = d.org + ":" + d.name + "_" + binary
	}
	return row
}
