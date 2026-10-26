// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import "strings"

// hasYAMLVersionDirective reports whether any source line is a "%"-directive
// line as js-yaml 4.1.1 reads it: a leading byte-order mark and leading
// horizontal white space are skipped, then "%" must be followed by a byte that
// is neither space nor tab nor a line break. js-yaml takes any such run as the
// directive name, warning on unknown names ("%FOO", "%YAM", "%ABC", "%x",
// "%YAMLx", "%YAMLé", "%YAMLL", "%%YAML") yet reading on, while yaml.v3
// rejects each (unknown directive name / expected directive name /
// non-alphabetical character), so the parsed walk is blind and only the
// lexical fallback covers them. A bare "%" and "% YAML" are not matched:
// js-yaml rejects both for an empty name, and yaml.v3 does too. Only a line
// starting with "%" counts, so URL-encoded values holding "%" mid-line
// ("abc%YAML 1.2", "x%20y") are not directives. A tab-indented directive is
// matched because js-yaml 4.1.1 reads it (yaml.v3 and PyYAML 6.0.3 reject it);
// a space-indented one and a bare "%YAML" are matched conservatively though
// every probed parser rejects them there. Only " \t\uFEFF" are stripped, so a
// vertical-whitespace- or NBSP-indented "%" stays a plain scalar, which the
// parsed walk already covers. A BOM-prefixed directive is matched: both
// downstream parsers read it while yaml.v3 rejects it.
func hasYAMLVersionDirective(lines []string) bool {
	for _, line := range lines {
		rest := strings.TrimLeft(line, " \t\uFEFF")
		if len(rest) < 2 || rest[0] != '%' {
			continue
		}
		if rest[1] == ' ' || rest[1] == '\t' || rest[1] == '\r' || rest[1] == '\n' {
			continue
		}
		return true
	}
	return false
}
