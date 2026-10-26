// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// hasYAMLVersionDirective reports whether any source line is a "%YAML"
// version-directive line as js-yaml 4.1.1 reads it: a leading byte-order mark
// and leading horizontal white space are skipped, then "%YAML"
// (case-insensitive: js-yaml 4.1.1 and PyYAML read "%yaml" too while yaml.v3
// rejects every 1.2+ version) must be followed by a non-letter or the end of
// the line, so "%YAML1.2" matches and "%YAMLx" does not. A bare "%" byte is
// not matched: only a line starting with "%YAML" counts, so URL-encoded
// values holding "%" mid-line are not directives. A tab-indented "%YAML" is
// matched because js-yaml 4.1.1 reads it as a directive (yaml.v3 and PyYAML
// 6.0.3 reject it); a space-indented one is matched conservatively though
// every probed parser rejects it there. A BOM-prefixed directive is matched:
// both downstream parsers read it while yaml.v3 rejects it.
func hasYAMLVersionDirective(lines []string) bool {
	for _, line := range lines {
		rest := strings.TrimLeft(line, " \t\uFEFF")
		if len(rest) < len("%YAML") || rest[0] != '%' {
			continue
		}
		if !strings.EqualFold(rest[1:len("%YAML")], "YAML") {
			continue
		}
		if len(rest) == len("%YAML") {
			return true
		}
		if r, _ := utf8.DecodeRuneInString(rest[len("%YAML"):]); !unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
