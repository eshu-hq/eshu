// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// This file guards the F2 fail-open (2026-07 hardening review): parseDelim's
// unquoted-delimiter scan used isIdentByte, an [A-Za-z_][A-Za-z0-9_]*
// identifier approximation, to decide where the delimiter word ends. Real
// bash reads the next WORD as the delimiter and stops only at a genuine word
// separator (see isShellWordSeparator) -- a '#' mid-word is ordinary text
// there, exactly like anywhere else in bash. `cat <<E#F` truncated to
// delimiter "E" under the old rule, so the real closing line "E#F" never
// matched, and the heredoc (plus everything after it, including a real
// over-budget heredoc) was silently dropped as unterminated -- 0 detected,
// exit 0.
//
// Verified against real bash:
//
//	cat <<E#F
//	body line
//	E#F
//	cat <<REALBIG2
//	<601-byte body>
//	REALBIG2
//	echo done
//
// exits 0, printing "body line" then the 601-byte body then "done" -- two
// real heredocs.

func TestScanContent_HashInUnquotedDelimiterNotTruncated(t *testing.T) {
	body := strings.Repeat("Y", 600) + "\n" // 601 bytes, over budget
	src := "cat <<E#F\nbody line\nE#F\ncat <<REALBIG2\n" + body + "REALBIG2\necho done\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 2 {
		t.Fatalf("expected both the E#F and REALBIG2 heredocs, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Line != 1 {
		t.Fatalf("expected the E#F opener on line 1, got line %d", heredocs[0].Line)
	}
	if heredocs[1].Line != 4 {
		t.Fatalf("expected the REALBIG2 opener on line 4, got line %d", heredocs[1].Line)
	}
	if heredocs[1].Size <= defaultBudget {
		t.Fatalf("expected the REALBIG2 body over budget, got %d", heredocs[1].Size)
	}
}

// TestScanContent_HashStartingDelimiterIsCommentNotDelimiter is the negative
// control: the position right after "<<"/"<<-" (and any blanks) is ITSELF a
// word start, so an UNESCAPED '#' there begins a real comment, not a
// delimiter starting with '#'.
//
// Verified against real bash: `cat <<#FOO` is a SYNTAX ERROR (the comment
// eats the would-be delimiter, leaving "<<" with none) --
//
//	$ printf 'cat <<#FOO\n' | bash; echo $?
//	bash: line 1: syntax error near unexpected token `newline'
//	2
//
// -- so parseDelim must reject "#FOO" as a delimiter (same as any other
// invalid-delimiter case), not accept it.
func TestScanContent_HashStartingDelimiterIsCommentNotDelimiter(t *testing.T) {
	if _, _, _, ok := parseDelim("#FOO"); ok {
		t.Fatalf("expected '#FOO' to be rejected as a delimiter (word-start '#' is a comment), but parseDelim accepted it")
	}
}

