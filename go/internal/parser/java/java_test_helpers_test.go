// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// javaFixturePath resolves a path under tests/fixtures relative to this
// package directory. The external Java engine tests live one level below
// internal/parser, so they cannot reuse the parent package's fixture helper.
func javaFixturePath(t *testing.T, pathParts ...string) string {
	t.Helper()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || sourceFile == "" {
		t.Fatal("runtime.Caller(0) could not locate Java test source file")
		return ""
	}

	fixtureParts := []string{filepath.Dir(sourceFile), "..", "..", "..", "..", "tests", "fixtures"}
	return filepath.Join(append(fixtureParts, pathParts...)...)
}

// writeJavaTestFile creates path's parent directories and writes body there.
// parsertest.WriteFile does not create directories, and the Java engine tests
// write fixtures under src/main/java/... inside a fresh t.TempDir(), so the
// MkdirAll stays here and only the write is delegated.
func writeJavaTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	parsertest.WriteFile(t, path, body)
}
