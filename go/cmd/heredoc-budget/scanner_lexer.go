// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "strings"

// This file holds the character-by-character quote/substitution/comment
// lexer that findAllOpeners drives: the frame stack, the opener-delimiter
// parser, and their small helpers. ScanContent (scanner.go) owns the
// line-by-line heredoc-body state machine and calls into this lexer once per
// line that is not already inside a heredoc body. Split out of scanner.go to
// stay under the repo's 500-line-per-file cap; the two files are one logical
// unit and change together.

// opener describes a recognized heredoc opener: its delimiter word, whether
// it uses the `<<-` tab-stripping form, and whether the delimiter itself was
// quoted (`<<'DELIM'`/`<<"DELIM"`), which disables runtime expansion of the
// body.
type opener struct {
	delim    string
	tabStrip bool
	quoted   bool
}

// Quote/substitution stack frame markers used by findAllOpeners. Each byte
// pushed onto the stack records what opened that lexical context:
//
//   - frameSingle ('): a POSIX single-quoted string. Fully inert — no
//     escapes, no substitution, no nested quoting; only its own closing '
//     ends it.
//   - frameDouble ("): a double-quoted string. Backslash escapes the next
//     char; a bare `'` inside it is NOT special (matches bash); but `$(`
//     STILL opens a nested substitution frame, because command substitution
//     is not suppressed inside double quotes (only inside single quotes).
//   - frameAnsiC ($'...'): an ANSI-C-quoted string. Backslash escapes the
//     next char (so `\'` does not end the string); no substitution
//     recognized inside it.
//   - frameSubst ($(...)): a command substitution. This is bash re-entering
//     a fresh, UNQUOTED lexical scope — it is scanned exactly like the
//     top-level (base) context, including recognizing further quotes,
//     nested `$(`, and real heredoc openers — until its own closing `)`.
//
// The "base" context (empty stack) behaves the same as frameSubst for
// scanning purposes, except a stray `)` at the base has nothing to pop.
const (
	frameSingle = '\''
	frameDouble = '"'
	frameAnsiC  = 'A'
	frameSubst  = '('
)

// inQuoteFrame reports whether the top of stack is a live quoted string
// (single, double, or ANSI-C) as opposed to being at the base context or
// inside an unquoted `$(...)` substitution. ScanContent uses this to decide
// whether a "#"-looking line is really a comment or just the tail of a
// still-open multi-line string.
func inQuoteFrame(stack []byte) bool {
	if len(stack) == 0 {
		return false
	}
	switch stack[len(stack)-1] {
	case frameSingle, frameDouble, frameAnsiC:
		return true
	default:
		return false
	}
}

