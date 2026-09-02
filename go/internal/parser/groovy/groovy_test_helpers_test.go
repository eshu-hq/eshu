// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package groovy_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// groovyFixturePath resolves a path under tests/fixtures relative to this
// package directory. The external Groovy engine tests live one level below
// internal/parser, so they cannot reuse the parent package's fixture helper.
func groovyFixturePath(t *testing.T, parts ...string) string {
	t.Helper()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || sourceFile == "" {
		t.Fatal("runtime.Caller(0) could not locate Groovy test source file")
		return ""
	}

	fixtureParts := []string{filepath.Dir(sourceFile), "..", "..", "..", "..", "tests", "fixtures"}
	return filepath.Join(append(fixtureParts, parts...)...)
}

// writeGroovyTestFile writes body to path, creating the parent directories
// first. The cyclomatic-complexity fixture writes into a nested src/ layout,
// so the directory must exist before parsertest.WriteFile runs.
func writeGroovyTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	parsertest.WriteFile(t, path, body)
}

// assertEmptyNamedBucket requires payload[key] to be an empty map slice. It is
// a local copy of the parent package's helper because parsertest has no
// empty-bucket assertion yet.
func assertEmptyNamedBucket(t *testing.T, payload map[string]any, key string) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	if len(items) != 0 {
		t.Fatalf("%s = %#v, want empty bucket", key, items)
	}
}
