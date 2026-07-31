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
// and findAllOpeners's escape branches). Also deliberately excludes '\r':
// real bash does not treat it as whitespace or a word boundary either --
// verified against real bash, a CRLF script's heredoc delimiter genuinely
// keeps the trailing '\r' as part of the word (see stripTrailingCR below for
// where and why that trailing byte is normalised away instead, P1-1/2026-07
// hardening review round 2).
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
//
// A delimiter that STARTS with a quote (`<<'DELIM'...`, `<<"DELIM"...`) used
// to be handled by a separate top-level branch here that returned
// immediately once the opening quote closed, never continuing to read the
// rest of the word. That is wrong whenever anything follows the closing
// quote on the same word: real bash keeps reading -- ordinary word
// concatenation, the same rule that already made `<<FOO'BAR'` (an unquoted
// prefix followed by a quoted segment) resolve to delimiter "FOOBAR" in the
// loop below. An EMPTY quoted pair immediately followed by unquoted text
// needs the identical treatment in the other order (P1-2, codex review of PR
// #5890): bash reads a heredoc opened by two adjacent single quotes directly
// followed by the letter E as delimiter "E", not "". The old early-return
// version returned delimiter "" there, so the scanner waited for a BLANK
// closing line instead of the real "E" line -- silently folding every
// intervening line, including any real heredoc's own opener/body/closer
// text, into one phantom "quoted" heredoc body measured against the wrong
// (full, not margin) budget. Fixed by deleting that separate branch: a
// leading quote now falls straight into the general word-scan loop below and
// is handled by the SAME quote case that already handles a quote appearing
// mid-word, so both continue reading the rest of the word after the quote
// closes.
//
// Verified against real bash: an opener whose delimiter is an empty quoted
// pair directly followed by "E" closes on "E"; `<<'A'B` closes on "AB";
// `<<A'` followed immediately by another empty quoted pair then `B` (already
// correct pre-fix, since it never went through the separate leading-quote
// branch) closes on "AB"; a delimiter made of two adjacent empty quoted
// pairs back to back closes on an empty line; and `<<'A'"B"C` (mixed quote
// styles) closes on "ABC" -- see scanner_quoted_prefix_test.go for each
// transcript.
func parseDelim(s string) (name string, quoted bool, consumed int, ok bool) {
	if s == "" {
		return "", false, 0, false
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
	// A quote character -- whether it opens the word at s[0] (P1-2 above) or
	// appears MID-WORD -- starts a quoted SEGMENT of the same word, exactly
	// like ordinary bash word concatenation (`echo FOO'BAR'` prints
	// "FOOBAR", one word; `echo '<<'"'"'foo'"'"'"` idioms compose the same
	// way). The mid-word case was found via this pass's mandated adversarial
	// probe for "a delimiter containing ... quotes", not one of the four
	// originally assigned bugs. Before that probe's fix, the byte-scan
	// treated the quote characters as literal delimiter bytes, computing
	// "FOO'BAR'" instead of the real bash delimiter "FOOBAR", which then
	// never matched the true (quote-free) closing line and silently dropped
	// the heredoc as unterminated. Verified against real bash: `cat
	// <<FOO'BAR'` closes on a literal "FOOBAR" line and disables body
	// expansion, exactly like a fully-quoted delimiter -- and, per P1-2
	// above, so does the leading-quote direction (`<<'FOO'BAR`, delimiter
	// "FOOBAR").
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
	// stripTrailingCR is the P1-1 fix (codex review of PR #5890): see its
	// own doc comment for why a trailing '\r' captured here (real bash does
	// not treat '\r' as a word separator, so a CRLF script's bare delimiter
	// legitimately includes it) must be normalised away here, matching
	// closesHeredoc's (scanner.go) pre-existing normalisation of the
	// candidate closing line, through the SAME shared function.
	return stripTrailingCR(b.String()), quoted, j, true
}

// stripTrailingCR removes exactly one trailing '\r' byte, if present. It is
// the ONE shared CRLF-tolerance rule parseDelim (above) and closesHeredoc
// (scanner.go) both route through, so a CRLF-terminated script's heredoc
// delimiter and its candidate closing line are normalised identically before
// they are compared.
//
// Before this helper existed, the two sides normalised '\r' independently:
// closesHeredoc already stripped it (pre-existing, to tolerate a
// CRLF-terminated closing line), but parseDelim's broadened word scan
// (F2/2026-07 hardening review) did not, so on a CRLF script a bare
// delimiter like "EOF" now legitimately captured the line's trailing '\r' as
// an ordinary word byte -- "EOF\r" -- since isShellWordSeparator
// deliberately does not treat '\r' as a real bash word separator (real bash
// itself does not; see its doc comment). The two independently-normalised
// sides then never matched, so the heredoc was dropped as unterminated and
// its body, however oversized, was never measured (P1-1, codex review of PR
// #5890). Routing both call sites through this one function, instead of a
// second hand-written `strings.TrimSuffix(x, "\r")`, is what keeps them from
// drifting apart the same way isShellWordSeparator already does for
// word-boundary bytes.
//
// This is deliberately a byte-level normalisation, not a bash-fidelity
// simulation: real bash treats a delimiter's or a closing line's trailing
// '\r' as ordinary content (see isShellWordSeparator's doc comment), so a
// script with INCONSISTENT line endings within one heredoc (a CRLF opener
// but an LF-only closing line, or vice versa) is unterminated in real bash
// but closes under this scanner's more lenient, symmetric stripping. That is
// an intentional choice, not an oversight: this scanner's job is to catch a
// real oversized heredoc body before it becomes a #5074 hang, and erring
// toward still finding and measuring a body in an inconsistent-line-ending
// script is the safer direction than matching bash's stricter (and, for a
// heredoc, silently-swallow-the-rest-of-the-file) failure mode exactly.
func stripTrailingCR(s string) string {
	return strings.TrimSuffix(s, "\r")
}