// findAllOpeners scans line for every heredoc opener — `<<DELIM`,
// `<<'DELIM'`, `<<"DELIM"`, or the `<<-` tab-stripped variant of each — and
// returns them in left-to-right order, along with the updated quote/
// substitution stack (see the frame* constants) for the caller to pass back
// in on the next line. `<<<` here-strings are recognized and skipped rather
// than mistaken for a heredoc opener with an empty or malformed delimiter.
//
// The scan tracks quote/substitution context as it goes, so a `<<IDENT`
// written inside a string literal (e.g. `echo "a <<X b"`) is not mistaken
// for a real opener (#5079) — bash itself never treats `<<` as redirection
// inside a quoted string. Because the scan keeps going after a match instead
// of stopping at the first one, a line that opens more than one heredoc
// (`cmd <<A <<B`) yields every opener, not just the first (#5079); bash
// reads their bodies back to back immediately after the command line, so
// ScanContent processes them in the same left-to-right order.
//
// The scan also tracks unquoted, word-starting `#` as a real bash comment
// (#5085/#5079 review): once one is seen at the base/substitution level, the
// rest of the line is inert and scanning stops immediately, so a heredoc-
// opener-shaped fragment inside a trailing comment (e.g. "echo x # <<EOF")
// is not mistaken for a real opener. See the `wordStart` variable and its
// use below for the exact rule and the real-bash verification it rests on.
//
// "Word-starting" is tracked as EXPLICIT STATE (the `wordStart` local),
// never re-derived from a raw byte lookback, because a lookback cannot tell
// a genuine separator apart from one that was already consumed as half of a
// wider unit. A P1 regression shipped exactly this way: the original check
// read line[i-1] directly and treated any space/tab byte there as proof of
// a word boundary, but a backslash-escaped space (`x\ #<<EOF`) leaves that
// space byte sitting at line[i-1] even though the escape branch already
// consumed it two bytes at a time -- so the lookback wrongly saw a "real"
// separator that bash itself does not, misreading a genuine heredoc opener
// as a trailing comment (0 heredocs detected, exit 0, the exact fail-open
// this gate exists to catch). `wordStart` instead reflects what the PREVIOUS
// iteration actually consumed: true only after real unescaped whitespace or
// an unquoted statement-separator operator (`;`, `|`, `&`), and at the start
// of the line; false after anything else, including an escape's second
// byte, a quote/substitution open or close, and a heredoc-opener match --
// none of those are real word boundaries in bash, escaped or not.
//
// The stack is threaded in and back out (rather than reset per call) because
// quoting is not a per-line property in bash: a double-quoted string can
// span several physical lines, and a `$(...)` opened on one line can stay
// open across lines too. Command substitution also gets its own dedicated
// handling: `$(` opens a fresh, UNQUOTED scope even while nested inside an
// outer double-quoted string that has not closed yet, because bash does not
// suppress command substitution inside double quotes (only inside single
// quotes) — so a real heredoc inside `"...$(cat <<Y ... Y)..."` is still a
// real heredoc, not string content.
func findAllOpeners(line string, stack []byte) ([]opener, []byte) {
	var openers []opener

	top := func() byte {
		if len(stack) == 0 {
			return 0
		}
		return stack[len(stack)-1]
	}

	// wordStart is true when the position about to be examined is a genuine
	// bash word boundary: the start of the line, or the byte immediately
	// after real (unescaped, unquoted) whitespace or a statement-separator
	// operator (`;`, `|`, `&`) that was itself consumed as its own token. It
	// is explicit state, not a raw byte lookback -- see the doc comment on
	// findAllOpeners for why a lookback is unsound after a variable-width
	// consume (escape, quote close, substitution close).
	wordStart := true

	for i := 0; i < len(line); {
		c := line[i]
		// Capture this iteration's word-start state before it is
		// (re)computed below. Every branch defaults to "not a word start"
		// for the NEXT iteration; only the real-separator case at the
		// bottom of the base/subst switch sets it back to true. This must
		// happen unconditionally, before any branch below can `continue`,
		// so every exit path leaves wordStart correctly set for whatever
		// byte comes next.
		atWordStart := wordStart
		wordStart = false

		switch top() {
		case frameSingle:
			// POSIX single quotes: no escapes, nothing else is special.
			if c == frameSingle {
				stack = stack[:len(stack)-1]
			}
			i++
		case frameAnsiC:
			// $'...': backslash escapes the next char, so `\'` does not
			// close the string early (the #5079 review false negative).
			if c == '\\' && i+1 < len(line) {
				i += 2
				continue
			}
			if c == '\'' {
				stack = stack[:len(stack)-1]
			}
			i++
		case frameDouble:
			if c == '\\' && i+1 < len(line) {
				i += 2
				continue
			}
			if c == '"' {
				stack = stack[:len(stack)-1]
				i++
				continue
			}
			// Command substitution is NOT suppressed inside double quotes,
			// so `$(` still opens a fresh unquoted frame here.
			if c == '$' && i+1 < len(line) && line[i+1] == '(' {
				stack = append(stack, frameSubst)
				i += 2
				continue
			}
			i++
		default: // base context (empty stack) or inside an unquoted $(...)
			switch {
			case c == '\\' && i+1 < len(line):
				// Backslash escapes the next char even outside any quote,
				// e.g. the extremely common `'\''` idiom for embedding a
				// literal `'` inside a single-quoted string: close (`'`),
				// escaped literal quote (`\'`), reopen (`'`). Without this,
				// the escaped `'` is wrongly read as opening a fresh quote
				// frame that the very next `'` (the real reopen) instantly
				// closes again — landing back at base one idiom-cycle too
				// early. Any literal `"`/`'` still inside what bash
				// considers the reopened string then gets misread as a
				// real quote-open, desyncing the stack for the rest of the
				// file and silently swallowing a real heredoc later on
				// (found via adversarial review against a real script in
				// this repo, not a synthetic case).
				//
				// The escaped byte is NOT a word boundary regardless of
				// what it is — an escaped space or tab does not end the
				// current word in bash, so wordStart stays false (already
				// set above) for whatever follows. This is the P1
				// regression fix: the old code inferred word-start from
				// the raw byte at line[i-1], which cannot tell this
				// escaped separator apart from a real one once `i` has
				// jumped two bytes ahead.
				i += 2
			case c == '#' && atWordStart:
				// An unquoted '#' that starts a word begins a real bash
				// comment: the rest of the line is ignored, so anything
				// after it that looks like a heredoc opener (e.g.
				// "echo x # <<EOF") is inert, not a real opener. Treating
				// it as real would desync the scanner and swallow a
				// genuine over-budget heredoc later in the file — the
				// same fail-open class as the full-line comment case in
				// ScanContent, just triggered by a TRAILING comment
				// instead of a full-line one (#5085/#5079 review).
				// Verified against real /bin/bash. A '#' that does NOT
				// start a word — "echo foo#bar", "${x#pat}", "$#" — is
				// ordinary text in real bash, not a comment start; each
				// fails the wordStart check and falls through untouched,
				// so a real heredoc opener later on the same line is
				// still found. A '#' inside a quote never reaches this
				// default case at all (it is handled by the
				// frameSingle/frameDouble/frameAnsiC cases above, where
				// '#' has no special meaning), so quoted '#' is already
				// correctly excluded.
				return openers, stack
			case c == '\'':
				stack = append(stack, frameSingle)
				i++
			case c == '"':
				stack = append(stack, frameDouble)
				i++
			case c == '$' && i+1 < len(line) && line[i+1] == '\'':
				stack = append(stack, frameAnsiC)
				i += 2
			case c == '$' && i+1 < len(line) && line[i+1] == '(':
				stack = append(stack, frameSubst)
				i += 2
			case c == ')' && top() == frameSubst:
				stack = stack[:len(stack)-1]
				i++
			case c == '<':
				if i+1 >= len(line) || line[i+1] != '<' {
					i++
					continue
				}
				// `<<<` is a here-string, not a heredoc. Skip past the
				// third '<' so it cannot be re-matched as its own (bogus)
				// heredoc opener.
				if i+2 < len(line) && line[i+2] == '<' {
					i += 3
					continue
				}
				rest := line[i+2:]
				tabStrip := strings.HasPrefix(rest, "-")
				if tabStrip {
					rest = rest[1:]
				}
				// Bash allows optional blanks between `<<`/`<<-` and the
				// delimiter (`cat << EOF`, `cat <<- 'EOF'`). Trim them so a
				// whitespace-separated heredoc is not missed — a fail-open
				// the gate exists to block. The delimiter must still start
				// with a letter or `_` (parseDelim), so an arithmetic
				// left-shift like `$(( x << 2 ))` is not mistaken for a
				// heredoc opener.
				trimmed := strings.TrimLeft(rest, " \t")
				blanks := len(rest) - len(trimmed)
				if delim, quoted, consumed, ok := parseDelim(trimmed); ok {
					openers = append(openers, opener{delim: delim, tabStrip: tabStrip, quoted: quoted})
					advance := 2 + blanks + consumed
					if tabStrip {
						advance++
					}
					i += advance
					continue
				}
				// Not a valid delimiter after "<<" (e.g. no identifier
				// follows) — keep scanning for another candidate.
				i++
			case c == ' ' || c == '\t' || c == ';' || c == '|' || c == '&':
				// A real, unescaped blank or statement-separator operator
				// IS a genuine word boundary in bash, so the byte right
				// after it is a real word start for the '#'-comment check
				// above. Verified against real bash for `;` and `|`
				// (`true;#<<EOF`, `true|#<<EOF` both discard the rest of
				// the line as a comment, same as a plain blank); `&`
				// behaves identically (background-job separator). This
				// set is deliberately narrow — only the bytes actually
				// verified — not every bash operator (see AGENTS.md).
				wordStart = true
				i++
			default:
				i++
			}
		}
	}
	return openers, stack
}

// parseDelim parses a heredoc delimiter word from the start of s, which is
// the text immediately following "<<"/"<<-" and any blanks. It accepts a
// bare identifier or a single- or double-quoted identifier, per DELIM =
// [A-Za-z_][A-Za-z0-9_]*. It reports whether the delimiter was quoted (which
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
		name := s[1 : 1+end]
		if !isIdentifier(name) {
			return "", false, 0, false
		}
		return name, true, end + 2, true // consumed = opening quote + name + closing quote
	}
	j := 0
	for j < len(s) && isIdentByte(s[j], j == 0) {
		j++
	}
	if j == 0 {
		return "", false, 0, false
	}
	return s[:j], false, j, true
}

// isIdentifier reports whether s matches [A-Za-z_][A-Za-z0-9_]* in full.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isIdentByte(s[i], i == 0) {
			return false
		}
	}
	return true
}

// isIdentByte reports whether b is a valid byte at the given position of a
// [A-Za-z_][A-Za-z0-9_]* identifier; first distinguishes the leading byte
// (which cannot be a digit) from the rest.
func isIdentByte(b byte, first bool) bool {
	switch {
	case b == '_', b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z':
		return true
	case b >= '0' && b <= '9':
		return !first
	default:
		return false
	}
}