// TestScanContent_QuotedDelimiterHashNotRejected guards the quoted-delimiter
// half of F2: the old code additionally ran isIdentifier on a QUOTED
// delimiter's content, which also rejected '#' -- even though a quoted
// delimiter has no word-boundary ambiguity at all (the matching quote chars
// already mark exactly where it starts and ends). Verified against real
// bash: both single- and double-quoted "E#F" delimiters work identically to
// the unquoted case, and disable body expansion (quoted=true).
func TestScanContent_QuotedDelimiterHashNotRejected(t *testing.T) {
	tests := []struct {
		name   string
		opener string
	}{
		{"single_quoted", "cat <<'E#F'"},
		{"double_quoted", `cat <<"E#F"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := tt.opener + "\nbody line\nE#F\necho after\n"
			heredocs := ScanContent(src)
			if len(heredocs) != 1 {
				t.Fatalf("expected 1 heredoc for %q, got %d: %+v", tt.opener, len(heredocs), heredocs)
			}
			if heredocs[0].Unquoted {
				t.Fatalf("expected a quoted E#F delimiter to disable expansion (Unquoted=false), got %+v", heredocs[0])
			}
		})
	}
}

// TestScanContent_EmptyQuotedDelimiterAccepted guards a related edge the old
// isIdentifier check also rejected: bash accepts an empty quoted delimiter
// (two adjacent single-quote characters right after "<<"), matched by a
// blank closing line.
//
// Verified against real bash:
//
//	$ printf 'cat <<Q\nbody\n\nafter\n' | sed "s/Q/''/" | bash
//	body
//	after
func TestScanContent_EmptyQuotedDelimiterAccepted(t *testing.T) {
	src := "cat <<''\nbody\n\necho after\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc for an empty quoted delimiter, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Size != len("body\n") {
		t.Fatalf("expected body 'body\\n' only, got size %d", heredocs[0].Size)
	}
}

// TestScanContent_BackslashEscapedDelimiterIdiom guards a construct the
// broadened isShellWordSeparator-based scan must handle explicitly: real
// bash treats a backslash ANYWHERE in an otherwise-unquoted delimiter word as
// an escape that (a) is stripped from the resulting delimiter text and (b)
// marks the WHOLE delimiter quoted (disables body expansion), identical to
// real quoting -- not as an ordinary delimiter character. Without this,
// broadening the accepted character set to fix the '#' case above would
// instead accept the literal backslash INTO the delimiter name (e.g.
// "\EOF"), which would then never match the real (backslash-free) closing
// line and silently drop the heredoc as unterminated -- trading one fail-open
// for another.
//
// Verified against real bash:
//
//	$ X=shouldnotexpand printf 'cat <<\EOF\n$X\nEOF\n' | bash  # leading escape
//	$X
//	$ X=shouldnotexpand bash -c 'cat <<FO\O
//	$X
//	FOO
//	after'
//	$X
//	bash: line 4: after: command not found
//
// The second transcript proves BOTH halves at once: the body is NOT
// expanded ("$X" printed literally, i.e. quoted=true) AND the real closing
// line is "FOO" (the escaped backslash removed, not "FO\O" or "FOO" with an
// extra literal backslash) -- confirmed because the heredoc closed exactly
// there and "after" ran as a separate (failing) command, rather than being
// swallowed as unterminated body content.
func TestScanContent_BackslashEscapedDelimiterIdiom(t *testing.T) {
	tests := []struct {
		name   string
		opener string
		delim  string // the real, literal closing line bash expects
	}{
		{"leading_escape", `cat <<\EOF`, "EOF"},
		{"mid_word_escape", `cat <<FO\O`, "FOO"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.Repeat("z", 600) + "\n" // over budget
			src := tt.opener + "\n" + body + tt.delim + "\necho after\n"
			heredocs := ScanContent(src)
			if len(heredocs) != 1 {
				t.Fatalf("expected 1 heredoc for %q (closing on %q), got %d: %+v", tt.opener, tt.delim, len(heredocs), heredocs)
			}
			if heredocs[0].Unquoted {
				t.Fatalf("expected a backslash-escaped delimiter to disable expansion (Unquoted=false), got %+v", heredocs[0])
			}
			if heredocs[0].Size <= defaultBudget {
				t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
			}
		})
	}
}

// TestScanContent_NumericFirstDelimiterStillRejected re-confirms the
// pre-existing, intentional numeric-first-delimiter restriction survives the
// F2 rewrite unchanged: `cat <<123` is a valid real-bash heredoc (delimiter
// "123"), but this scanner declines to recognize it on purpose, as
// defense-in-depth against misreading an arithmetic left shift. This is not
// a new assertion (scanner_test.go's TestScanContent_WhitespaceBeforeDelimHandled
// already checks the arithmetic side); this checks parseDelim directly so a
// future edit to the unquoted-word loop cannot silently drop the restriction.
func TestScanContent_NumericFirstDelimiterStillRejected(t *testing.T) {
	if _, _, _, ok := parseDelim("123"); ok {
		t.Fatalf("expected a numeric-first delimiter to still be rejected, but parseDelim accepted it")
	}
}

// TestScanContent_DollarInUnquotedDelimiterLiteral is a follow-up probe
// (mandated hunt, "a delimiter containing $"): real bash never expands a
// heredoc delimiter word when determining what it is -- `cat <<E$F` closes
// on the literal line "E$F", unexpanded, and is otherwise an ordinary
// unquoted (expansion-enabled-in-the-BODY) heredoc.
//
// Verified against real bash:
//
//	$ printf 'cat <<E$F\nbody\nE$F\necho after\n' | bash
//	body
//	after
//
// '$' is not a shell word separator, so the existing unquoted-word scan
// already includes it in the delimiter text unchanged -- this probe found no
// new gap, and this test pins that down as a regression guard.
func TestScanContent_DollarInUnquotedDelimiterLiteral(t *testing.T) {
	src := "cat <<E$F\nbody\nE$F\necho after\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc for a delimiter containing '$', got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Unquoted != true {
		t.Fatalf("expected an ordinary (no quote/escape) delimiter to stay Unquoted=true, got %+v", heredocs[0])
	}
}

// TestScanContent_ConcatenatedQuotedSegmentInDelimiter is a follow-up probe
// (mandated hunt, "a delimiter containing ... quotes") that found a genuine
// NEW gap, not one of the four assigned bugs: ordinary bash word
// concatenation applies to heredoc delimiters too -- an unquoted prefix
// directly followed by a quoted segment (`<<FOO'BAR'`, no space) is ONE word,
// "FOOBAR", exactly like `echo FOO'BAR'` prints "FOOBAR". Before this probe,
// parseDelim's unquoted-word scan had no concept of a quote character
// appearing MID-word (only s[0] being a quote was handled, by the earlier,
// separate fully-quoted branch): it read the literal quote bytes into the
// delimiter name, computing "FOO'BAR'" instead of real bash's "FOOBAR",
// which then never matched the true (quote-free) closing line and silently
// dropped the heredoc as unterminated.
//
// Verified against real bash:
//
//	$ X=shouldnotexpand printf "cat <<FOO'BAR'\n\$X\nFOOBAR\necho after\n" | bash
//	$X
//	after
//
// (body NOT expanded, i.e. quoted=true, and the real closing line is the
// concatenated "FOOBAR", not "FOO'BAR'" or "FOOBAR'").
func TestScanContent_ConcatenatedQuotedSegmentInDelimiter(t *testing.T) {
	body := strings.Repeat("z", 600) + "\n" // over budget
	src := "cat <<FOO'BAR'\n" + body + "FOOBAR\necho after\n"

	heredocs := ScanContent(src)

	if len(heredocs) != 1 {
		t.Fatalf("expected 1 heredoc for the concatenated FOO'BAR' delimiter, got %d: %+v", len(heredocs), heredocs)
	}
	if heredocs[0].Unquoted {
		t.Fatalf("expected the concatenated quoted segment to disable expansion (Unquoted=false), got %+v", heredocs[0])
	}
	if heredocs[0].Size <= defaultBudget {
		t.Fatalf("expected over-budget body, got %d", heredocs[0].Size)
	}
}
