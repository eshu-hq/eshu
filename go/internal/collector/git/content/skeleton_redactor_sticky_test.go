// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"strings"
	"testing"
)

// TestRedactSkeletonFailureNeverClears checks that nothing after a lexical
// failure brings the scan back to code, not a block-comment close, a quote, a
// heredoc marker, or a brace. The frames the failure leaves open must not be
// popped by later lines (ruling 5, decision 3).
func TestRedactSkeletonFailureNeverClears(t *testing.T) {
	t.Parallel()

	const rest = "Passw0rdHunter2: 1\nn = 2\n"
	for name, in := range map[string]string{
		"a comment close then a quote":     "x = \"abc\n*/\n\"\n" + rest,
		"a quote then a quote":             "x = \"abc\n\"\n\"\n" + rest,
		"a heredoc marker then a quote":    "x = \"abc\nEOF\n\"\n" + rest,
		"a brace pair then a quote":        "x = \"abc\n{ }\n\"\n" + rest,
		"a bare CR in a string, then both": "x = \"a\rb\n*/\n\"\n" + rest,
	} {
		out := skeletonForTest(t, in)
		lines := strings.Split(out, "\n")
		for i, line := range lines {
			if line != "" && !isWholeLine(line) {
				t.Errorf("%s: line %d kept %q\n out %q", name, i+1, line, out)
			}
		}
	}
}
