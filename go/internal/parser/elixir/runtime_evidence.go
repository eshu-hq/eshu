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
// evaluated, and Elixir code is never executed.
func scanMixRuntime(source []byte) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	for lineNo, line := range strings.Split(string(source), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimLeft(line, " \t") == "" || strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		match := mixElixirKeyPattern.FindStringIndex(line)
		if match == nil || mixKeyInsideString(line, match[0]) {
			continue
		}
		rest := strings.TrimLeft(line[match[1]:], " \t")
		if rest == "" || rest[0] != '"' {
			evidence.Rejected++
			continue
		}
		value, tail, ok := mixQuotedLiteral(rest)
		if !ok || !mixLiteralTailClosed(tail) {
			evidence.Rejected++
			continue
		}
		evidence.Add(mixRuntimeSourceKind, "elixir", value, lineNo+1)
	}
	return evidence
}

// mixKeyInsideString reports whether the key match sits inside a
// double-quoted string on the same line, so a description mentioning elixir:
// never becomes a declaration or a rejection. An odd run of unescaped quotes
// before the match means the match is quoted.
func mixKeyInsideString(line string, match int) bool {
	quotes := 0
	for i := 0; i < match; i++ {
		if line[i] == '\\' {
			i++
			continue
		}
		if line[i] == '"' {
			quotes++
		}
	}
	return quotes%2 == 1
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
