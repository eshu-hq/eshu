// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// writeTestFile creates the fixture's parent directory before delegating to
// parsertest.WriteFile, because these Engine tests write nested Cargo-shaped
// paths such as src/lib.rs that the shared helper does not create.
func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()

	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", parent, err)
	}
	parsertest.WriteFile(t, path, body)
}

// assertStringFieldValue requires item[field] to equal want exactly. It keeps
// the parent parser package's strict string comparison, which treats a missing
// or non-string field as the empty string rather than converting it.
func assertStringFieldValue(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, _ := item[field].(string)
	if got != want {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

// assertStringSliceFieldValue requires item[field] to be a []string equal to
// want in order. It keeps the parent parser package's strict type assertion so
// a payload that stops emitting a concrete []string fails instead of coercing.
func assertStringSliceFieldValue(
	t *testing.T,
	item map[string]any,
	field string,
	want []string,
) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		t.Fatalf("%s = %T, want []string", field, item[field])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}
