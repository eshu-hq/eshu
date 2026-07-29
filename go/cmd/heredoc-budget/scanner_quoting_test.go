// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// This file guards three quote-tracking false negatives found in adversarial
// review of the #5079 fix, each independently reproduced against real
// /bin/bash before being encoded here: a real, over-budget heredoc is
// silently swallowed (0 detected where 1 exists) because findAllOpeners's
// per-line quote tracking does not model how bash actually scopes quoting.
// All three share one root cause and one fix: quote/substitution context
// must persist across physical lines, and `$(...)` must be recognized as a
// fresh, unquoted lexical scope even while nested inside an outer
// double-quoted string.

// TestScanContent_AnsiCQuoteEscapedApostropheNotMisreadAsClose guards the
// ANSI-C ($'...') false negative: `\'` inside a $'...' string is an escaped
// quote, not a string terminator. The old plain single-quote toggle has no
// escape awareness, so it closes the string early at the escaped `'` and
// treats the trailing `<<X` as a real opener, which then swallows the real
// heredoc that follows.
//
// Verified against real bash: `echo $'a\'b <<X inside ansi-c string'` opens
// no heredoc at all; the following `cat <<EOF ... EOF` is the only heredoc.
func TestScanContent_AnsiCQuoteEscapedApostropheNotMisreadAsClose(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n" // over budget
	src := `echo $'a\'b <<X inside ansi-c string'` + "\ncat <<EOF\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the real EOF heredoc to be detected despite the $'...' string, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 2 {
		t.Fatalf("expected the real opener on line 2, got line %d", heredocs[0].Line)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_CommandSubstitutionInsideDoubleQuoteRecognizesHeredoc
// guards the command-substitution false negative: `$(...)` opens a fresh,
// unquoted lexical scope in real bash even while nested inside an outer
// double-quoted string that has not closed yet — command substitution is
// NOT suppressed inside double quotes (only inside single quotes). The old
// scanner had no concept of `$(...)` at all, so once inside an outer `"`,
// every subsequent `<<` was (wrongly) treated as string content forever,
// silently swallowing a real heredoc nested inside the substitution.
//
// Verified against real bash:
//
//	echo "prefix $(cat <<Y
//	<body>
//	Y
//	) suffix"
//
// prints "prefix <body> suffix" — the heredoc opens and is read normally.
func TestScanContent_CommandSubstitutionInsideDoubleQuoteRecognizesHeredoc(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n" // over budget
	src := "echo \"prefix $(cat <<Y\n" + body + "Y\n) suffix\"\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the heredoc nested in $(...) inside the outer double quote to be detected, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 1 {
		t.Fatalf("expected the opener on line 1, got line %d", heredocs[0].Line)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_DoubleQuoteSpanningMultipleLinesTracksAcrossLines guards
// the cross-line false negative: a double-quoted string that spans more
// than one physical line (with or without a trailing backslash-continuation)
// must stay "quoted" on the later lines too. The old scanner reset its quote
// state to unquoted on every call to findAllOpeners (once per line), so the
// second physical line of a multi-line string started unquoted and a
// `<<IDENT` there was wrongly treated as a real opener, silently swallowing
// whatever real heredoc followed.
//
// Verified against real bash (backslash-continuation form):
//
//	echo "line one \
//	continues <<X here"
//	cat <<EOF
//	<body>
//	EOF
//
// prints "line one continues <<X here" then the EOF heredoc body -- exactly
// one heredoc, not the <<X.
func TestScanContent_DoubleQuoteSpanningMultipleLinesTracksAcrossLines(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n" // over budget
	src := "echo \"line one \\\ncontinues <<X here\"\ncat <<EOF\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the real EOF heredoc to be detected despite the multi-line double-quoted string, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 3 {
		t.Fatalf("expected the real opener on line 3, got line %d", heredocs[0].Line)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_DoubleQuoteSpanningLinesWithoutBackslashAlsoTracked is the
// plain (non-backslash-continuation) variant: double quotes can span
// multiple physical lines in bash with no continuation marker at all (the
// embedded newline is just part of the string). This must be tracked
// identically to the backslash-continuation case above.
func TestScanContent_DoubleQuoteSpanningLinesWithoutBackslashAlsoTracked(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n"
	src := "echo \"line one\ncontinues <<X here\"\ncat <<EOF\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the real EOF heredoc to be detected despite the multi-line double-quoted string, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 3 {
		t.Fatalf("expected the real opener on line 3, got line %d", heredocs[0].Line)
	}
}

// TestScanContent_EmbeddedSingleQuoteIdiomDoesNotDesyncStack guards a real
// regression found while proving the quote-stack fix above against this
// repo's own scripts/**/*.sh: scripts/verify-remote-e2e-remediation-benchmark.sh
// uses the extremely common bash idiom for embedding a literal `'` inside a
// single-quoted string — close (`'`), escaped literal quote (`\'`), reopen
// (`'`) — twice in one regex literal. Without backslash-escape awareness at
// the unquoted/base level, the scanner misreads the escaped `\'` as opening
// a fresh quote frame that the immediately following reopen-`'` instantly
// closes again, landing back at "base" one idiom-cycle too early. A literal
// `"` that is still (per real bash) inside the reopened single-quoted string
// then gets misread as a real double-quote open that never finds its
// closing `"` for the rest of the line, desyncing the stack for the entire
// rest of the file and silently swallowing a real heredoc dozens of lines
// later (0 detected where 1 exists) — the same dangerous fail-open shape as
// the other quoting bugs in this file, just triggered by ordinary,
// widely-used shell quoting rather than an exotic construct.
func TestScanContent_EmbeddedSingleQuoteIdiomDoesNotDesyncStack(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n" // over budget
	// Mirrors the real fixture: a single-quoted regex containing the
	// close/escaped-quote/reopen idiom, with a literal `"` immediately after
	// the escaped quote (exactly where the desync happened), followed by a
	// real heredoc that must still be found.
	src := `rg 'a[[:space:]"'\''])b'` + "; then\ncat <<EOF\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the real EOF heredoc to survive the embedded-quote idiom, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 2 {
		t.Fatalf("expected the real opener on line 2, got line %d", heredocs[0].Line)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_CommentLineInsideMultiLineQuoteNotSkipped guards a
// corollary of the cross-line fix: the full-line "#" comment shortcut must
// not fire while a multi-line double-quoted string is still open, or a
// "#..."-shaped continuation line would be skipped instead of scanned for
// the closing quote, leaking the open-quote state past where the string
// actually ends.
func TestScanContent_CommentLineInsideMultiLineQuoteNotSkipped(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n"
	src := "echo \"line one\n# not a real comment, still inside the string\"\ncat <<EOF\n" + body + "EOF\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected the real EOF heredoc to be detected, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 3 {
		t.Fatalf("expected the real opener on line 3, got line %d", heredocs[0].Line)
	}
}
