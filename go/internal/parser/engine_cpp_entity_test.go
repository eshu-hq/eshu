// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathCPPEnumExtraction proves that the C++ parser
// extracts enum_specifier nodes into the enums bucket. Both scoped
// (enum class) and unscoped (plain enum) forms are covered.
func TestDefaultEngineParsePathCPPEnumExtraction(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "enums.cpp")
	writeTestFile(
		t,
		filePath,
		`enum Color {
    RED = 0,
    GREEN = 1,
    BLUE = 2
};

enum class LogLevel : int {
    DEBUG = 0,
    INFO = 1,
    WARN = 2,
    ERROR = 3
};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got["lang"] != "cpp" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "cpp")
	}

	assertNamedBucketContains(t, got, "enums", "Color")
	assertNamedBucketContains(t, got, "enums", "LogLevel")

	// Verify enum items carry expected fields.
	for _, name := range []string{"Color", "LogLevel"} {
		item := assertBucketItemByName(t, got, "enums", name)
		assertStringFieldValue(t, item, "lang", "cpp")
		if _, ok := item["line_number"]; !ok {
			t.Fatalf("enum %q missing line_number field", name)
		}
		if _, ok := item["end_line"]; !ok {
			t.Fatalf("enum %q missing end_line field", name)
		}
	}
}

// TestDefaultEngineParsePathCPPUnionExtraction proves that the C++ parser
// extracts union_specifier nodes into the unions bucket.
func TestDefaultEngineParsePathCPPUnionExtraction(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "unions.cpp")
	writeTestFile(
		t,
		filePath,
		`union NumericValue {
    int intVal;
    float floatVal;
    double doubleVal;
};

union StringOrNumber {
    char str[32];
    int num;
};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got["lang"] != "cpp" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "cpp")
	}

	assertNamedBucketContains(t, got, "unions", "NumericValue")
	assertNamedBucketContains(t, got, "unions", "StringOrNumber")

	// Verify union items carry expected fields.
	for _, name := range []string{"NumericValue", "StringOrNumber"} {
		item := assertBucketItemByName(t, got, "unions", name)
		assertStringFieldValue(t, item, "lang", "cpp")
		if _, ok := item["line_number"]; !ok {
			t.Fatalf("union %q missing line_number field", name)
		}
		if _, ok := item["end_line"]; !ok {
			t.Fatalf("union %q missing end_line field", name)
		}
	}
}

// TestDefaultEngineParsePathCPPUnionPreScan verifies that C++ unions are
// visible to PreScan symbol collection.
func TestDefaultEngineParsePathCPPUnionPreScan(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "unions.cpp")
	writeTestFile(
		t,
		filePath,
		`union NumericValue {
    int intVal;
    float floatVal;
};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.PreScanPaths([]string{filePath})
	if err != nil {
		t.Fatalf("PreScanPaths() error = %v, want nil", err)
	}

	assertPrescanContains(t, got, "NumericValue", filePath)
}

// TestDefaultEngineParsePathCPPEnumPreScan verifies that C++ enums are
// visible to PreScan symbol collection.
func TestDefaultEngineParsePathCPPEnumPreScan(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "enums.cpp")
	writeTestFile(
		t,
		filePath,
		`enum class LogLevel : int {
    DEBUG = 0,
    INFO = 1,
};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.PreScanPaths([]string{filePath})
	if err != nil {
		t.Fatalf("PreScanPaths() error = %v, want nil", err)
	}

	assertPrescanContains(t, got, "LogLevel", filePath)
}
