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
// each language parser to report ns/op and allocs/op against a ~10K-LOC input
// so the numbers reflect repo-scale parse cost, not a tiny-file fast path.
const minBenchmarkLOC = 10_000

// parseBenchCase binds one language sub-benchmark to its real regression
// fixture and the file extension the registry dispatches on. The fixture is the
// gzip-then-base64 corpus at tests/fixtures/parsers; the extension determines
// which Definition LookupByPath selects, so it must match a registered parser.
type parseBenchCase struct {
	// language is the sub-benchmark name reported by b.Run.
	language string
	// fixture is the base name under tests/fixtures/parsers (without the
	// .gz.b64 suffix), e.g. "golang_regression.go".
	fixture string
	// ext is the on-disk extension used when materializing the padded input so
	// the registry dispatches to the intended language parser.
	ext string
}

// parseBenchCases enumerates every language parser dispatched by the engine.
// The table is explicit so a missing or renamed fixture surfaces as a loud
// b.Fatalf in its own sub-benchmark, never a silent coverage gap. TypeScript
// and TSX are distinct registry definitions; both route through
// parseJavaScriptLike but dispatch on different extensions, so each gets its own
// case. There is no standalone .ts regression fixture, so the typescript case
// reuses the TSX corpus written with a .ts extension (valid TS that exercises
// the same parser entry point).
var parseBenchCases = []parseBenchCase{
	{language: "c", fixture: "c_regression.c", ext: ".c"},
	{language: "cpp", fixture: "cpp_regression.cpp", ext: ".cpp"},
	{language: "csharp", fixture: "csharp_regression.cs", ext: ".cs"},
	{language: "dart", fixture: "dart_regression.dart", ext: ".dart"},
	{language: "elixir", fixture: "elixir_regression.ex", ext: ".ex"},
	{language: "go", fixture: "golang_regression.go", ext: ".go"},
	{language: "groovy", fixture: "groovy_regression.groovy", ext: ".groovy"},
	{language: "haskell", fixture: "haskell_regression.hs", ext: ".hs"},
	{language: "java", fixture: "java_regression.java", ext: ".java"},
	{language: "javascript", fixture: "javascript_regression.js", ext: ".js"},
	{language: "kotlin", fixture: "kotlin_regression.kt", ext: ".kt"},
	{language: "perl", fixture: "perl_regression.pl", ext: ".pl"},
	{language: "php", fixture: "php_regression.php", ext: ".php"},
	{language: "python", fixture: "python_regression.py", ext: ".py"},
	{language: "ruby", fixture: "ruby_regression.rb", ext: ".rb"},
	{language: "rust", fixture: "rust_regression.rs", ext: ".rs"},
	{language: "scala", fixture: "scala_regression.scala", ext: ".scala"},
	{language: "sql", fixture: "sql_regression.sql", ext: ".sql"},
	{language: "swift", fixture: "swift_regression.swift", ext: ".swift"},
	{language: "tsx", fixture: "typescriptjsx_search_domain_regression.tsx", ext: ".tsx"},
	{language: "typescript", fixture: "typescriptjsx_search_domain_regression.tsx", ext: ".ts"},
}

// BenchmarkParse reports parse cost (ns/op, B/op, allocs/op), throughput (MB/s
// via b.SetBytes), and input size (LOC) for every language parser against a
// ~10K-LOC input sourced from the real regression fixtures. Real code shape
// exercises real parser branches that uniform synthetic input would not, so the
// numbers track production parse cost. The benchmark is credential-free and
// deterministic: it decodes a committed fixture, pads it to >= 10K LOC, writes
// it under b.TempDir(), and parses it through the shared engine, whose
// tree-sitter parser pool is the production entry point.
func BenchmarkParse(b *testing.B) {
	engine, err := DefaultEngine()
	if err != nil {
		b.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	for _, tc := range parseBenchCases {
		b.Run(tc.language, func(b *testing.B) {
			source, loc := loadBenchSource(b, tc.fixture)
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

// loadBenchSource decodes the named regression fixture and repeats its content
// until the result reaches at least minBenchmarkLOC lines. It returns the padded
// bytes and the final line count. A missing or corrupt fixture is a fatal error
// in the calling sub-benchmark so coverage gaps are never silent.
func loadBenchSource(b *testing.B, fixture string) ([]byte, int) {
	b.Helper()
	decoded := decodeRegressionFixture(b, fixture)
	if len(bytes.TrimSpace(decoded)) == 0 {
		b.Fatalf("fixture %q decoded to empty content", fixture)
	}

	unit := decoded
	if !bytes.HasSuffix(unit, []byte("\n")) {
		unit = append(unit, '\n')
	}
	baseLines := bytes.Count(unit, []byte("\n"))
	if baseLines == 0 {
		b.Fatalf("fixture %q has no lines after decode", fixture)
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
