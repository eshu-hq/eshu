// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func ensureParentDirectory(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

func osWriteFile(path string, body []byte) error {
	return os.WriteFile(path, body, 0o644)
}

func repoFixturePath(parts ...string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller(0) failed")
	}

	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	elements := append([]string{root, "tests", "fixtures"}, parts...)
	return filepath.Join(elements...)
}

// assertStringSliceFieldValue requires item[field] to equal want in order. It
// lives here rather than in a language-specific test file because several
// engine test files assert ordered string-slice payload fields.
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
