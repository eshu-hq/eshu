// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auditpreflight

import "strings"

// replaceSection swaps the body of a "### heading" section with newValue for
// tests, preserving the rest of the issue body.
func replaceSection(body, heading, newValue string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	inTarget := false
	for _, line := range lines {
		if rest, ok := strings.CutPrefix(line, "### "); ok {
			if inTarget {
				inTarget = false
			}
			if strings.EqualFold(strings.TrimSpace(rest), heading) {
				out = append(out, line, "", newValue, "")
				inTarget = true
				continue
			}
		}
		if inTarget {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
