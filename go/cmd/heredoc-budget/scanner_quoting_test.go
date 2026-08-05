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
//
// The `<<X` fragment inside the `$'...'` string is deliberate and kept by
// intent, though the proof does not depend on it: strip the fragment and the
// escape-awareness mutations still red this test, because the trailing `'`
// alone opens a persistent single-quote frame that swallows the real
// `cat <<EOF`. It is kept because it is the package's only in-fixture record
// of #5079's ANSI-C string-literal sub-case, and it should not be simplified
// out of the fixture.
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

// TestScanContent_HashNotStartingWordStaysLiteral guards the flip side of
// TestScanContent_TrailingCommentOpenerDoesNotHideRealHeredoc: an unquoted
// '#' that does NOT start a word is ordinary bash text, not a comment start,
// and must not swallow a real heredoc opener that follows it on the SAME
// line. Each case here was verified against real /bin/bash (all five open a
// real heredoc with no hang and no misparse):
//
//	echo foo#bar <<EOF        -> prints "foo#bar", heredoc opens normally
//	echo ${x#pat} <<EOF       -> unquoted param-expansion '#', heredoc opens
//	echo $# <<EOF             -> positional-count '#', heredoc opens
//	echo 'a # b' <<EOF        -> '#' inert inside single quotes, heredoc opens
//	echo "a # b" <<EOF        -> '#' inert inside double quotes, heredoc opens
//
// If the scanner's start-of-word check were wrong (e.g. treating any '#' as
// a comment start regardless of what precedes it), every one of these would
// regress to the same fail-open this file's other tests guard against: the
// trailing `<<EOF` would be swallowed as comment text and the real heredoc
// would go undetected.
// TestScanContent_EscapedWhitespaceBeforeHashStaysLiteral guards a P1
// REGRESSION introduced by the trailing-comment fix above (the
// `TestScanContent_HashNotStartingWordStaysLiteral` cases were all proven
// correct at the time, but none of them exercised a backslash-escaped
// whitespace byte immediately before `#`). The old check inferred
// "word-starting" by reading the raw byte at line[i-1], which cannot tell a
// REAL separator apart from one that was already consumed as the second half
// of a backslash-escape pair: the escape branch (`case c == '\\' && i+1 <
// len(line): i += 2`) advances `i` by two, but the escaped byte is still
// physically sitting at line[i-1] once the loop reaches the following `#`.
// In real bash, a backslash-escaped blank does not end the current word, so
// the `#` right after it does not start a new word either -- it is ordinary
// text, and a heredoc opener later on the same line is still real.
//
// Verified against real /bin/bash (transcript captured during the fix):
//
//	$ printf 'echo x\\ #<<EOF\nBODY9\nEOF\n' > t1.sh; bash t1.sh; echo $?
//	x #
//	0
//	$ printf 'echo x\\\t#<<EOF\nBODY9\nEOF\n' > t2.sh; bash t2.sh; echo $?
//	x	#
//	0
//
// Both runs print the echoed word and exit 0 with no "command not found" --
// proof the heredoc genuinely opened and its body/close were consumed
// normally, exactly as findAllOpeners must now report.
func TestScanContent_EscapedWhitespaceBeforeHashStaysLiteral(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"escaped_space_before_hash", `echo x\ #<<EOF`},
		{"escaped_tab_before_hash", "echo x\\\t#<<EOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.Repeat("z", 600) + "\n" // over budget
			src := tt.line + "\n" + body + "EOF\n"

			heredocs := ScanContent(src)

			if len(heredocs) != 1 {
				t.Fatalf("expected the real EOF heredoc to be detected despite the escaped whitespace before '#', got %d: %+v", len(heredocs), heredocs)
			}
			if heredocs[0].Size <= defaultBudget {
				t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
			}
		})
	}
}

