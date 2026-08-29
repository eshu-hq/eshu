// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elixir_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// elixirFixturePath resolves a path under tests/fixtures relative to this
// package directory. The external Elixir engine tests live one level below
// internal/parser, so they cannot reuse the parent package's fixture helper.
func elixirFixturePath(t *testing.T, parts ...string) string {
	t.Helper()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || sourceFile == "" {
		t.Fatal("runtime.Caller(0) could not locate Elixir test source file")
		return ""
	}

	fixtureParts := []string{filepath.Dir(sourceFile), "..", "..", "..", "..", "tests", "fixtures"}
	return filepath.Join(append(fixtureParts, parts...)...)
}

// writeElixirTestFile writes body to path, creating the parent directories
// first. Several Elixir fixtures live under a nested lib/ layout that the
// framework detectors key on, so the directory must exist before the write.
func writeElixirTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	parsertest.WriteFile(t, path, body)
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

// assertFunctionByNameAndClass returns the functions-bucket item matching both
// name and class_context. Elixir reuses one function name across protocol
// implementations, so name alone does not identify a row.
func assertFunctionByNameAndClass(
	t *testing.T,
	payload map[string]any,
	name string,
	classContext string,
) map[string]any {
	t.Helper()

	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	for _, function := range functions {
		functionName, _ := function["name"].(string)
		functionClassContext, _ := function["class_context"].(string)
		if functionName == name && functionClassContext == classContext {
			return function
		}
	}
	t.Fatalf("functions missing name %q with class_context %q in %#v", name, classContext, functions)
	return nil
}
