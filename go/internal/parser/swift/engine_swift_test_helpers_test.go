// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package swift_test

import (
	"os"
	"path/filepath"
	"runtime"
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
