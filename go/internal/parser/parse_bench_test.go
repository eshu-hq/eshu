// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minBenchmarkLOC is the floor for benchmark input size. B-3 (#3796) requires
// each parser to report ns/op and allocs/op against a ~10K-LOC input so the
// numbers reflect repo-scale parse cost, not a tiny-file fast path.
const minBenchmarkLOC = 10_000

// parseBenchCase binds one language sub-benchmark to its real regression
// fixture and the file extension the registry dispatches on. The fixture is the
// gzip-then-base64 corpus at tests/fixtures/parsers; the extension determines
// which Definition LookupByPath selects, so it must match a registered parser.
//
// parserKey is the registry identity the case covers. It anchors the coverage
// guard (TestBenchmarkParseCoversEveryRegisteredParser) to the registry's own
// notion of a parser rather than to the on-disk extension, so a registered
// parser can never silently drop out of the ns/op matrix.
type parseBenchCase struct {
	// language is the sub-benchmark name reported by b.Run.
	language string
	// parserKey is the registered Definition.ParserKey this case benchmarks.
	parserKey string
	// fixture is the base name under tests/fixtures/parsers (without the
	// .gz.b64 suffix), e.g. "golang_regression.go". Mutually exclusive with
	// testdataFile.
	fixture string
	// testdataFile is a package-local plain-text source under
	// internal/parser/testdata/bench, used when no gzip corpus fixture exists or
	// when the corpus would route through the wrong grammar. Mutually exclusive
	// with fixture.
	testdataFile string
	// ext is the on-disk extension used when materializing the padded input so
	// the registry dispatches to the intended language parser.
	ext string
}

// parseBenchCases enumerates the tree-sitter language parsers dispatched by the
// engine. The table is explicit so a missing or renamed fixture surfaces as a
// loud b.Fatalf in its own sub-benchmark, never a silent coverage gap.
// TypeScript and TSX are distinct registry definitions; both route through
// parseJavaScriptLike but dispatch on different extensions, so each gets its own
// case. The typescript case uses a committed JSX-free TypeScript fixture under
// testdata/bench (not the TSX corpus) so it measures normal TypeScript parsing
// rather than tree-sitter error recovery on invalid JSX. Config/manifest
// parsers live in configBenchCases (parse_bench_config_test.go).
var parseBenchCases = []parseBenchCase{
	{language: "c", parserKey: "c", fixture: "c_regression.c", ext: ".c"},
	{language: "cpp", parserKey: "cpp", fixture: "cpp_regression.cpp", ext: ".cpp"},
	{language: "csharp", parserKey: "c_sharp", fixture: "csharp_regression.cs", ext: ".cs"},
	{language: "dart", parserKey: "dart", fixture: "dart_regression.dart", ext: ".dart"},
	{language: "elixir", parserKey: "elixir", fixture: "elixir_regression.ex", ext: ".ex"},
	{language: "go", parserKey: "go", fixture: "golang_regression.go", ext: ".go"},
	{language: "groovy", parserKey: "groovy", fixture: "groovy_regression.groovy", ext: ".groovy"},
	{language: "haskell", parserKey: "haskell", fixture: "haskell_regression.hs", ext: ".hs"},
	{language: "java", parserKey: "java", fixture: "java_regression.java", ext: ".java"},
	{language: "javascript", parserKey: "javascript", fixture: "javascript_regression.js", ext: ".js"},
	{language: "kotlin", parserKey: "kotlin", fixture: "kotlin_regression.kt", ext: ".kt"},
	{language: "perl", parserKey: "perl", fixture: "perl_regression.pl", ext: ".pl"},
	{language: "php", parserKey: "php", fixture: "php_regression.php", ext: ".php"},
	{language: "python", parserKey: "python", fixture: "python_regression.py", ext: ".py"},
	{language: "ruby", parserKey: "ruby", fixture: "ruby_regression.rb", ext: ".rb"},
	{language: "rust", parserKey: "rust", fixture: "rust_regression.rs", ext: ".rs"},
	{language: "scala", parserKey: "scala", fixture: "scala_regression.scala", ext: ".scala"},
	{language: "sql", parserKey: "sql", fixture: "sql_regression.sql", ext: ".sql"},
	{language: "swift", parserKey: "swift", fixture: "swift_regression.swift", ext: ".swift"},
	{language: "tsx", parserKey: "tsx", fixture: "typescriptjsx_search_domain_regression.tsx", ext: ".tsx"},
	{language: "typescript", parserKey: "typescript", testdataFile: "typescript.ts", ext: ".ts"},
	// __dockerfile__ and __jenkinsfile__ dispatch on exact/prefix filenames, not
	// extensions, so they are benchmarked via configBenchCases which honors
	// Definition.ExactNames.
}

