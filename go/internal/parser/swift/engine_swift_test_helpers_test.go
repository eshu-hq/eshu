// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package swift_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// swiftFixturePath resolves a path under tests/fixtures relative to this
// package directory. The external Swift engine tests live one level below
// internal/parser, so they cannot reuse the parent package's fixture helper.
func swiftFixturePath(t *testing.T, parts ...string) string {
	t.Helper()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || sourceFile == "" {
		t.Fatal("runtime.Caller(0) could not locate Swift test source file")
		return ""
	}

	fixtureParts := []string{filepath.Dir(sourceFile), "..", "..", "..", "..", "tests", "fixtures"}
	return filepath.Join(append(fixtureParts, parts...)...)
}

// writeSwiftTestFile writes body to path, creating the parent directories
// first. Several Swift dead-code and Vapor route fixtures write into a nested
// Sources/App layout, so the directory must exist before the write.
func writeSwiftTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	parsertest.WriteFile(t, path, body)
}

// assertFunctionByName returns the functions-bucket item matching name.
func assertFunctionByName(t *testing.T, payload map[string]any, name string) map[string]any {
	t.Helper()

	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	for _, function := range functions {
		functionName, _ := function["name"].(string)
		if functionName == name {
			return function
		}
	}
	t.Fatalf("functions missing name %q in %#v", name, functions)
	return nil
}

// assertFunctionByNameAndClass returns the functions-bucket item matching both
// name and class_context. Swift reuses method names across protocol
// conformances, extensions, and overrides, so name alone does not identify a
// row.
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

// assertParserStringSliceEquals requires item[field] to equal want in order.
func assertParserStringSliceEquals(t *testing.T, item map[string]any, field string, want []string) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		t.Fatalf("%s = %T, want []string", field, item[field])
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

// assertParserStringSliceNotContains requires item[field], when present, to
// not contain want.
func assertParserStringSliceNotContains(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		return
	}
	for _, value := range got {
		if value == want {
			t.Fatalf("%s = %#v, want not to contain %#v", field, got, want)
		}
	}
}