// TestScanContent_RealWordBoundaryBeforeHashIsGenuineComment is the
// mirror-image guard: a REAL (unescaped) blank, the start of the line, or an
// unquoted statement-separator operator (`;`, `|`, `&`) immediately before
// `#` genuinely starts a bash comment, so a heredoc-opener-shaped fragment
// trailing it on the SAME line must stay inert -- while a separate, real
// heredoc later in the file must still be found. This is the fail-open this
// gate exists to prevent: if the word-start check were ever loosened to
// treat every non-alphanumeric byte as "not word-starting" (over-fixing the
// escape case above), a genuine comment could wrongly keep scanning past
// `#`, phantom-opening on the fragment and desyncing the scanner so the real
// heredoc below is silently dropped (0 detected, exit 0).
//
// Verified against real /bin/bash (each construct's own line, run standalone
// as `<construct>\nBODY9\nEOF\n`; a real comment leaves no heredoc open, so
// BODY9/EOF are read back as two ordinary, unknown commands):
//
//	$ printf 'echo x #<<EOF\nBODY9\nEOF\n' | bash; echo $?
//	x
//	bash: line 2: BODY9: command not found
//	bash: line 3: EOF: command not found
//	127
//	$ printf '#<<EOF\nBODY9\nEOF\n' | bash; echo $?
//	bash: line 2: BODY9: command not found
//	bash: line 3: EOF: command not found
//	127
//	$ printf 'true;#<<EOF\nBODY9\nEOF\n' | bash; echo $?
//	bash: line 2: BODY9: command not found
//	bash: line 3: EOF: command not found
//	127
//	$ printf 'true|#<<EOF\nBODY9\nEOF\n' | bash; echo $?
//	bash: line 2: BODY9: command not found
//	bash: line 3: EOF: command not found
//	127
//
// Every case exits 127 with "command not found" for BODY9 and EOF -- proof
// no heredoc opened, i.e. the `#` really did start a comment that swallowed
// the rest of its line, including the `<<EOF`-shaped fragment.
func TestScanContent_RealWordBoundaryBeforeHashIsGenuineComment(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"real_unescaped_space_before_hash", "echo x #<<FAKE"},
		{"hash_at_start_of_line", "#<<FAKE"},
		{"hash_after_semicolon", "true;#<<FAKE"},
		{"hash_after_pipe", "true|#<<FAKE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.Repeat("z", 600) + "\n" // over budget
			// The commented line's own "<<FAKE" must never open a heredoc;
			// the real, differently-named "REALEOF" heredoc below it must
			// still be the only one found.
			src := tt.line + "\ncat <<REALEOF\n" + body + "REALEOF\n"

			heredocs := ScanContent(src)

			if len(heredocs) != 1 {
				t.Fatalf("expected only the real REALEOF heredoc, got %d: %+v", len(heredocs), heredocs)
			}
			if heredocs[0].Line != 2 {
				t.Fatalf("expected the real opener on line 2, got line %d", heredocs[0].Line)
			}
			if heredocs[0].Size <= defaultBudget {
				t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
			}
		})
	}
}

func TestScanContent_HashNotStartingWordStaysLiteral(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"mid_word_hash", "echo foo#bar <<EOF"},
		{"unquoted_param_expansion_hash", "x=foobar\necho ${x#foo} <<EOF"},
		{"positional_count_hash", `echo $# <<EOF`},
		{"hash_inside_single_quotes", "echo 'a # b' <<EOF"},
		{"hash_inside_double_quotes", `echo "a # b" <<EOF`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.Repeat("z", 600) + "\n" // over budget
			src := tt.line + "\n" + body + "EOF\n"

			heredocs := ScanContent(src)

			if len(heredocs) != 1 {
				t.Fatalf("expected the real EOF heredoc to be detected despite the non-comment '#', got %d: %+v", len(heredocs), heredocs)
			}
			if heredocs[0].Size <= defaultBudget {
				t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
			}
		})
	}
}

// TestScanContent_QuoteCharacterInsideOtherQuoteTypeStaysInert guards #5079's
// "quote inside the other quote type" case explicitly: a `"` byte appearing
// inside a `'...'` string, and a `'` byte appearing inside a `"..."` string,
// must both be inert (real bash never treats the "wrong" quote character as
// special once already inside a string of the other kind -- only the
// matching quote closes it). findAllOpeners already gets this right BY
// CONSTRUCTION: the frameSingle case's switch has no branch for `"`, and the
// frameDouble case's switch has no branch for `'`, so an off-type quote byte
// just falls through to the default `i++` in each case. This test locks that
// in as a named regression guard rather than leaving it as an unstated
// property of the code.
//
// Each case is a SINGLE line with the real heredoc opener chained after the
// string via `&&`, so a lexer that wrongly nested the off-type quote (pushing
// a second frame instead of treating it as literal) can be told apart from
// correct behavior without spanning lines: the erroneous nested frame would
// swallow the string's own real closing quote as inert content (since the
// wrong frame is listening for the OTHER quote character), leaving the stack
// corrupted for the rest of the line -- so the real `<<EOF` opener that
// follows is never reached in the base/default lexer context that heredoc
// detection requires, and 0 heredocs are found instead of 1. Reproduced
// against real /bin/bash first: `echo 'abc "def' && cat <<EOF` and `echo
// "abc 'def" && cat <<EOF` each print their literal string (embedded
// off-type quote included) and then read the heredoc normally.
func TestScanContent_QuoteCharacterInsideOtherQuoteTypeStaysInert(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"double_quote_inside_single_quoted_string", `echo 'abc "def' && cat <<EOF`},
		{"single_quote_inside_double_quoted_string", `echo "abc 'def" && cat <<EOF`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.Repeat("z", 600) + "\n" // over budget
			src := tt.line + "\n" + body + "EOF\n"

			heredocs := ScanContent(src)

			if len(heredocs) != 1 {
				t.Fatalf("expected the real EOF heredoc to be detected despite the off-type quote byte inside the string, got %d: %+v", len(heredocs), heredocs)
			}
			if heredocs[0].Line != 1 {
				t.Fatalf("expected the opener on line 1 (chained after the string via &&), got line %d", heredocs[0].Line)
			}
			if heredocs[0].Size <= defaultBudget {
				t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
			}
		})
	}
}
