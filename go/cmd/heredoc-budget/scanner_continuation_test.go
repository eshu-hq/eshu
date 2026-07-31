// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// This file guards the F3 fail-open (2026-07 hardening review): a bare
// trailing backslash at the end of a physical line is a real bash line
// continuation (`\<newline>` is spliced away before tokenizing) everywhere
// EXCEPT inside a single-quoted string, where backslash has no escape
// meaning at all and the sequence is kept literally. The old scanner had no
// concept of this at all: ScanContent's full-line-comment shortcut and
// findAllOpeners's `wordStart := true` reset both restarted fresh on every
// PHYSICAL line, with no memory that the previous line ended in a bare
// trailing backslash. When the continuation fuses mid-word onto a next line
// that begins with '#', bash does NOT see a comment there (the '#' is fused
// onto the previous word, not at a word start) but the old scanner did,
// phantom-treating the fused line as a full-line comment and silently
// dropping everything after it, including a real over-budget heredoc.
//
// The fix handles this at the LINE-JOINING level (ScanContent), not by
// special-casing '#': before checking for a full-line comment or looking for
// openers, ScanContent now asks findAllOpeners whether the line ends in a
// splice-eligible dangling backslash and, if so, fuses the next physical
// line directly onto it (dropping the backslash, no separator) and rescans
// the FUSED text from scratch, repeating for consecutive continuations. Both
// the comment shortcut and the opener scan then see the same fused text a
// real bash parser would.

// TestScanContent_ContinuationFusesHashIntoRealCode is the literal F3 repro:
// a backslash-newline continuation fuses "echo hi" with a line beginning
// "#not a comment <<REALBIG", so bash does NOT treat it as a comment (the
// '#' is not at a word start once fused) and DOES open a real heredoc.
//
// Verified against real bash:
//
//	echo hi\
//	#not a comment <<REALBIG
//	<601-byte body>
//	REALBIG
//	echo done
//
// exits 0. The heredoc's body is never printed by `echo` (echo does not
// read stdin), but bash still opens and fully consumes it as a genuine
// redirection on the fused command line -- exactly the live pipe-buffer
// hang risk this gate exists to catch, and "echo done" runs afterward,
// proving REALBIG was read and closed normally rather than left
// unterminated.
func TestScanContent_ContinuationFusesHashIntoRealCode(t *testing.T) {
	body := strings.Repeat("Y", 600) + "\n" // 601 bytes, over budget
	src := "echo hi\\\n#not a comment <<REALBIG\n" + body + "REALBIG\necho done\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected exactly the real REALBIG heredoc, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 1 {
		t.Fatalf("expected the fused opener reported at its starting line 1, got line %d", heredocs[0].Line)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_ContinuationInsideSingleQuoteNotSpliced guards the
// documented exception: backslash has NO escape meaning inside a
// single-quoted string, so a trailing backslash there is literal text, never
// a continuation. Verified against real bash: `echo 'a\` + newline + `b' #
// <<FAKE` prints "a\nb # <<FAKE" verbatim -- i.e. real bash keeps BOTH the
// backslash and the newline as literal characters inside the single-quoted
// string (unlike the double-quote case, which splices them away). If this
// fix wrongly spliced inside single quotes too, the trailing "# <<FAKE"
// (real, ordinary text belonging to the echo argument, not a comment or a
// heredoc) risks being mis-scanned once fused onto a different position.
// This test pins that a real heredoc appearing later in the file is still
// found at its own, un-fused line, proving the single-quoted continuation
// was correctly left alone (not spliced, not phantom-anything).
func TestScanContent_ContinuationInsideSingleQuoteNotSpliced(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n"
	src := "echo 'a\\\nb' # <<FAKE\ncat <<REALEOF\n" + body + "REALEOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected only the real REALEOF heredoc, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 3 {
		t.Fatalf("expected the real opener on line 3, got line %d", heredocs[0].Line)
	}
}

// TestScanContent_ContinuationInsideDoubleQuoteAlsoFuses is the positive
// double-quote control (also exercised indirectly by the existing
// TestScanContent_DoubleQuoteSpanningMultipleLinesTracksAcrossLines, which
// passed before this fix only because being inside an open double-quoted
// string already suppressed heredoc/comment detection regardless of line
// joining). This test targets the line-joining behavior itself: the fused
// text must match what real bash actually produces (verified: `echo "line
// one \` + newline + `continues"` prints "line one continues" -- both the
// backslash AND the newline are removed, leaving exactly one space, matching
// the trailing space already present before the backslash on line one).
func TestScanContent_ContinuationInsideDoubleQuoteAlsoFuses(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n"
	src := "echo \"line one \\\ncontinues\"\ncat <<EOF\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the real EOF heredoc, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 3 {
		t.Fatalf("expected the real opener on line 3, got line %d", heredocs[0].Line)
	}
}

// TestScanContent_MultipleConsecutiveContinuationsFused guards that fusion
// repeats across more than one continuation in a row, matching real bash
// (each `\<newline>` splices independently; verified: three chained
// continuations fuse into one logical line, e.g. "a\<nl>b\<nl>c" runs as a
// single command "abc").
func TestScanContent_MultipleConsecutiveContinuationsFused(t *testing.T) {
	body := strings.Repeat("Y", 600) + "\n"
	src := "echo a\\\nb\\\nc <<REALBIG\n" + body + "REALBIG\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected exactly one heredoc from the triple-fused line, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 1 {
		t.Fatalf("expected the fused opener reported at its starting line 1, got line %d", heredocs[0].Line)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_ContinuationSplitRightAfterHeredocOperatorNowClosed guards
// a PRE-EXISTING gap this fix closes as a side effect, not one of the four
// assigned bugs: doc.go's "Still open" list previously documented that a
// heredoc opener split immediately after `<<`/`<<-` by a backslash-newline
// continuation (`cat <<\` then a newline, then the delimiter on the next
// physical line) opened a real heredoc in real bash that the old
// line-at-a-time scanner never saw. Because this fix's line-joining is
// general (it fuses on ANY dangling trailing backslash outside a
// single-quoted string, not just the '#'-after-continuation shape), it also
// fuses this construct into one logical "cat <<EOF" line, closing the gap.
//
// Verified against real bash:
//
//	cat <<\
//	EOF
//	<601-byte body>
//	EOF
//
// prints the body and exits 0.
func TestScanContent_ContinuationSplitRightAfterHeredocOperatorNowClosed(t *testing.T) {
	body := strings.Repeat("Y", 600) + "\n"
	src := "cat <<\\\nEOF\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the split-opener heredoc to now be detected, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}
