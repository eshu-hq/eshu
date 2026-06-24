// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"path/filepath"
	"runtime"
	"testing"
)

// repoSpecsDir resolves the repository specs/ directory from this test file's
// location so tests can read the real capability matrix and overlay.
func repoSpecsDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", "specs"))
}
