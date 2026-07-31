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
//   - frameArith ($((...)), one frame PER open paren): bash arithmetic
//     evaluation, entered via the three-byte "$((" opener. `<<` inside it is
//     the arithmetic SHIFT operator, never a heredoc opener, and `#` is
//     ordinary text, never a comment (verified against real bash: `(( 1
//     <#comment\n2 ))` reports an arithmetic syntax error citing the literal
//     "#comment" text, proving '#' is not stripped as a comment there).
//     "$((" pushes TWO frameArith frames (one per paren beyond the leading
//     "$"), and every literal `(` encountered while top-of-stack is
//     frameArith pushes one more, popped by the next `)` — reusing the
//     single-byte-per-frame stack as a paren-depth counter, so a grouping
//     paren inside the expression (`$(( (a+b) * c ))`) does not close the
//     frame early. Command substitution still works inside arithmetic (real
//     bash allows `$(cmd)` inside `$((...))`), so a nested `$(` still pushes
//     its own frameSubst on top, same as anywhere else (#5085/#5079 review).
//
// The "base" context (empty stack) behaves the same as frameSubst for
// scanning purposes, except a stray `)` at the base has nothing to pop.
const (
	frameSingle = '\''
	frameDouble = '"'
	frameAnsiC  = 'A'
	frameSubst  = '('
	frameArith  = 'M'
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
//
// findAllOpeners also recognizes bash arithmetic evaluation (`$((...))`,
// tracked via the frameArith frame) as its own lexical scope distinct from
// command substitution: `<<` inside it is the shift operator, never a
// heredoc opener, and `#` is ordinary text, never a comment (2026-07
// hardening review, F1). Before this, the scanner modeled only the FIRST `(`
// of `$((` (indistinguishable from a plain `$(cmd)`), so `<<` inside an
// arithmetic expression was read as a real heredoc opener whenever its
// operand was identifier-shaped (`x=$(( flags << shiftamount ))`) rather
// than the numeric shape the old parseDelim-only mitigation blocked.
//
// The third return value reports whether line ends in a "dangling" trailing
// backslash that real bash splices onto the next physical line (a
// `\<newline>` continuation) BEFORE either line is tokenized — true when the
// backslash is the last byte of line and scanning was NOT inside a
// single-quoted or ANSI-C string (2026-07 hardening review, F3; verified
// against real bash that a trailing `\<newline>` is spliced away inside
// double-quoted strings and at the base level, but is kept as two literal
// characters inside `'...'` and `$'...'`). ScanContent uses this to fuse the
// next physical line directly onto this one and rescan the combined text
// from scratch, so a continuation that fuses mid-word onto a line beginning
// with '#' (bash does NOT see a comment there) is not mistaken for one.
func findAllOpeners(line string, stack []byte) ([]opener, []byte, bool) {
	var openers []opener
	var continuesOnNextLine bool

	top := func() byte {
		if len(stack) == 0 {
			return 0
		}
		return stack[len(stack)-1]
	}

	// wordStart is true when the position about to be examined is a genuine
	// bash word boundary: the start of the line, or the byte immediately
	// after real (unescaped, unquoted) whitespace or one of the operator
	// characters this scanner recognizes as a word separator (see
	// isShellWordSeparator: `;`, `|`, `&`, `<`, `>`, `(`, `)`) that was
	// itself consumed as its own token. It is explicit state, not a raw byte
	// lookback -- see the doc comment on findAllOpeners for why a lookback
	// is unsound after a variable-width consume (escape, quote close,
	// substitution close).
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
			if c == '\\' {
				if i+1 < len(line) {
					i += 2
					continue
				}
				// Dangling trailing backslash with nothing left on the line
				// to escape: real bash splices this onto the next physical
				// line even inside an open double-quoted string (verified:
				// `echo "line one \` + newline + `continues"` prints "line
				// one continues" -- both the backslash AND the newline are
				// removed, F3/2026-07 hardening review). Report it so
				// ScanContent can fuse the next line on and rescan.
				continuesOnNextLine = true
				i++
				continue
			}
			if c == '"' {
				stack = stack[:len(stack)-1]
				i++
				continue
			}
			// Command substitution and arithmetic evaluation are NOT
			// suppressed inside double quotes, so `$(`/`$((` still open a
			// fresh frame here, exactly like the default case below.
			if c == '$' && i+1 < len(line) && line[i+1] == '(' {
				if i+2 < len(line) && line[i+2] == '(' {
					stack = append(stack, frameArith, frameArith)
					i += 3
					continue
				}
				stack = append(stack, frameSubst)
				i += 2
				continue
			}
			i++
		default: // base context (empty stack), inside $(...), or inside $((...))
			switch {
			case c == '\\' && i+1 >= len(line):
				// Dangling trailing backslash: see the identical frameDouble
				// branch above. Applies at the base level and inside
				// $(...)/$((...)) too (F3/2026-07 hardening review).
				continuesOnNextLine = true
				i++
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
			case c == '#' && atWordStart && top() != frameArith:
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
				// correctly excluded. `top() != frameArith` excludes
				// arithmetic evaluation too: '#' is never a comment inside
				// `$((...))` (verified against real bash, F1/2026-07
				// hardening review) -- without this guard, a real '#'
				// inside an arithmetic expression would wrongly end
				// scanning and could hide a real heredoc opener later on
				// the same line.
				return openers, stack, continuesOnNextLine
			case c == '\'':
				stack = append(stack, frameSingle)
				i++
			case c == '"':
				stack = append(stack, frameDouble)
				i++
			case c == '$' && i+1 < len(line) && line[i+1] == '\'':
				stack = append(stack, frameAnsiC)
				i += 2
			case c == '$' && i+1 < len(line) && line[i+1] == '(' && i+2 < len(line) && line[i+2] == '(':
				// "$((" opens arithmetic evaluation, not command
				// substitution -- push TWO frameArith frames (F1/2026-07
				// hardening review; see the frameArith doc comment above).
				// Checked BEFORE the plain "$(" case below so it always
				// wins when both would otherwise match.
				stack = append(stack, frameArith, frameArith)
				i += 3
			case c == '$' && i+1 < len(line) && line[i+1] == '(':
				stack = append(stack, frameSubst)
				i += 2
			case c == '(' && top() == frameArith:
				// A literal grouping paren inside an arithmetic expression
				// (`$(( (a+b) * c ))`) is one more nesting level, popped by
				// its own matching `)` below -- not a subshell open, and
				// not (yet) a word boundary (see the bare '(' case further
				// down, which this must be checked before).
				stack = append(stack, frameArith)
				i++
			case c == ')' && top() == frameArith:
				stack = stack[:len(stack)-1]
				i++
			case c == ')' && top() == frameSubst:
				stack = stack[:len(stack)-1]
				i++
			case c == '(':
				// A bare '(' (subshell open, not part of "$(") IS a genuine
				// bash word boundary (F4/2026-07 hardening review; verified
				// against real bash: `(#<<NEVERCLOSES` / `echo x` / `)` /
				// a real heredoc exits 0 with exactly the real heredoc
				// found -- the `<<NEVERCLOSES` fragment right after '(' is
				// inert comment text, not a phantom opener). Must be
				// checked AFTER the frameArith-nesting case above so a
				// literal paren inside arithmetic is not double-handled.
				wordStart = true
				i++
			case c == ')':
				// A bare ')' that is NOT closing a "$(...)" substitution --
				// e.g. a case-pattern terminator ("*)") -- is also a
				// genuine word boundary (F4/2026-07 hardening review;
				// verified against real bash with a case-pattern `)`).
				// Deliberately does NOT cover the frameSubst-closing case
				// above: real bash concatenates a command substitution's
				// result into the surrounding word (`$(true)#<<FAKE` is
				// verified to phantom-open a REAL heredoc from the `<<FAKE`
				// fragment, i.e. that ')' is NOT a word boundary), so that
				// case is intentionally left as-is.
				wordStart = true
				i++
			case c == '<':
				if top() == frameArith {
					// `<<` inside arithmetic is the shift operator, never a
					// heredoc opener (F1/2026-07 hardening review) -- skip
					// one '<' at a time; the next iteration reprocesses
					// whatever follows normally.
					i++
					continue
				}
				if i+1 >= len(line) || line[i+1] != '<' {
					// A bare '<' (redirection operator, not part of "<<")
					// is a genuine bash word boundary too (F4/2026-07
					// hardening review; verified against real bash: `cat
					// <#<<FAKE` is a syntax error because the comment eats
					// the mandatory redirection target, which is only
					// possible if bash's own lexer treats '#' right after a
					// bare '<' as a real word start).
					wordStart = true
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
				// the gate exists to block.
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
				// set, plus '(', ')', '<', and '>' above, is
				// isShellWordSeparator (see scanner_delim.go) —
				// deliberately narrow, covering only the bytes actually
				// verified against real bash, not every bash operator (see
				// AGENTS.md).
				wordStart = true
				i++
			case c == '>':
				// A bare '>' (output-redirection operator) is a genuine
				// bash word boundary too (F4/2026-07 hardening review;
				// verified against real bash the same way as bare '<'
				// above: `cat >#<<FAKE` is a syntax error for the identical
				// reason -- the comment eats the mandatory redirection
				// target).
				wordStart = true
				i++
			default:
				i++
			}
		}
	}
	return openers, stack, continuesOnNextLine
}
