// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elixir

import (
	"regexp"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_elixir "github.com/tree-sitter/tree-sitter-elixir/bindings/go"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// mixRuntimeSourceKind names the mix.exs elixir: requirement evidence. It is
// the only mix.exs version field this package declares (#9308 Others row).
const mixRuntimeSourceKind = "mix_exs_elixir"

// mixElixirKeyPattern finds an elixir: keyword outside a larger word. A
// leading :elixir atom or a longer name never matches, so only the project
// keyword form is read.
var mixElixirKeyPattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9_:])elixir\s*:`)

// ExtractMixRuntimeEvidence reads one supplied mix.exs body for content-only
// callers. Native parsing attaches the same evidence on its existing decode.
func ExtractMixRuntimeEvidence(source []byte) shared.RuntimeEvidence {
	evidence := scanMixRuntime(source)
	if len(evidence.Values) == 0 || evidence.State == "over_cap" {
		return evidence
	}
	if mixHasSyntaxError(source) {
		evidence.State = "unavailable"
		evidence.Values = nil
	}
	return evidence
}

// mixRuntimeEvidence binds scanner output to a caller-proven syntax verdict,
// so the native parse reads the tree once for symbols and evidence together.
// A file whose tree holds an error cannot establish runtime truth.
func mixRuntimeEvidence(source []byte, hasSyntaxError bool) shared.RuntimeEvidence {
	evidence := scanMixRuntime(source)
	if hasSyntaxError && len(evidence.Values) > 0 && evidence.State != "over_cap" {
		evidence.State = "unavailable"
		evidence.Values = nil
	}
	return evidence
}

// scanMixRuntime keeps every literal double-quoted elixir: value with its
// source line. Anything else on the key is fail-closed and counted: module
// attributes, calls, charlists, and unterminated or escaped strings are never
// evaluated, key matches inside triple-quoted heredoc regions are never read,
// and Elixir code is never executed.
func scanMixRuntime(source []byte) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	lines := strings.Split(string(source), "\n")
	heredoc := ""
	for lineNo, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if heredoc != "" {
			idx := mixHeredocCloser(line, heredoc)
			if idx < 0 {
				continue
			}
			line = line[idx+len(heredoc):]
			heredoc = ""
		}
		if strings.TrimLeft(line, " \t") == "" || strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		if delim, pos, ok := mixEntersHeredoc(line); ok {
			heredoc = delim
			if match := mixElixirKeyPattern.FindStringIndex(line); match != nil && match[0] >= pos {
				continue
			}
		}
		match := mixElixirKeyPattern.FindStringIndex(line)
		if match == nil || mixKeyInsideString(line, match[0]) || mixKeyAfterComment(line, match[0]) {
			continue
		}
		rest := strings.TrimLeft(line[match[1]:], " \t")
		valueLine := lineNo + 1
		if rest == "" && lineNo+1 < len(lines) {
			rest = strings.TrimLeft(strings.TrimSuffix(lines[lineNo+1], "\r"), " \t")
			valueLine = lineNo + 2
		}
		if rest == "" || rest[0] != '"' {
			evidence.Rejected++
			continue
		}
		value, tail, ok := mixQuotedLiteral(rest)
		if !ok || !mixLiteralTailClosed(tail) {
			evidence.Rejected++
			continue
		}
		evidence.Add(mixRuntimeSourceKind, "elixir", value, valueLine)
	}
	return evidence
}

// mixKeyInsideString reports whether the key match sits inside a quoted
// string on the same line, so a description mentioning elixir: never becomes
// a declaration or a rejection. Both double-quoted strings and single-quoted
// charlists open a quoted region; a quote of one kind inside the other kind's
// region is literal text.
func mixKeyInsideString(line string, match int) bool {
	inSingle, inDouble := false, false
	for i := 0; i < match; i++ {
		switch line[i] {
		case '\\':
			i++
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		}
	}
	return inSingle || inDouble
}

// mixKeyAfterComment reports whether the key match sits after an unquoted #
// on the same line, so a trailing comment mentioning elixir: never becomes a
// declaration or a rejection. A # inside a quoted region is literal text.
func mixKeyAfterComment(line string, match int) bool {
	inSingle, inDouble := false, false
	for i := 0; i <= match; i++ {
		switch line[i] {
		case '\\':
			i++
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return true
			}
		}
	}
	return false
}

// mixHeredocOpener finds the first triple-quoted heredoc opener in the code
// portion of line: outside single-line strings and before any unquoted #.
// Sigils stay out of scope for this reader. Returns the byte offset and the
// delimiter, which is either triple double quotes or triple single quotes.
func mixHeredocOpener(line string) (int, string, bool) {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '\'':
			if !inDouble {
				if !inSingle && strings.HasPrefix(line[i:], "'''") {
					return i, "'''", true
				}
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				if !inDouble && strings.HasPrefix(line[i:], `"""`) {
					return i, `"""`, true
				}
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return 0, "", false
			}
		}
	}
	return 0, "", false
}

// mixHeredocCloser finds the closing delimiter in a heredoc body line. An
// escaped delimiter is heredoc content, not the end of the region.
func mixHeredocCloser(line, delim string) int {
	for i := 0; i+len(delim) <= len(line); i++ {
		if line[i] == '\\' {
			i++
			continue
		}
		if strings.HasPrefix(line[i:], delim) {
			return i
		}
	}
	return -1
}

// mixEntersHeredoc reports whether line opens a triple-quoted heredoc that
// does not close on the same line, with the opener offset. A single-line
// heredoc never spans lines, so only an unclosed opener puts later lines
// inside a heredoc region; the existing single-line guards already read keys
// around a same-line pair.
func mixEntersHeredoc(line string) (string, int, bool) {
	base := 0
	for base < len(line) {
		pos, delim, ok := mixHeredocOpener(line[base:])
		if !ok {
			return "", 0, false
		}
		pos += base
		rest := line[pos+len(delim):]
		if idx := mixHeredocCloser(rest, delim); idx >= 0 {
			base = pos + len(delim) + idx + len(delim)
			continue
		}
		return delim, pos, true
	}
	return "", 0, false
}

// mixQuotedLiteral reads one double-quoted literal starting at rest[0]. A
// backslash rejects the value outright: requirement strings hold no escapes,
// so anything escaped is not the plain version form this reader owns. The
// literal never spans lines.
func mixQuotedLiteral(rest string) (value, tail string, ok bool) {
	for i := 1; i < len(rest); i++ {
		switch rest[i] {
		case '\\':
			return "", "", false
		case '"':
			return rest[1:i], rest[i+1:], true
		}
	}
	return "", "", false
}

// mixLiteralTailClosed reports whether the text after a requirement literal is
// only its list punctuation or a comment. Anything else (an operator, a
// second string, trailing code) is a dynamic expression and fails closed.
func mixLiteralTailClosed(tail string) bool {
	tail = strings.TrimLeft(tail, " \t,]")
	if tail == "" {
		return true
	}
	return strings.HasPrefix(tail, "#")
}

// mixHasSyntaxError reports whether the Elixir tree for source holds an
// error. A broken file cannot locate its requirement, so its values are
// unavailable rather than absent.
func mixHasSyntaxError(source []byte) bool {
	parser := tree_sitter.NewParser()
	language := tree_sitter.NewLanguage(tree_sitter_elixir.Language())
	if err := parser.SetLanguage(language); err != nil {
		parser.Close()
		return true
	}
	defer parser.Close()
	tree := parser.Parse(source, nil)
	if tree == nil {
		return true
	}
	defer tree.Close()
	return tree.RootNode().HasError()
}
