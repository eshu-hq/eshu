// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"errors"
	"fmt"
	"strings"
)

// Tokenizer for the required-gates terminal publisher's case arms.
//
// # Why this is deliberately narrow
//
// #6194 recorded nine review rounds spent growing a TEXTUAL model of bash --
// whitespace, then seven metacharacters, then backticks -- one bypass at a
// time, and it never converged: that model is a re-implementation of bash's
// lexer in string handling, and every round found another spelling it read
// wrong. This file does not try again. It declares a small closed grammar,
// parses exactly that, and REFUSES to judge anything outside it. An arm the
// scanner cannot parse becomes a loud validation error, never a permissive
// default, so an unmodelled construct fails the gate instead of being
// silently mis-read.
//
// # The accepted grammar
//
//	line       := [ statement { separator statement } ] [ comment ]
//	separator  := a run of ";", "&" and "|" (so ";", "&&" and "||" all separate)
//	statement  := word { blank word }
//	word       := one or more segments with no blank between them
//	segment    := unquoted-run | "'" not-a-quote "'" | `"` not-a-quote `"`
//	comment    := "#" starting a word, through end of line
//
// `;;` is matched before `;` and ends the arm: it is the case-arm terminator,
// and what follows belongs to the next arm.
//
// Single quotes run literally to the closing quote, which is what bash does.
// Double quotes are literal here too, because the three constructs that would
// make them otherwise -- a backslash escape, a backtick, and `$(` -- are
// rejected rather than modelled.
//
// # Deliberately not handled; each returns an error
//
//   - a quote left open at end of line. Quotes do not span lines here.
//   - a backslash outside single quotes. It escapes the next character, and at
//     end of line it continues onto the next one.
//   - a backtick or `$(`. Command substitution can produce an assignment this
//     scanner cannot see.
//   - `(`, `)`, `<` and `>`. Grouping, subshells and redirection all change
//     which word starts a statement.
//
// Parameter expansion (`${state}`, `$state`) is NOT rejected; it is carried
// through as literal word text. That is safe in this direction. An arm
// assigning `state="${x}"` reads as the value `${x}`, which is neither `error`
// nor `failure`, so the cancelled-arm check reports it rather than passing it.

// errUnparseableShell marks input outside the grammar above. Callers turn it
// into a validation error rather than falling back to a guess.
var errUnparseableShell = errors.New("outside the shell shapes this validator parses")

// unmodelledShellMeta are the characters that carry shell meaning this scanner
// does not model, and so rejects when it meets one outside quotes.
const unmodelledShellMeta = "\\`()<>"

// shellWord is one word of a statement: the text with quoting removed, plus
// how many leading bytes of that text came from UNQUOTED input. Bash treats
// `NAME=VALUE` as an assignment only when the `NAME=` half is unquoted --
// `"state=success"` is a command name, not an assignment -- so the two halves
// have to stay distinguishable once the quotes are gone.
type shellWord struct {
	text     string
	unquoted int
}

// shellStatement is one `;`/`&&`/`||`-separated statement, in word order.
type shellStatement []shellWord

// scanShellLine parses one line of the accepted grammar into its statements.
// terminated reports that a top-level `;;` ended the line, which for a case
// arm means the arm is over. A line outside the grammar returns
// errUnparseableShell wrapped with the reason.
func scanShellLine(line string) (statements []shellStatement, terminated bool, err error) {
	var (
		words    shellStatement
		text     strings.Builder
		unquoted int
		quoted   bool
		inWord   bool
	)
	flushWord := func() {
		if !inWord {
			return
		}
		words = append(words, shellWord{text: text.String(), unquoted: unquoted})
		text.Reset()
		unquoted, quoted, inWord = 0, false, false
	}
	flushStatement := func() {
		flushWord()
		if len(words) > 0 {
			statements = append(statements, words)
			words = nil
		}
	}
	for i := 0; i < len(line); {
		c := line[i]
		switch {
		case c == ' ' || c == '\t':
			flushWord()
			i++
		case c == '#' && !inWord:
			// Bash starts a comment at a `#` that begins a word. One inside a
			// word (`PR#6218`) is an ordinary character.
			flushStatement()
			return statements, false, nil
		case strings.HasPrefix(line[i:], ";;"):
			flushStatement()
			return statements, true, nil
		case c == ';' || c == '&' || c == '|':
			flushStatement()
			for i < len(line) && (line[i] == ';' || line[i] == '&' || line[i] == '|') {
				i++
			}
		case c == '\'' || c == '"':
			segment, width, quoteErr := scanQuotedSegment(line[i:])
			if quoteErr != nil {
				return nil, false, quoteErr
			}
			text.WriteString(segment)
			quoted, inWord = true, true
			i += width
		case strings.IndexByte(unmodelledShellMeta, c) >= 0:
			return nil, false, fmt.Errorf("%w: %q outside quotes", errUnparseableShell, string(c))
		default:
			text.WriteByte(c)
			if !quoted {
				unquoted++
			}
			inWord = true
			i++
		}
	}
	flushStatement()
	return statements, false, nil
}

// scanQuotedSegment reads the quoted segment that opens s, returning its
// literal contents and how many bytes of s it consumed including both quotes.
func scanQuotedSegment(s string) (string, int, error) {
	quote := s[0]
	end := strings.IndexByte(s[1:], quote)
	if end < 0 {
		name := "single"
		if quote == '"' {
			name = "double"
		}
		return "", 0, fmt.Errorf("%w: unterminated %s quote", errUnparseableShell, name)
	}
	segment := s[1 : 1+end]
	if quote == '"' {
		// A single-quoted string is literal in bash, so nothing inside one can
		// surprise us. A double-quoted one still expands, and these three are
		// the constructs that would make its contents not what they look like.
		if bad := strings.IndexAny(segment, "\\`"); bad >= 0 {
			return "", 0, fmt.Errorf("%w: %q inside double quotes", errUnparseableShell, string(segment[bad]))
		}
		if strings.Contains(segment, "$(") {
			return "", 0, fmt.Errorf("%w: command substitution inside double quotes", errUnparseableShell)
		}
	}
	return segment, end + 2, nil
}

// shellAssignment reports the `NAME=VALUE` an assignment word carries. It is
// an assignment only when the `=` and the name before it came through
// unquoted, because bash reads `"state=success"` as the name of a command to
// run, not as a variable to set.
func shellAssignment(word shellWord) (name, value string, ok bool) {
	eq := strings.IndexByte(word.text, '=')
	if eq <= 0 || eq >= word.unquoted {
		return "", "", false
	}
	name = word.text[:eq]
	for i := 0; i < len(name); i++ {
		if !isShellNameByte(name[i]) || (i == 0 && name[i] >= '0' && name[i] <= '9') {
			return "", "", false
		}
	}
	return name, word.text[eq+1:], true
}

// isShellNameByte reports whether c may appear in a shell variable name.
// Callers exclude a digit in first position themselves.
func isShellNameByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
