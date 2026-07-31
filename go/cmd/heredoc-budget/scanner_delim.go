// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "strings"

// This file holds parseDelim and the word-boundary predicate it shares with
// findAllOpeners (scanner_lexer.go). Split out of scanner_lexer.go to stay
// under the repo's 500-line-per-file cap; the two files are one logical unit
// (findAllOpeners is parseDelim's only real caller) and change together.

// isShellWordSeparator reports whether b is one of the bash "word separator"
// bytes this scanner treats as a genuine word boundary at the unquoted
// (base/substitution/arithmetic) lexical level: blank (space, tab) or one of
// the control/redirection operator characters (';', '|', '&', '<', '>', '(',
// ')') that ends the current word wherever it appears, standalone, outside
// any quote. findAllOpeners's wordStart tracking (for recognizing a trailing
// '#' comment) and parseDelim's unquoted-delimiter scan below (for knowing
// where an unquoted heredoc delimiter word ends) both need the IDENTICAL
// notion of "word boundary" -- driving both from this one function, instead
// of two hand-maintained lists, is what keeps them from drifting apart
// (2026-07 hardening review, F2/F4).
//
// Deliberately excludes quote characters (single quote, double quote), the
// dollar sign, and the backtick: none of those end a bash word -- foo"bar"baz,
// foo$(cmd)baz, and a backtick-quoted foo cmd baz are each a single
// concatenated word, not three. Also
// excludes backslash, which escapes the next byte rather than separating a
// word from what follows it (see parseDelim's own backslash handling below,
// and findAllOpeners's escape branches).
func isShellWordSeparator(b byte) bool {
	switch b {
	case ' ', '\t', ';', '|', '&', '<', '>', '(', ')':
		return true
	default:
		return false
	}
}

// parseDelim parses a heredoc delimiter word from the start of s, which is
// the text immediately following "<<"/"<<-" and any blanks. It accepts
// either a bare (unquoted) word or a single- or double-quoted word, matching
// what real bash treats as a heredoc delimiter -- not the
// [A-Za-z_][A-Za-z0-9_]* identifier shape this used to approximate, which
// truncated a delimiter like "E#F" (from `<<E#F`) at the first non-identifier
// byte, silently dropping the real heredoc and everything after it when the
// truncated delimiter never matched the true closing line (F2/2026-07
// hardening review). It reports whether the delimiter was quoted (which
// disables runtime expansion of the body, see Heredoc.Unquoted) and how many
// bytes of s the delimiter token consumed, so the caller can resume scanning
// immediately after it on the same line.
func parseDelim(s string) (name string, quoted bool, consumed int, ok bool) {
	if s == "" {
		return "", false, 0, false
	}
	if s[0] == '\'' || s[0] == '"' {
		q := s[0]
		end := strings.IndexByte(s[1:], q)
		if end < 0 {
			return "", false, 0, false
		}
		// A quoted delimiter's content is exactly the bytes between the
		// matching quote characters, whatever they are -- real bash places
		// no identifier-shaped restriction on it. "<<'E#F'", "<<'has
		// spaces'", and even "<<''" (an empty delimiter, matched by a blank
		// closing line) are all valid, verified against real bash. Unlike
		// the unquoted branch below, quoting already marks exactly where
		// the word starts and ends, so no separator/escape handling applies
		// here.
		return s[1 : 1+end], true, end + 2, true
	}
	if s[0] == '#' {
		// The position right after "<<"/"<<-" and any blanks is itself a
		// word start, and a '#' that starts a word begins a real bash
		// comment, not a delimiter -- verified: "cat <<#FOO" is a syntax
		// error in real bash (the comment eats the would-be delimiter,
		// leaving "<<" with none), not a heredoc with delimiter "#FOO".
		return "", false, 0, false
	}
	if s[0] >= '0' && s[0] <= '9' {
		// Numeric-first delimiter rejected ON PURPOSE (pre-existing,
		// intentional -- not a bug, and not something this pass removes):
		// "cat <<123" is a valid heredoc with delimiter "123" in real bash
		// too, but this scanner declines to recognize it as
		// defense-in-depth against misreading an arithmetic left shift
		// ("$(( x << 2 ))") as a heredoc opener. findAllOpeners's frameArith
		// tracking is now the PRIMARY defense against that misread
		// (F1/2026-07 hardening review); this restriction stays as an
		// intentional secondary one.
		return "", false, 0, false
	}

	// Unquoted delimiter: bash reads the next WORD, which ends at the first
	// genuine word separator (isShellWordSeparator above), not at the first
	// non-identifier byte. A backslash anywhere in the word is an escape --
	// stripped from the resulting name and, per real bash, marking the
	// WHOLE delimiter quoted (disabling body expansion, identical to real
	// quoting): both the classic `<<\EOF` idiom (escaping just the first
	// character) and a mid-word escape (`<<FO\O`, delimiter "FOO") were
	// verified against real bash to behave this way. A backslash with
	// NOTHING after it (the last byte of s) is a dangling line continuation
	// in progress, not an escape -- reporting failure here (rather than
	// guessing a bogus one-byte delimiter) lets the byte-by-byte fallback
	// in findAllOpeners's caller reach that same backslash through the main
	// per-character escape/continuation handling, which is what actually
	// detects and reports the continuation (see continuesOnNextLine,
	// F3/2026-07 hardening review).
	//
	// A quote character appearing MID-WORD (not at s[0], which the
	// fully-quoted branch above already intercepts) starts a quoted SEGMENT
	// of the same word, exactly like ordinary bash word concatenation
	// (`echo FOO'BAR'` prints "FOOBAR", one word) -- found via this pass's
	// mandated adversarial probe for "a delimiter containing ... quotes",
	// not one of the four assigned bugs. Before this, the byte-scan treated
	// the quote characters as literal delimiter bytes, computing "FOO'BAR'"
	// instead of the real bash delimiter "FOOBAR", which then never matched
	// the true (quote-free) closing line and silently dropped the heredoc as
	// unterminated. Verified against real bash: `cat <<FOO'BAR'` closes on a
	// literal "FOOBAR" line and disables body expansion, exactly like a
	// fully-quoted delimiter.
	var b strings.Builder
	j := 0
	for j < len(s) && !isShellWordSeparator(s[j]) {
		switch s[j] {
		case '\\':
			if j+1 >= len(s) {
				return "", false, 0, false
			}
			b.WriteByte(s[j+1])
			quoted = true
			j += 2
		case '\'', '"':
			q := s[j]
			end := strings.IndexByte(s[j+1:], q)
			if end < 0 {
				return "", false, 0, false
			}
			b.WriteString(s[j+1 : j+1+end])
			quoted = true
			j += end + 2
		default:
			b.WriteByte(s[j])
			j++
		}
	}
	if j == 0 {
		return "", false, 0, false
	}
	return b.String(), quoted, j, true
}
