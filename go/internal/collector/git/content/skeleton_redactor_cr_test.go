// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"strings"
	"testing"
)

// TestRedactSkeletonBareCarriageReturn covers a \r that is not the end of a
// CRLF line (rev-8833 round 5, J1). hclsyntax lexes a bare \r inside a quoted
// string as TokenQuotedNewline and inside heredoc text as TokenInvalid, and in
// the second case it stops lexing. The scanner treats both as a lexical failure:
// every later line goes whole. A \r in code or in a comment is no failure.
func TestRedactSkeletonBareCarriageReturn(t *testing.T) {
	t.Parallel()

	const secret = "Passw0rdHunter2: 1"
	cases := []struct {
		name string
		in   string
		// wholeFrom is the 0-based line from which every line must be whole.
		wholeFrom int
	}{
		{"bare CR as the first heredoc text", "A=<<EOF\n\rEOF\n" + secret + "\nEOF\nn = 2\n", 1},
		{"bare CR inside heredoc text", "A = <<EOF\na\rb\nEOF\nn = 2\n" + secret + "\n", 1},
		{"bare CR in heredoc text that holds a dollar", "A = <<EOF\n$x \r $$y\nEOF\nn = 2\n" + secret + "\n", 1},
		{"bare CR after an interpolation in heredoc text", "A = <<EOF\n${a}\rb\nEOF\nn = 2\n" + secret + "\n", 1},
		{"bare CR inside a quoted string", "x = \"a\rb\"\n" + secret + "\nn = 2\n", 1},
		{"bare CR inside a string in an interpolation", "x = \"${ \"a\rb\" }\"\n" + secret + "\n", 1},
		{"bare CR in a heredoc inside an interpolation", "x = \"${ <<IN\na\rb\nIN\n}\"\n" + secret + "\n", 1},
		{"CR after a trailing CR in heredoc text", "x = <<EOF\r\na\r\r\nEOF\r\n" + secret + "\r\n", 1},
		{"CR after the escape in a string", "x = \"a\\\rb\"\n" + secret + "\n", 1},
	}
	for _, tc := range cases {
		out := skeletonForTest(t, tc.in)
		lines := strings.Split(out, "\n")
		if strings.Contains(out, "Passw0rdHunter2") {
			t.Errorf("%s: secret survived: %q", tc.name, out)
		}
		for i := tc.wholeFrom; i < len(lines); i++ {
			if !isWholeLine(lines[i]) {
				t.Errorf("%s: line %d kept %q\n out %q", tc.name, i+1, lines[i], out)
			}
		}
	}

	// A \r that is no lexical failure leaves the following lines to the normal
	// rules: CRLF endings, and a bare \r in code or in a comment.
	for name, tc := range map[string]struct{ in, want string }{
		"CRLF heredoc":              {"x = <<EOF\r\nPassw0rdHunter2: 1\r\nEOF\r\nn = 2\r\n", "x = <<EOF\r\nREDACTED_value\r\nEOF\r\nn = 2\r\n"},
		"bare CR in a line comment": {"# a\rb\nn = 2\n", "REDACTED_value\nn = 2\n"},
		"bare CR in code":           {"n = 2\r\r\nm = 3\n", "n = REDACTED_value\r\nm = 3\n"},
	} {
		if got := skeletonForTest(t, tc.in); got != tc.want {
			t.Errorf("%s:\n got  %q\n want %q", name, got, tc.want)
		}
	}
}
