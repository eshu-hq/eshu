// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// retiredLanguageExtensionFixture is one row of the extension map the
// language-query builders used to fall back on (`languageFileExtensions`,
// deleted with the `f.name ENDS WITH` terms in #6546). Each row is an
// extension the query DSL promised for a language, with a minimal source the
// engine for that extension accepts.
type retiredLanguageExtensionFixture struct {
	language  string
	extension string
	source    string
}

// retiredLanguageExtensionFixtures lists every (language, extension) pair of
// the retired map except `.lhs`, which TestLiterateHaskellStaysUnregistered
// covers. `.h` sat under both `c` and `cpp` there; the parser registry binds
// an extension once and binds `.h` to cpp, so it is listed under cpp here and
// TestRetiredHeaderExtensionIsTaggedCpp pins that choice.
var retiredLanguageExtensionFixtures = []retiredLanguageExtensionFixture{
	{"c", ".c", "int main(void) { return 0; }\n"},
	{"cpp", ".cpp", "int main() { return 0; }\n"},
	{"cpp", ".cc", "int main() { return 0; }\n"},
	{"cpp", ".cxx", "int main() { return 0; }\n"},
	{"cpp", ".hpp", "#pragma once\nint answer();\n"},
	{"cpp", ".hxx", "#pragma once\nint answer();\n"},
	{"cpp", ".h", "#pragma once\nint answer();\n"},
	{"csharp", ".cs", "class Answer { }\n"},
	{"dart", ".dart", "void main() {}\n"},
	{"elixir", ".ex", "defmodule Answer do\nend\n"},
	{"elixir", ".exs", "defmodule Answer do\nend\n"},
	{"go", ".go", "package answer\n"},
	{"haskell", ".hs", "main :: IO ()\nmain = return ()\n"},
	{"java", ".java", "class Answer {}\n"},
	{"javascript", ".js", "function answer() {}\n"},
	{"javascript", ".jsx", "const Answer = () => <div />;\n"},
	{"javascript", ".mjs", "export function answer() {}\n"},
	{"javascript", ".cjs", "module.exports = function answer() {};\n"},
	{"hcl", ".hcl", "variable \"answer\" {}\n"},
	{"kotlin", ".kt", "fun answer() {}\n"},
	{"kotlin", ".kts", "println(\"answer\")\n"},
	{"perl", ".pl", "sub answer { 1 }\n1;\n"},
	{"perl", ".pm", "package Answer;\nsub answer { 1 }\n1;\n"},
	{"php", ".php", "<?php\nfunction answer() {}\n"},
	{"python", ".py", "def answer():\n    pass\n"},
	{"python", ".pyi", "def answer() -> None: ...\n"},
	{"ruby", ".rb", "def answer\nend\n"},
	{"rust", ".rs", "fn answer() {}\n"},
	{"scala", ".scala", "object Answer\n"},
	{"scala", ".sc", "object Answer\n"},
	{"sql", ".sql", "SELECT 1;\n"},
	{"swift", ".swift", "func answer() {}\n"},
	{"typescript", ".ts", "function answer(): void {}\n"},
	{"typescript", ".tsx", "const Answer = () => <div />;\n"},
}

// TestRetiredExtensionsParseToAnAdmittedLanguageSpelling pins the producer
// side of #6578: with the extension fallback gone, `f.language IN $languages`
// is the only predicate the builders carry, so every extension the retired
// map promised for a language must reach the graph with a non-empty
// `language` the builders admit for that language. The check runs the
// production parse path (parser.Engine.ParsePath on the default registry) and
// reads the payload the way the collector does, then asks
// graphLanguageSpellings whether that spelling is bound for the query.
func TestRetiredExtensionsParseToAnAdmittedLanguageSpelling(t *testing.T) {
	t.Parallel()

	engine := newDefaultParserEngine(t)
	for _, tt := range retiredLanguageExtensionFixtures {
		t.Run(tt.language+" "+tt.extension, func(t *testing.T) {
			t.Parallel()

			emitted := parseFixtureLanguage(t, engine, tt.extension, tt.source)
			if emitted == "" {
				t.Fatalf("%s fixture parsed with an empty language; the File node would never match `f.language IN $languages` for %q", tt.extension, tt.language)
			}
			admitted := graphLanguageSpellings(tt.language)
			if !slices.Contains(admitted, emitted) {
				t.Fatalf("%s fixture parsed with language %q, which graphLanguageSpellings(%q) = %v does not admit", tt.extension, emitted, tt.language, admitted)
			}
		})
	}
}

// TestRetiredHeaderExtensionIsTaggedCpp pins that a `.h` file is tagged cpp
// and only cpp. The retired map listed `.h` under `c` too, but the registry
// binds an extension to exactly one parser, so a `c` request has never seen a
// header through the property predicate and the builders do not fold cpp into
// the c spelling list to change that.
func TestRetiredHeaderExtensionIsTaggedCpp(t *testing.T) {
	t.Parallel()

	engine := newDefaultParserEngine(t)
	emitted := parseFixtureLanguage(t, engine, ".h", "#pragma once\nint answer();\n")
	if emitted != "cpp" {
		t.Fatalf(".h fixture parsed with language %q, want cpp", emitted)
	}
	if spellings := graphLanguageSpellings("c"); slices.Contains(spellings, "cpp") {
		t.Fatalf("graphLanguageSpellings(\"c\") = %v admits cpp; a c query would return every C++ file", spellings)
	}
}

// TestLiterateHaskellStaysUnregistered pins that `.lhs` is left to no parser
// on purpose. The retired map promised it for haskell, but the Haskell grammar
// has no literate mode: a bird-track file with prose between the `> ` lines
// parses to an empty payload, so binding the extension would write a File
// tagged haskell that carries none of its symbols. A missing File is the
// honest answer until a literate-aware engine exists.
func TestLiterateHaskellStaysUnregistered(t *testing.T) {
	t.Parallel()

	if definition, ok := parser.DefaultRegistry().LookupByExtension(".lhs"); ok {
		t.Fatalf(".lhs resolved to parser %q; literate Haskell is deliberately unregistered", definition.ParserKey)
	}
}

func newDefaultParserEngine(t *testing.T) *parser.Engine {
	t.Helper()
	engine, err := parser.NewEngine(parser.DefaultRegistry(), parser.NewRuntime())
	if err != nil {
		t.Fatalf("parser.NewEngine() error = %v, want nil", err)
	}
	return engine
}

// parseFixtureLanguage writes source to a file with the given extension in a
// fresh repository root, parses it through the production engine, and returns
// the language the collector would stamp on the File: the payload's
// `language` key, else `lang`, trimmed (gitrepo.snapshotPayloadString).
func parseFixtureLanguage(t *testing.T, engine *parser.Engine, extension, source string) string {
	t.Helper()
	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "answer"+extension)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
	payload, err := engine.ParsePath(repoRoot, path, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v; the collector skips this file and no File node is written", extension, err)
	}
	for _, key := range []string{"language", "lang"} {
		if text, ok := payload[key].(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}
