// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// benchExemptParsers lists registered parser keys that BenchmarkParse
// deliberately does not benchmark, each with a concrete reason. A parser
// belongs here only when a parse-cost number would be meaningless or
// fabricated; prefer adding a real sub-benchmark over an exemption.
//
// The map is the single source of truth for "intentionally not benchmarked".
// TestBenchmarkParseCoversEveryRegisteredParser fails if a registered parser is
// neither benchmarked nor exempted, so a newly registered parser can never
// silently drop out of the ns/op coverage matrix.
var benchExemptParsers = map[string]string{
	"java_metadata": "java_metadata is a path-pattern pseudo-parser for META-INF " +
		"service/spring descriptor files (LookupByPath routes by exact path, not by " +
		"extension or content). It performs no tree-sitter parse and emits a fixed " +
		"metadata payload, so a synthesized >=10K-LOC input would measure file I/O " +
		"plus map allocation, not parse cost.",
	"raw_text": "raw_text is an opaque passthrough: parseRawText captures content_body " +
		"verbatim for downstream relationship extraction without invoking any grammar. " +
		"Its cost is a single os.ReadFile, so an ns/op figure would track I/O, not parse work.",
}

// registeredParserKeys returns every parser key in the default registry in
// deterministic order so coverage assertions are stable across runs.
func registeredParserKeys(t *testing.T) []string {
	t.Helper()
	keys := DefaultRegistry().ParserKeys()
	if len(keys) == 0 {
		t.Fatal("DefaultRegistry().ParserKeys() returned no parsers")
	}
	sort.Strings(keys)
	return keys
}

// benchmarkedParserKeys returns the set of parser keys covered by either the
// language or the config/manifest benchmark tables.
func benchmarkedParserKeys() map[string]string {
	covered := make(map[string]string, len(parseBenchCases)+len(configBenchCases))
	for _, tc := range parseBenchCases {
		covered[tc.parserKey] = "language"
	}
	for _, tc := range configBenchCases {
		covered[tc.parserKey] = "config"
	}
	return covered
}

// TestBenchmarkParseCoversEveryRegisteredParser is the coverage guard for
// B-3 (#3796): every registered parser definition MUST be either benchmarked by
// BenchmarkParse / BenchmarkParseConfig or listed in benchExemptParsers with a
// non-empty reason. A registered parser that is neither is a silent gap in the
// parse-cost matrix and fails this test.
func TestBenchmarkParseCoversEveryRegisteredParser(t *testing.T) {
	covered := benchmarkedParserKeys()

	var gaps []string
	for _, key := range registeredParserKeys(t) {
		if _, ok := covered[key]; ok {
			continue
		}
		if reason := strings.TrimSpace(benchExemptParsers[key]); reason != "" {
			continue
		}
		gaps = append(gaps, key)
	}

	if len(gaps) > 0 {
		t.Fatalf("registered parsers without a benchmark or documented exemption: %v\n"+
			"add a case to parseBenchCases/configBenchCases or an entry to benchExemptParsers with a reason", gaps)
	}
}

// TestBenchExemptParsersAreRegisteredAndNotBenchmarked keeps the exemption map
// honest: every exemption MUST name a real registered parser, MUST carry a
// non-empty reason, and MUST NOT also appear in a benchmark table (which would
// make the exemption misleading dead config).
func TestBenchExemptParsersAreRegisteredAndNotBenchmarked(t *testing.T) {
	registered := make(map[string]struct{})
	for _, key := range registeredParserKeys(t) {
		registered[key] = struct{}{}
	}
	covered := benchmarkedParserKeys()

	for key, reason := range benchExemptParsers {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("benchExemptParsers[%q] has an empty reason", key)
		}
		if _, ok := registered[key]; !ok {
			t.Errorf("benchExemptParsers[%q] is not a registered parser key", key)
		}
		if lane, ok := covered[key]; ok {
			t.Errorf("benchExemptParsers[%q] is also benchmarked in the %q table; "+
				"remove the exemption or the benchmark case", key, lane)
		}
	}
}

// TestTypeScriptBenchFixtureParsesClean proves the typescript benchmark fixture
// is genuinely JSX-free TypeScript: parsed through the typescript grammar (the
// same grammar BenchmarkParse/typescript dispatches to), the tree has no error
// nodes. This guards Finding 2 (#4128) — the prior fixture reused the TSX corpus
// with a .ts extension, where JSX is invalid and the benchmark silently measured
// tree-sitter error recovery instead of normal TypeScript parsing. A clean parse
// here means the benchmark measures the normal parse path.
func TestTypeScriptBenchFixtureParsesClean(t *testing.T) {
	path := filepath.Join("testdata", "bench", "typescript.ts")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read typescript bench fixture %q: %v", path, err)
	}
	if len(strings.TrimSpace(string(source))) == 0 {
		t.Fatalf("typescript bench fixture %q is empty", path)
	}

	runtime := NewRuntime()
	parser, err := runtime.Parser("typescript")
	if err != nil {
		t.Fatalf("runtime.Parser(typescript) error = %v, want nil", err)
	}
	defer runtime.PutParser("typescript", parser)

	tree := parser.Parse(source, nil)
	defer tree.Close()

	root := tree.RootNode()
	if root.HasError() {
		t.Fatalf("typescript bench fixture %q parsed with tree-sitter error nodes; "+
			"it must be valid JSX-free TypeScript so the benchmark measures normal parsing, "+
			"not error recovery", path)
	}
}
