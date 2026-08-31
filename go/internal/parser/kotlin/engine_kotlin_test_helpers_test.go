// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// kotlinFixturePath resolves a path under tests/fixtures relative to this
// package directory. The external Kotlin engine tests live one level below
// internal/parser, so they cannot reuse the parent package's fixture helper.
func kotlinFixturePath(parts ...string) string {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller(0) failed")
	}

	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", ".."))
	elements := append([]string{root, "tests", "fixtures"}, parts...)
	return filepath.Join(elements...)
}

// writeKotlinTestFile writes body to path, creating the parent directories
// first. Several Kotlin fixtures live under a nested src/main/kotlin layout
// that the package-aware sibling return-type lookup keys on, so the directory
// must exist before the write.
func writeKotlinTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", path, err)
	}
}

// assertStringFieldValue requires item[field] to hold the string want.
func assertStringFieldValue(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, _ := item[field].(string)
	if got != want {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

// assertIntFieldValue requires item[field] to hold the int want.
func assertIntFieldValue(t *testing.T, item map[string]any, field string, want int) {
	t.Helper()

	got, ok := item[field].(int)
	if !ok {
		t.Fatalf("%s = %T, want int", field, item[field])
	}
	if got != want {
		t.Fatalf("%s = %d, want %d", field, got, want)
	}
}

// assertBoolFieldValue requires item[field] to hold the bool want.
func assertBoolFieldValue(t *testing.T, item map[string]any, field string, want bool) {
	t.Helper()

	got, ok := item[field].(bool)
	if !ok {
		t.Fatalf("%s = %T, want bool", field, item[field])
	}
	if got != want {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}
