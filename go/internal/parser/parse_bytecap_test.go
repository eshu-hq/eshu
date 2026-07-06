// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"strings"
	"testing"
)

// jsParseByteCap and phpParseByteCap mirror the unexported byte caps defined
// in the javascript and php sub-packages
// (javascript.jsParseByteCap, php.phpParseByteCap). They are duplicated here
// only as literal test constants because those caps are package-private
// implementation details; both must stay equal to 1 MiB.
const (
	jsParseByteCap  = 1 << 20
	phpParseByteCap = 1 << 20
)

// TestParsePathJSBoundsOversizedFile proves the fix for #4766: a JavaScript
// file over the 1 MiB parse-byte cap must not be handed whole to tree-sitter.
// Superlinear tree-sitter cost on pathological files (a 2.7MB webpack bundle
// measured 15.9s, ~224x a normal parse) makes an uncapped parse a full-corpus
// hazard. The bounded file must return quickly, must still return a payload,
// and must record the bound in payload["js_parse_bounded"].
func TestParsePathJSBoundsOversizedFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "big_bundle.js")
	writeTestFile(t, sourcePath, oversizedJSFunctionArraySource(t))

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	bounded, _ := got["js_parse_bounded"].([]map[string]any)
	if len(bounded) == 0 {
		t.Fatalf("js_parse_bounded = empty, want at least one bounded-file entry in %#v", got)
	}
	entry := bounded[0]
	if entry["path"] != sourcePath {
		t.Fatalf("js_parse_bounded entry path = %v, want %q", entry["path"], sourcePath)
	}
	bytesVal, ok := entry["original_bytes"].(int)
	if !ok || bytesVal <= jsParseByteCap {
		t.Fatalf("js_parse_bounded entry original_bytes = %v, want > %d", entry["original_bytes"], jsParseByteCap)
	}

	// The parse must have been skipped, not merely slow: no function rows are
	// extracted from the oversized source when the cap fires.
	functions, _ := got["functions"].([]map[string]any)
	if len(functions) != 0 {
		t.Fatalf("functions = %d entries, want 0 when the byte cap skips the tree-sitter parse", len(functions))
	}
}

// TestParsePathTSBoundsOversizedFile proves the byte cap also covers
// TypeScript, which shares the javascript-family Parse entry point.
func TestParsePathTSBoundsOversizedFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "big_bundle.ts")
	writeTestFile(t, sourcePath, oversizedJSFunctionArraySource(t))

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	bounded, _ := got["js_parse_bounded"].([]map[string]any)
	if len(bounded) == 0 {
		t.Fatalf("js_parse_bounded = empty, want at least one bounded-file entry for a capped .ts file")
	}
}

// TestParsePathTSXBoundsOversizedFile proves the byte cap also covers TSX,
// which shares the javascript-family Parse entry point.
func TestParsePathTSXBoundsOversizedFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "big_bundle.tsx")
	writeTestFile(t, sourcePath, oversizedJSFunctionArraySource(t))

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	bounded, _ := got["js_parse_bounded"].([]map[string]any)
	if len(bounded) == 0 {
		t.Fatalf("js_parse_bounded = empty, want at least one bounded-file entry for a capped .tsx file")
	}
}

// TestParsePathJSSmallFileUnaffected proves 0/0 identity for the common case:
// a normal, under-cap JavaScript file must parse exactly as before the byte
// cap was introduced -- functions extracted, no js_parse_bounded entry.
func TestParsePathJSSmallFileUnaffected(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "small.js")
	writeTestFile(t, sourcePath, `
function greet(name) {
  return "hello " + name;
}

function farewell(name) {
  return "bye " + name;
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "functions", "greet")
	assertNamedBucketContains(t, got, "functions", "farewell")

	bounded, _ := got["js_parse_bounded"].([]map[string]any)
	if len(bounded) != 0 {
		t.Fatalf("js_parse_bounded = %#v, want empty for an under-cap file", bounded)
	}
}

// TestParsePathPHPBoundsOversizedFile proves the fix for #4766 on PHP: a file
// over the 1 MiB cap must not be handed whole to tree-sitter, and the bound
// must be recorded in payload["php_parse_bounded"].
func TestParsePathPHPBoundsOversizedFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "big_map.php")
	writeTestFile(t, sourcePath, oversizedPHPFunctionArraySource(t))

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	bounded, _ := got["php_parse_bounded"].([]map[string]any)
	if len(bounded) == 0 {
		t.Fatalf("php_parse_bounded = empty, want at least one bounded-file entry in %#v", got)
	}
	entry := bounded[0]
	if entry["path"] != sourcePath {
		t.Fatalf("php_parse_bounded entry path = %v, want %q", entry["path"], sourcePath)
	}
	bytesVal, ok := entry["original_bytes"].(int)
	if !ok || bytesVal <= phpParseByteCap {
		t.Fatalf("php_parse_bounded entry original_bytes = %v, want > %d", entry["original_bytes"], phpParseByteCap)
	}

	functions, _ := got["functions"].([]map[string]any)
	if len(functions) != 0 {
		t.Fatalf("functions = %d entries, want 0 when the byte cap skips the tree-sitter parse", len(functions))
	}
}

// TestParsePathPHPSmallFileUnaffected proves 0/0 identity for the common
// case: a normal, under-cap PHP file parses exactly as before the byte cap
// was introduced.
func TestParsePathPHPSmallFileUnaffected(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "small.php")
	writeTestFile(t, sourcePath, `<?php

function greet($name) {
    return "hello " . $name;
}

function farewell($name) {
    return "bye " . $name;
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "functions", "greet")
	assertNamedBucketContains(t, got, "functions", "farewell")

	bounded, _ := got["php_parse_bounded"].([]map[string]any)
	if len(bounded) != 0 {
		t.Fatalf("php_parse_bounded = %#v, want empty for an under-cap file", bounded)
	}
}

// oversizedJSFunctionArraySource builds a synthetic >1MiB JavaScript source
// made of many small generated functions, reproducing the "large generated
// bundle" shape (webpack/minified output) that triggers superlinear
// tree-sitter parse cost without embedding any real third-party or employer
// source.
func oversizedJSFunctionArraySource(t *testing.T) string {
	t.Helper()

	var b strings.Builder
	i := 0
	for b.Len() < jsParseByteCap+4096 {
		b.WriteString("function generatedFn")
		b.WriteString(itoaTestHelper(i))
		b.WriteString("(a, b, c) { return a + b + c + ")
		b.WriteString(itoaTestHelper(i))
		b.WriteString("; }\n")
		i++
	}
	return b.String()
}

// oversizedPHPFunctionArraySource builds a synthetic >1MiB PHP source made of
// many small generated functions, mirroring the CMS/CID-font-map generated
// shape (e.g. TCPDF font maps) that triggers superlinear tree-sitter parse
// cost.
func oversizedPHPFunctionArraySource(t *testing.T) string {
	t.Helper()

	var b strings.Builder
	b.WriteString("<?php\n")
	i := 0
	for b.Len() < phpParseByteCap+4096 {
		b.WriteString("function generatedFn")
		b.WriteString(itoaTestHelper(i))
		b.WriteString("($a, $b, $c) { return $a + $b + $c + ")
		b.WriteString(itoaTestHelper(i))
		b.WriteString("; }\n")
		i++
	}
	return b.String()
}

func itoaTestHelper(i int) string {
	if i == 0 {
		return "0"
	}
	digits := make([]byte, 0, 12)
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
