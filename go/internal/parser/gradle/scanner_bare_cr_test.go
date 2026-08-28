// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gradle

import (
	"strings"
	"testing"
)

// TestStripCommentsEndsLineCommentAtBareCR is the #6268 regression for the
// Gradle scanner. A classic-Mac build script separates lines with a bare '\r'
// and carries no '\n', so a `//` scan that only stops at '\n' erases the rest
// of the file -- every dependency after the first comment silently disappears
// instead of failing loudly.
func TestStripCommentsEndsLineCommentAtBareCR(t *testing.T) {
	t.Parallel()

	source := "dependencies {\r// pinned by the platform team\rimplementation 'com.example:lib:1.2.3'\r}\r"
	stripped := stripCommentsAndStringInteriorsKept(source)
	if !strings.Contains(stripped, "com.example:lib:1.2.3") {
		t.Fatalf("stripCommentsAndStringInteriorsKept(%q) = %q, want the declaration after the comment kept", source, stripped)
	}
	if strings.Contains(stripped, "pinned by the platform team") {
		t.Fatalf("stripCommentsAndStringInteriorsKept(%q) = %q, want the comment body removed", source, stripped)
	}
	if got := strings.Count(stripped, "\r"); got != 4 {
		t.Fatalf("stripCommentsAndStringInteriorsKept(%q) kept %d '\\r' bytes, want 4: the terminator belongs to the line, not the comment", source, got)
	}
}

// TestStripCommentsKeepsCRLFSourceIntact is the control: CRLF build scripts
// already worked, so this stays green on both sides of the #6268 fix.
func TestStripCommentsKeepsCRLFSourceIntact(t *testing.T) {
	t.Parallel()

	source := "dependencies {\r\n// pinned by the platform team\r\nimplementation 'com.example:lib:1.2.3'\r\n}\r\n"
	stripped := stripCommentsAndStringInteriorsKept(source)
	if !strings.Contains(stripped, "com.example:lib:1.2.3") {
		t.Fatalf("stripCommentsAndStringInteriorsKept(CRLF) = %q, want the declaration kept", stripped)
	}
	if strings.Contains(stripped, "pinned by the platform team") {
		t.Fatalf("stripCommentsAndStringInteriorsKept(CRLF) = %q, want the comment body removed", stripped)
	}
}

// TestUnclosedStringTerminatesAtBareCR pins the sibling of the comment scan in
// the same function. copyStringLiteral's doc promises an unclosed single-line
// string ends "at the newline so subsequent lines parse cleanly rather than
// getting absorbed into a runaway string"; under bare-CR line endings that
// promise only holds once '\r' counts as a newline too (#6268).
//
// String interiors are copied verbatim, so a runaway string does not delete
// text -- it re-labels it. The observable damage is that everything it
// swallows stops being scanned, which is why this asserts on a comment that
// must still be stripped on the line after the unclosed quote rather than on
// the declaration text itself.
func TestUnclosedStringTerminatesAtBareCR(t *testing.T) {
	t.Parallel()

	source := "def a = 'unterminated\rdef b = 'x' // must still be stripped\r"
	stripped := stripCommentsAndStringInteriorsKept(source)
	if strings.Contains(stripped, "must still be stripped") {
		t.Fatalf("stripCommentsAndStringInteriorsKept(%q) = %q, want the later comment stripped rather than swallowed by a runaway string", source, stripped)
	}
	if !strings.Contains(stripped, "def b = 'x'") {
		t.Fatalf("stripCommentsAndStringInteriorsKept(%q) = %q, want the line after the unclosed quote kept", source, stripped)
	}
}
