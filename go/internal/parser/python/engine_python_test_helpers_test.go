// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// writeTestFile writes body to path, creating the parent directories first.
// The relocated Python engine tests write fixtures at arbitrary depths under
// t.TempDir(), so the directory must exist before the write.
func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	parsertest.WriteFile(t, path, body)
}

// pythonFixturePath resolves a path under tests/fixtures relative to this
// package directory. The external Python engine tests live one level below
// internal/parser, so they cannot reuse the parent package's fixture helper.
func pythonFixturePath(t *testing.T, parts ...string) string {
	t.Helper()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || sourceFile == "" {
		t.Fatal("runtime.Caller(0) could not locate Python test source file")
		return ""
	}

	fixtureParts := []string{filepath.Dir(sourceFile), "..", "..", "..", "..", "tests", "fixtures"}
	return filepath.Join(append(fixtureParts, parts...)...)
}

// assertParserStringSliceFieldValue requires item[field] to equal want in
// order, comparing with reflect.DeepEqual rather than parsertest's
// slices.Equal so a nil field still fails against a non-nil want. Several
// dead-code-semantics assertions on classes.bases distinguish "field absent"
// from "field present but empty", which slices.Equal collapses.
func assertParserStringSliceFieldValue(t *testing.T, item map[string]any, field string, want []string) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		t.Fatalf("%s = %T, want []string", field, item[field])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

// assertFunctionByName requires payload["functions"] to contain an item whose
// name field equals name. The relocated dead-code-roots tests look functions
// up by name alone, and the parent package's helper of the same name is an
// unexported test declaration, so it is not importable from here.
func assertFunctionByName(t *testing.T, payload map[string]any, name string) map[string]any {
	t.Helper()

	return parsertest.AssertBucketItemByName(t, payload, "functions", name)
}

// assertBucketItemByFieldValue returns the payload[bucket] item whose field
// equals want. Python call-semantics and dead-code-semantics assertions key
// function_calls entries by full_name or call_kind rather than by name, so
// parsertest's name-keyed lookups do not apply.
func assertBucketItemByFieldValue(
	t *testing.T,
	payload map[string]any,
	bucket string,
	field string,
	want string,
) map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		value, _ := item[field].(string)
		if value == want {
			return item
		}
	}
	t.Fatalf("%s missing %s=%q in %#v", bucket, field, want, items)
	return nil
}