// BenchmarkParse reports parse cost (ns/op, B/op, allocs/op), throughput (MB/s
// via b.SetBytes), and input size (LOC) for every tree-sitter language parser
// against a ~10K-LOC input. Real code shape exercises real parser branches that
// uniform synthetic input would not, so the numbers track production parse cost.
// The benchmark is credential-free and deterministic: it loads a committed
// source (gzip corpus fixture or package-local testdata file), pads it to
// >= 10K LOC, writes it under b.TempDir(), and parses it through the shared
// engine, whose tree-sitter parser pool is the production entry point.
func BenchmarkParse(b *testing.B) {
	engine, err := DefaultEngine()
	if err != nil {
		b.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	for _, tc := range parseBenchCases {
		b.Run(tc.language, func(b *testing.B) {
			source, loc := loadCaseSource(b, tc)
			repoRoot := b.TempDir()
			filePath := filepath.Join(repoRoot, "input"+tc.ext)
			if err := os.WriteFile(filePath, source, 0o644); err != nil {
				b.Fatalf("write %s: %v", filePath, err)
			}

			b.SetBytes(int64(len(source)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := engine.ParsePath(repoRoot, filePath, false, Options{}); err != nil {
					b.Fatalf("ParsePath(%s) error = %v, want nil", tc.language, err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(loc), "LOC")
		})
	}
}

// loadCaseSource loads the source for one language case from its gzip corpus
// fixture or its package-local testdata file, padded to >= minBenchmarkLOC.
func loadCaseSource(b *testing.B, tc parseBenchCase) ([]byte, int) {
	b.Helper()
	if tc.fixture != "" && tc.testdataFile != "" {
		b.Fatalf("case %q sets both fixture and testdataFile; choose one", tc.language)
	}
	if tc.testdataFile != "" {
		return loadTestdataBenchSource(b, tc.testdataFile)
	}
	return loadBenchSource(b, tc.fixture)
}

// loadBenchSource decodes the named regression fixture and repeats its content
// until the result reaches at least minBenchmarkLOC lines. It returns the padded
// bytes and the final line count. A missing or corrupt fixture is a fatal error
// in the calling sub-benchmark so coverage gaps are never silent.
func loadBenchSource(b *testing.B, fixture string) ([]byte, int) {
	b.Helper()
	return padBenchSource(b, decodeRegressionFixture(b, fixture), fixture)
}

// loadTestdataBenchSource reads a package-local benchmark source under
// internal/parser/testdata/bench and pads it to >= minBenchmarkLOC lines.
func loadTestdataBenchSource(b *testing.B, name string) ([]byte, int) {
	b.Helper()
	path := filepath.Join("testdata", "bench", name)
	source, err := os.ReadFile(path) //nolint:gosec // committed package-local test fixture path
	if err != nil {
		b.Fatalf("read testdata bench source %q: %v", path, err)
	}
	return padBenchSource(b, source, name)
}

// padBenchSource repeats decoded source until it reaches at least
// minBenchmarkLOC lines, returning the padded bytes and the final line count.
func padBenchSource(b *testing.B, decoded []byte, name string) ([]byte, int) {
	b.Helper()
	if len(bytes.TrimSpace(decoded)) == 0 {
		b.Fatalf("source %q decoded to empty content", name)
	}

	unit := decoded
	if !bytes.HasSuffix(unit, []byte("\n")) {
		unit = append(unit, '\n')
	}
	baseLines := bytes.Count(unit, []byte("\n"))
	if baseLines == 0 {
		b.Fatalf("source %q has no lines after decode", name)
	}

	repeats := (minBenchmarkLOC + baseLines - 1) / baseLines
	if repeats < 1 {
		repeats = 1
	}

	var builder bytes.Buffer
	builder.Grow(len(unit) * repeats)
	for range repeats {
		builder.Write(unit)
	}
	padded := builder.Bytes()
	return padded, bytes.Count(padded, []byte("\n"))
}

// decodeRegressionFixture reads tests/fixtures/parsers/<fixture>.gz.b64 and
// reverses its base64-then-gzip encoding, returning the original source bytes.
func decodeRegressionFixture(b *testing.B, fixture string) []byte {
	b.Helper()
	path := repoFixturePath("parsers", fixture+".gz.b64")
	encoded, err := os.ReadFile(path) //nolint:gosec // committed test fixture path
	if err != nil {
		b.Fatalf("read fixture %q: %v", path, err)
	}

	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		b.Fatalf("base64-decode fixture %q: %v", fixture, err)
	}

	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		b.Fatalf("gzip reader for fixture %q: %v", fixture, err)
	}
	defer func() { _ = reader.Close() }()

	source, err := io.ReadAll(reader)
	if err != nil {
		b.Fatalf("gunzip fixture %q: %v", fixture, err)
	}
	return source
}
