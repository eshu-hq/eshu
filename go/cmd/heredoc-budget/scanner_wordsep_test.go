// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// This file guards the F4 fail-open (2026-07 hardening review): the
// word-separator set findAllOpeners uses to decide a genuine bash word
// boundary (for the trailing '#'-comment rule) only covered blank/`;`/`|`/`&`.
// Real bash treats a bare `(` (subshell open), a bare `)` (e.g. a case-pattern
// terminator, NOT the `)` that closes `$(...)`), a bare `<`, and a bare `>` as
// word-separating metacharacters too -- verified against real bash below. The
// old scanner's `default: i++` fallback for these bytes never set wordStart,
// so a '#' immediately after one of them was not recognized as a real
// comment, and the heredoc-opener-shaped fragment inside it phantom-opened
// the scanner, silently swallowing a real heredoc later in the file (0
// detected, exit 0).
//
// Word-separator set this fix settles on:
// blank (space, tab), `;`, `|`, `&` (pre-existing) plus `(`, `)`, `<`, `>`
// (this fix). Deliberately excludes quote characters, `$`, and the backtick,
// none of which end a bash word (`foo"bar"baz`, `foo$(cmd)baz` are each one
// concatenated word). The `)` that closes a `$(...)` substitution is
// explicitly NOT included -- real bash concatenates a substitution's result
// into the surrounding word (`$(true)#<<FAKE` is verified to phantom-open a
// REAL heredoc from the `<<FAKE` fragment, i.e. `)` there is NOT a word
// boundary), matching the existing `case c == ')' && top() == frameSubst`
// handling, which is intentionally left untouched by this fix.

// TestScanContent_HashAfterBareParenIsGenuineComment guards the literal F4
// repro: a bare `(` (subshell open) immediately followed by '#' starts a real
// bash comment.
//
// Verified against real bash:
//
//	(#<<NEVERCLOSES
//	echo x
//	)
//	cat <<REALBIG4
//	<601-byte body>
//	REALBIG4
//	echo done
//
// exits 0, printing the body then "done" -- exactly one real heredoc
// (REALBIG4); the `<<NEVERCLOSES` fragment is inert comment text.
func TestScanContent_HashAfterBareParenIsGenuineComment(t *testing.T) {
	body := strings.Repeat("Y", 600) + "\n" // 601 bytes, over budget
	src := "(#<<NEVERCLOSES\necho x\n)\ncat <<REALBIG4\n" + body + "REALBIG4\necho done\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected exactly the real REALBIG4 heredoc, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 4 {
		t.Fatalf("expected the real opener on line 4, got line %d", heredocs[0].Line)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_HashAfterBareCaseParenIsGenuineComment guards the bare `)`
// case: a case-pattern terminator (`*)`) is NOT the `)` that closes `$(...)`,
// and '#' right after it starts a real comment.
//
// Verified against real bash:
//
//	case x in
//	*)#<<NEVERCLOSES
//	true;;
//	esac
//	cat <<REALBIG
//	<601-byte body>
//	REALBIG
//	echo done
//
// exits 0 with exactly one real heredoc.
func TestScanContent_HashAfterBareCaseParenIsGenuineComment(t *testing.T) {
	body := strings.Repeat("Y", 600) + "\n"
	src := "case x in\n*)#<<NEVERCLOSES\ntrue;;\nesac\ncat <<REALBIG\n" + body + "REALBIG\necho done\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected exactly the real REALBIG heredoc, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 5 {
		t.Fatalf("expected the real opener on line 5, got line %d", heredocs[0].Line)
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}

// TestScanContent_CommandSubstitutionCloseParenIsNotAWordBoundary is the
// negative control: unlike a bare `)`, the `)` that closes `$(...)` must NOT
// set wordStart, because real bash concatenates a substitution's result into
// the surrounding word, so a '#' right after it is NOT a comment start.
//
// Verified against real bash: `echo $(true)#<<FAKE` followed by a real
// `cat <<REALBIG10` heredoc emits a
// "warning: here-document ... delimited by end-of-file (wanted `FAKE')" and
// prints only an empty line (from `echo $(true)`) -- proof `<<FAKE` was read
// as a REAL heredoc opener (not a comment), and that it swallowed
// EVERYTHING after it, including the "cat <<REALBIG10" line and "echo done",
// as its own (never-closed, since no line is literally "FAKE") body, all the
// way to actual EOF.
//
// ScanContent must reproduce that exact (surprising) shape: FAKE opens,
// consumes the rest of the file as its body, is dropped for having no
// matching close (see ScanContent's documented unterminated-opener
// behavior), and REALBIG10 is never independently seen at all -- so the
// correct result here is ZERO heredocs, not one. If this fix wrongly treated
// the substitution's closing `)` as a word boundary too, `#<<FAKE` would
// instead be read as a genuine comment, FAKE would never open, and
// REALBIG10 WOULD be found as its own real heredoc (1 result) -- so
// asserting zero here is exactly what pins the closing `)` as NOT a word
// boundary; a regression that adds one would flip this to 1.
func TestScanContent_CommandSubstitutionCloseParenIsNotAWordBoundary(t *testing.T) {
	body := strings.Repeat("Y", 600) + "\n"
	src := "echo $(true)#<<FAKE\ncat <<REALBIG10\n" + body + "REALBIG10\necho done\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 0 {
		t.Fatalf("expected zero heredocs (FAKE swallows the rest of the file unterminated, matching real bash), got %d: %+v", len(heredocs), heredocs)
	}
}

