// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "strings"

// scalaBinaryVersion returns the artifact suffix sbt appends for `%%`:
// "2.13" for any 2.13.x, "3" for any 3.x, and "" when the version is not a
// recognisable Scala release. A version with a suffix (`2.13.0-M5`,
// `3.0.0-RC1`, `2.13.12-bin-typelevel-4`) returns "" because sbt does not
// always shorten those to the x.y form, and a wrong hint is worse than none.
func scalaBinaryVersion(version string) string {
	if strings.Contains(version, "-") {
		return ""
	}
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

	if !d.scopeKnown && state == "resolved" {
		// The configuration or axis could not be mapped, so the scope is a
		// placeholder; the row is partial rather than a resolved compile scope.
		state = "partial"
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
		"dependency_optional":         hasConfigPart(d.config, "optional"),
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
