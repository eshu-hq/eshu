// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// phpFixturePath resolves a path under tests/fixtures relative to this
// package directory. The external PHP engine tests live one level below
// internal/parser, so they cannot reuse the parent package's fixture helper.
func phpFixturePath(parts ...string) string {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller(0) failed")
	}

	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", ".."))
	elements := append([]string{root, "tests", "fixtures"}, parts...)
	return filepath.Join(elements...)
}

// writeBenchFile writes contents to path for a benchmark. parsertest.WriteFile
// takes a *testing.T, which a *testing.B cannot satisfy, so the benchmark keeps
// its own writer.
func writeBenchFile(b *testing.B, path, contents string) {
	b.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		b.Fatalf("write %s: %v", path, err)
	}
}

// phpAssertStringFieldContains requires item[field] to be a string containing
// want. A missing or non-string field fails rather than comparing against the
// zero value, so a malformed payload cannot pass on an empty want.
func phpAssertStringFieldContains(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, ok := item[field].(string)
	if !ok {
		t.Fatalf("%s = %#v (%T), want string", field, item[field], item[field])
	}
	if !strings.Contains(got, want) {
		t.Fatalf("%s = %#v, want to contain %#v", field, got, want)
	}
}

// phpAssertBoolFieldValue requires item[field] to hold the bool want.
func phpAssertBoolFieldValue(t *testing.T, item map[string]any, field string, want bool) {
	t.Helper()

	got, ok := item[field].(bool)
	if !ok {
		t.Fatalf("%s = %T, want bool", field, item[field])
	}
	if got != want {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

// phpAssertAnySliceFieldValue requires item[field] to be a []any that deep-equals
// want. Call args and context tuples are emitted as []any, which parsertest's
// []string assertions do not cover.
func phpAssertAnySliceFieldValue(t *testing.T, item map[string]any, field string, want []any) {
	t.Helper()

	got, ok := item[field].([]any)
	if !ok {
		t.Fatalf("%s = %T, want []any", field, item[field])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

// phpAssertNilField requires item[field] to be absent or nil. A present
// non-nil value of any type fails, so a drifted payload cannot satisfy the
// negative assertion.
func phpAssertNilField(t *testing.T, item map[string]any, field string) {
	t.Helper()

	if value, ok := item[field]; ok && value != nil {
		t.Fatalf("%s = %#v, want nil", field, value)
	}
}

// assertCallContextTuple requires item["context"] to be a []any whose first
// three entries are the enclosing declaration name, its kind, and its line.
// Each entry is type-checked before comparison so a wrongly-typed entry fails
// instead of comparing a zero value.
func assertCallContextTuple(
	t *testing.T,
	item map[string]any,
	wantName string,
	wantType string,
	wantLine int,
) {
	t.Helper()

	context, ok := item["context"].([]any)
	if !ok {
		t.Fatalf("context = %T, want []any", item["context"])
	}
	if len(context) < 3 {
		t.Fatalf("context = %#v, want at least 3 items", context)
	}
	if got, ok := context[0].(string); !ok || got != wantName {
		t.Fatalf("context[0] = %#v, want %#v", context[0], wantName)
	}
	if got, ok := context[1].(string); !ok || got != wantType {
		t.Fatalf("context[1] = %#v, want %#v", context[1], wantType)
	}
	if got, ok := context[2].(int); !ok || got != wantLine {
		t.Fatalf("context[2] = %#v, want %#v", context[2], wantLine)
	}
}
