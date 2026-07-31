// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// This file records the mandated adversarial-hunt probes run after fixing
// F1-F4 (2026-07 hardening review): `$((` nested in `$(`, `<<` inside
// `[[ ]]`, `#` after `!`, `<<` inside an array literal, and `<<-` with mixed
// tabs/spaces. Each was reproduced against real bash first; most matched
// this scanner's ALREADY-correct behavior post-fix (recorded here as
// regression guards), and are not new findings. Two probes DID find new
// gaps, tracked separately: the concatenated-quote delimiter case
// (TestScanContent_ConcatenatedQuotedSegmentInDelimiter in
// scanner_delim_test.go, fixed) and the legacy-backtick / split-after-"<<"
// gaps (pre-existing, still open, see doc.go).

// TestScanContent_ArithmeticNestedInsideCommandSubstitution is the reverse
// nesting direction from TestScanContent_NestedCommandSubstitutionInsideArithmeticStillRecognized
// (scanner_arith_test.go): `$((...))` appearing INSIDE `$(...)`, not the
// other way around.
//
// Verified against real bash:
//
//	x=$(echo $(( 1 << 2 )) )
//	cat <<REALP1
//	<601-byte body>
//	REALP1
//	echo done
//
// exits 0, printing the body then "done".
func TestScanContent_ArithmeticNestedInsideCommandSubstitution(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n"
	src := "x=$(echo $(( 1 << 2 )) )\ncat <<REALP1\n" + body + "REALP1\necho done\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected exactly the real REALP1 heredoc, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_HeredocAfterDoubleBracketCondition guards `<<` appearing
// on the same line right after a `[[ ]]` conditional (`[[`/`]]`/`&&` are not
// specially tracked by this scanner, so this is really confirming they do
// not interfere with ordinary heredoc-opener detection).
//
// Verified against real bash:
//
//	[[ 1 -lt 2 ]] && cat <<REALP2B
//	<601-byte body>
//	REALP2B
//	echo done
//
// exits 0.
func TestScanContent_HeredocAfterDoubleBracketCondition(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n"
	src := "[[ 1 -lt 2 ]] && cat <<REALP2B\n" + body + "REALP2B\necho done\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected exactly the real REALP2B heredoc, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_HashAfterBangIsNotAWordBoundary guards the "'#' after '!'"
// probe: '!' is deliberately NOT in isShellWordSeparator, so "!#" stays one
// word and the '#' inside it is NOT a comment start -- matching real bash,
// which reads "!#<<FAKE" as a single (unknown) command name "!#" followed by
// a REAL (if never-closed) heredoc redirection.
//
// Verified against real bash:
//
//	$ printf '!#<<FAKE\ncat <<REALP4\nbody\nREALP4\necho done\n' | bash; echo $?
//	bash: line 5: warning: here-document at line 1 delimited by end-of-file (wanted `FAKE')
//	bash: line 1: !#: command not found
//	127
//
// The warning proves FAKE genuinely opened and consumed the rest of the
// script (including "cat <<REALP4" and "echo done") as its own unterminated
// body -- so, exactly like TestScanContent_CommandSubstitutionCloseParenIsNotAWordBoundary
// in scanner_wordsep_test.go, the correct ScanContent result is ZERO
// heredocs, not the later REALP4 one. If '!' were ever wrongly added as a
// word separator, '#' would instead be read as a real comment and REALP4
// would be found as an independent heredoc (1 result) -- so this assertion
// pins '!' as NOT a word boundary.
func TestScanContent_HashAfterBangIsNotAWordBoundary(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n"
	src := "!#<<FAKE\ncat <<REALP4\n" + body + "REALP4\necho done\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 0 {
		t.Fatalf("expected zero heredocs (FAKE swallows the rest of the file unterminated, matching real bash), got %d: %+v", len(heredocs), heredocs)
	}
}

// TestScanContent_HeredocInsideArrayLiteralCommandSubstitution guards `<<`
// nested inside a command substitution that is itself inside an array
// literal assignment (`arr=($(cat <<EOF ... EOF))`). The array literal's
// parens are not specially tracked by this scanner (bash arrays are out of
// scope), so this confirms the nested `$(...)`/heredoc handling already in
// place is unaffected by the surrounding array syntax.
//
// Verified against real bash:
//
//	arr=($(cat <<REALP5
//	<601-byte body>
//	REALP5
//	))
//	echo ${#arr[@]}
//
// exits 0, printing "1" (one array element, the body's word-split content).
func TestScanContent_HeredocInsideArrayLiteralCommandSubstitution(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n"
	src := "arr=($(cat <<REALP5\n" + body + "REALP5\n))\necho done\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected exactly the real REALP5 heredoc, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_TabStripDoesNotStripLeadingSpaceBeforeTab guards `<<-`
// (tab-stripping) with MIXED tabs and spaces: POSIX (and real bash) strip
// only a LEADING RUN OF TABS, stopping at the first non-tab byte -- a space
// before a tab is never stripped through, so a closing line "space, tab,
// EOF" is NOT recognized as closing a "<<-EOF" heredoc.
//
// Verified against real bash:
//
//	$ printf 'cat <<-EOF\nbody\n \tEOF\necho after\n' | bash
//	bash: line 4: warning: here-document at line 1 delimited by end-of-file (wanted `EOF')
//	body
//	 	EOF
//	echo after
//
// (the heredoc never closes; its literal body runs to EOF). This is not a
// gap: closesHeredoc's `strings.TrimLeft(l, "\t")` already stops at the
// first non-tab byte, matching this exactly. This test pins the existing
// correct behavior as a named regression guard.
func TestScanContent_TabStripDoesNotStripLeadingSpaceBeforeTab(t *testing.T) {
	src := "cat <<-EOF\nbody\n \tEOF\necho after\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 0 {
		t.Fatalf("expected the <<-EOF heredoc to stay unterminated (space before tab must not be stripped), got %d: %+v", len(heredocs), heredocs)
	}
}

// TestScanContent_TabStripMixedTabsThenSpacesInBody is the companion case
// where tab-stripping DOES apply: leading tabs are stripped, but spaces
// AFTER them are not (POSIX strips only tabs, never blanks in general).
//
// Verified against real bash:
//
//	$ printf 'cat <<-EOF\n\t  indented with tab then spaces\n\tEOF\necho after\n' | bash
//	  indented with tab then spaces
//	after
func TestScanContent_TabStripMixedTabsThenSpacesInBody(t *testing.T) {
	src := "cat <<-EOF\n\t  indented with tab then spaces\n\tEOF\necho after\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size != len("\t  indented with tab then spaces\n") {
		t.Fatalf("expected the body size to include the un-stripped leading spaces, got %d", heredocs[0].Size)
	}
}
