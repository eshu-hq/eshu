// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import "strings"

// hasYAMLVersionDirective reports whether any source line is a "%YAML"
// version-directive line: "%YAML" at the start of the line followed by white
// space or the end of the line. The name match is case-insensitive because
// js-yaml 4.1.1 and PyYAML read "%yaml" and "%Yaml" too while yaml.v3 rejects
// every 1.2+ version. A bare "%" byte is not matched: URL-encoded values hold
// those and are not directives. An indented "%YAML" is not matched either: no
// probed parser reads it as a directive.
func hasYAMLVersionDirective(lines []string) bool {
	for _, line := range lines {
		if len(line) < len("%YAML") || line[0] != '%' {
			continue
		}
		if !strings.EqualFold(line[1:len("%YAML")], "YAML") {
			continue
		}
		if len(line) == len("%YAML") || line[len("%YAML")] == ' ' || line[len("%YAML")] == '\t' {
			return true
		}
	}
	return false
}
