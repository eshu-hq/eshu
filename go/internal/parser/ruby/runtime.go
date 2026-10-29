// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
)

const (
	rubyRuntimeMaxBytes  = 1 << 20
	rubyRuntimeMaxTokens = 1024
)

// RubyRuntimeValue is a literal Bundler interpreter declaration.
type RubyRuntimeValue struct {
	SourceKind, Value string
	Line              int
}

// RubyRuntimeEvidence is private, bounded parser output tied to the source bytes.
type RubyRuntimeEvidence struct {
	Values               []RubyRuntimeValue
	Rejected             int
	State                string
	ByteSize, TokenCount int
	Digest               string
}

// RubyRuntimeDigest identifies the exact source revision, including line endings.
func RubyRuntimeDigest(source []byte) string {
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}

func newRubyRuntimeEvidence(source []byte) RubyRuntimeEvidence {
	e := RubyRuntimeEvidence{State: "parsed_empty", ByteSize: len(source), Digest: RubyRuntimeDigest(source)}
	if len(source) > rubyRuntimeMaxBytes {
		e.State = "over_cap"
	}
	return e
}

func (e *RubyRuntimeEvidence) add(kind, value string, line int) {
	e.TokenCount++
	if e.State == "over_cap" || e.State == "unavailable" {
		return
	}
	if value == "" {
		e.Rejected++
		return
	}
	if e.TokenCount > rubyRuntimeMaxTokens {
		e.Values = nil
		e.State = "over_tokens"
		return
	}
	if e.State == "over_tokens" {
		return
	}
	e.Values = append(e.Values, RubyRuntimeValue{SourceKind: kind, Value: strings.Clone(value), Line: line})
	e.State = "parsed_values"
}

func rubyGemfileDirective(line string) (string, bool) {
	if !strings.HasPrefix(line, "ruby") || len(line) < 5 || (line[4] != ' ' && line[4] != '\t' && line[4] != '(') {
		return "", false
	}
	rest := strings.TrimSpace(line[4:])
	if strings.HasPrefix(rest, "(") {
		rest = strings.TrimSpace(rest[1:])
		if !strings.HasSuffix(rest, ")") {
			return "", true
		}
		rest = strings.TrimSpace(strings.TrimSuffix(rest, ")"))
	}
	tokens := bundlerSplitTopLevelCSV(rest)
	values := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if strings.HasPrefix(token, "file:") || strings.HasPrefix(token, "file =>") || strings.HasPrefix(token, ":file =>") {
			return "", true
		}
		value, tail, ok := bundlerReadQuoted(token)
		if !ok || strings.TrimSpace(tail) != "" || strings.ContainsAny(value, "#\\") {
			return "", true
		}
		values = append(values, strings.TrimSpace(value))
	}
	return strings.Join(values, ", "), true
}

func rubyLockVersion(line string) (string, bool) {
	if !strings.HasPrefix(line, "   ruby ") {
		return "", false
	}
	value := strings.TrimSpace(strings.TrimPrefix(line, "   ruby "))
	if value == "" || strings.ContainsAny(value, " \t#\\") {
		return "", true
	}
	return "ruby " + value, true
}

// ExtractBundlerRuntimeEvidence reads one supplied manifest through the same
// Bundler scanner as Parse. It is the content-only collector entrypoint.
func ExtractBundlerRuntimeEvidence(name string, source []byte) RubyRuntimeEvidence {
	payload, ok := parseBundlerPayload(name, source, false)
	if !ok {
		return RubyRuntimeEvidence{State: "unavailable", ByteSize: len(source), Digest: RubyRuntimeDigest(source)}
	}
	evidence := payload["ruby_runtime_evidence"].(RubyRuntimeEvidence)
	if strings.EqualFold(filepath.Base(name), bundlerGemfileName) && len(evidence.Values) > 0 && evidence.State != "over_cap" {
		parser := tree_sitter.NewParser()
		language := tree_sitter.NewLanguage(tree_sitter_ruby.Language())
		if err := parser.SetLanguage(language); err != nil {
			evidence.State = "unavailable"
			evidence.Values = nil
		} else {
			checkGemfileRuntimeSyntax(&evidence, name, source, parser)
		}
		parser.Close()
	}
	return evidence
}
