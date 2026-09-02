// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// sqlFixturePath resolves a path under tests/fixtures relative to this
// package directory. The parent's equivalent, repoFixturePath, is unexported
// and declared in internal/parser's own testhelpers_test.go, so it is not
// reachable from package sql_test: test files are not importable across
// packages, and the identifier is unexported besides. Directory depth has
// nothing to do with it -- this helper would still be needed if the package
// sat alongside the parent.
func sqlFixturePath(t *testing.T, pathParts ...string) string {
	t.Helper()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || sourceFile == "" {
		t.Fatal("runtime.Caller(0) could not locate SQL test source file")
		return ""
	}

	fixtureParts := []string{filepath.Dir(sourceFile), "..", "..", "..", "..", "tests", "fixtures"}
	return filepath.Join(append(fixtureParts, pathParts...)...)
}

// writeSQLTestFile creates path's parent directories and writes body there.
// parsertest.WriteFile does not create directories, and the migration fixtures
// write under prisma/migrations/ and migrations/ inside a fresh t.TempDir(),
// so the MkdirAll stays here and only the write is delegated.
func writeSQLTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	parsertest.WriteFile(t, path, body)
}
