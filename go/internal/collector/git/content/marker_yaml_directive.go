// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import "strings"

// hasYAMLVersionDirective reports whether any source line is a "%"-directive
// line as js-yaml 4.1.1 reads it: each "\n"-terminated source line is first
// split on the remaining breaks both downstream parsers honor (a carriage
// return, NEL U+0085, LS U+2028, PS U+2029), then a leading byte-order mark
// and leading horizontal white space are skipped, and "%" must be followed by
// a byte that is neither space nor tab. js-yaml takes any such run as the
// directive name, warning on unknown names ("%FOO", "%YAM", "%ABC", "%x",
// "%YAMLx", "%YAMLé", "%YAMLL", "%%YAML") yet reading on, while yaml.v3
// rejects each (unknown directive name / expected directive name /
// non-alphabetical character), so the parsed walk is blind and only the
// lexical fallback covers them. A bare "%" and "% YAML" are not matched:
// js-yaml rejects both for an empty name, and yaml.v3 does too. Only a
// break-separated segment starting with "%" counts, so URL-encoded values
// holding "%" mid-line ("abc%YAML 1.2", "x%20y") are not directives. A
// tab-indented directive is matched because js-yaml 4.1.1 reads it (yaml.v3
// and PyYAML 6.0.3 reject it); a space-indented one and a bare "%YAML" are
// matched conservatively though every probed parser rejects them there. A
// break-prefixed directive ("\r", NEL, LS, or PS before "%", or a directive
// on a later break-separated segment) is matched because both downstream
// parsers read it as a directive line while yaml.v3 rejects the body (found
// incompatible YAML document), so the parsed walk covers nothing and only the
// lexical fallback covers it. A VT-, FF-, or NBSP-indented "%" has no reader
// in the probed parsers and stays unmatched. A BOM-prefixed directive is
// matched: both downstream parsers read it while yaml.v3 rejects it.
func hasYAMLVersionDirective(lines []string) bool {
	for _, line := range lines {
		for _, segment := range splitYAMLBreaks(line) {
			rest := strings.TrimLeft(segment, " \t\uFEFF")
			if len(rest) < 2 || rest[0] != '%' {
				continue
			}
			if rest[1] == ' ' || rest[1] == '\t' {
				continue
			}
			return true
		}
	}
	return false
}

// splitYAMLBreaks splits a "\n"-terminated source line on the line breaks the
// sole caller's split misses yet js-yaml 4.1.1 and PyYAML 6.0.3 honor as line
// breaks before a directive: a carriage return, NEL U+0085, LS U+2028, and PS
// U+2029. Segments hold none of those bytes, so the "%"-name check in
// hasYAMLVersionDirective needs no per-break exclusion after the split.
func splitYAMLBreaks(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool {
		return r == '\r' || r == '\u0085' || r == '\u2028' || r == '\u2029'
	})
}
