// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// writeGoFixture creates path's parent directories and then writes body
// through parsertest.WriteFile. parsertest.WriteFile writes only into a
// directory that already exists, which is all a single-file fixture needs;
// the package-prescan and module-identity tests here lay out multi-package
// module trees (internal/addrs, pkg/bolt, services/api/go.mod, ...) under one
// t.TempDir(), so their parent directories must be created first. Tests that
// write one flat file call parsertest.WriteFile directly.
func writeGoFixture(t *testing.T, path string, body string) {
	t.Helper()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", dir, err)
	}
	parsertest.WriteFile(t, path, body)
}