// TestScanContent_BareAngleBracketsAreWordBoundaries proves that a bare '<'
// or '>' -- not part of a `<<` heredoc opener -- is tracked as a word
// boundary, matching real bash's own tokenization rules. `cat <#<<FAKE` and
// `cat >#<<FAKE` are each verified real-bash SYNTAX ERRORS:
//
//	$ printf 'cat <#<<FAKE\n' | bash; echo $?
//	bash: line 1: syntax error near unexpected token `newline'
//	bash: line 1: `cat <#<<FAKE'
//	2
//	$ printf 'cat >#<<FAKE\n' | bash; echo $?
//	bash: line 1: syntax error near unexpected token `newline'
//	bash: line 1: `cat >#<<FAKE'
//	2
//
// Both fail at PARSE time because the mandatory redirection target was
// entirely swallowed by a comment -- proof bash's own lexer treats '#'
// immediately after a bare '<'/'>' as a genuine word start, the same
// word-boundary recognition this scanner must model, even though such a
// line can never be part of a runnable script (any real script containing
// it is already bash-invalid). The control case proves the contrast: a
// QUOTED target right after '<' is never treated as a comment, and is read
// (at runtime, not parse time) as an ordinary, if nonexistent, filename:
//
//	$ printf 'cat <"#nofile"\n' | bash; echo $?
//	bash: line 1: #nofile: No such file or directory
//	1
//
// This scanner is a static lexer, not a bash parser -- it does not reject
// the syntactically invalid line, it just must not let the `<<FAKE`
// fragment inside the (real, per the above) comment phantom-open a heredoc
// that swallows a later real one.
func TestScanContent_BareAngleBracketsAreWordBoundaries(t *testing.T) {
	body := strings.Repeat("Y", 600) + "\n"
	tests := []struct {
		name string
		line string
	}{
		{"bare_lt_then_hash", "cat <#<<FAKE"},
		{"bare_gt_then_hash", "cat >#<<FAKE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := tt.line + "\ncat <<REALEOF\n" + body + "REALEOF\n"
			heredocs := ScanContent(src)
			if len(heredocs) != 1 {
				t.Fatalf("expected only the real REALEOF heredoc, got %d: %+v", len(heredocs), heredocs)
			}
			if heredocs[0].Line != 2 {
				t.Fatalf("expected the real opener on line 2, got line %d", heredocs[0].Line)
			}
		})
	}
}
