// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "strings"

// scalaBinaryVersion returns the artifact suffix sbt appends for `%%`:
// "2.13" for any 2.13.x, "2.10" through "2.12" likewise, "3" for any 3.x, and
// "" when the version is not a recognisable Scala release. Scala before 2.10
// had no binary-compatible minor line (sbt cross-built 2.9.x with the full
// version, such as `_2.9.2`), so it returns "". A version with a suffix
// (`2.13.0-M5`, `3.0.0-RC1`, `2.13.12-bin-typelevel-4`) returns "" because sbt
// does not always shorten those to the x.y form, and a wrong hint is worse
// than none.
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
		value := 0
		for index := 0; index < len(minor); index++ {
			if minor[index] < '0' || minor[index] > '9' || value > 1000 {
				return ""
			}
			value = value*10 + int(minor[index]-'0')
		}
		if value < 10 {
			return ""
		}
		return "2." + minor
	}
	return ""
}

// isDynamicRevision reports an Ivy dynamic revision that names no single
// version: a `1.+` prefix match, a `latest.release` status, or a range in any
// of Ivy's spellings (`[1.0,2.0]`, `[1.0,2.0[`, `]1.0,2.0]`, `(,2.0]`,
// `[1.0,)`). Gradle's parser reports such strings as written; sbt keeps them
// partial because the resolved version is chosen at update time.
func isDynamicRevision(version string) bool {
	return strings.HasSuffix(version, "+") ||
		strings.HasPrefix(version, "latest.") ||
		(version != "" && strings.ContainsAny(version[:1], "[]()")) ||
		strings.Contains(version, ",")
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
		if resolved, ok := vals[d.version]; ok && !d.chained && !strings.Contains(d.version, ".") {
			value = resolved
		} else {
			state = "unresolved"
			unresolvedKeys = []string{d.version}
		}
	case versionOpaque:
		state = "unresolved"
		unresolvedKeys = []string{d.version}
	}

	if d.scopeUndetermined() && state == "resolved" {
		// The statement did not match the grammar, a later assignment
		// superseded it, or the configuration could not be mapped: the scope
		// is a placeholder, so the row is partial, never a resolved compile.
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
		"dependency_optional":         d.optional,
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
