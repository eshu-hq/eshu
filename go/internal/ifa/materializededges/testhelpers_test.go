// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"path/filepath"
	"runtime"
	"testing"
)

// repoRootDir resolves the repository root relative to this test file, so
// repoRoot-anchored fixtures (go/internal/ifa/testdata/...) load regardless of
// the working directory `go test` runs with. Duplicated from ifa's
// coverage_falsegreen_test.go (repoRootDir): this package is one directory
// deeper than go/internal/ifa, so it walks up four levels instead of three,
// and an unexported test helper cannot cross the package boundary anyway.
func repoRootDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
}
