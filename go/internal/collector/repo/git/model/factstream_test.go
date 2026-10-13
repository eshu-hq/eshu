// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package model

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestRepositoryRelativePathNonexistentFileUnderSymlinkedRoot proves both
// sides of the relative path share one spelling when the file does not
// exist on disk (#8077). The repository root exists, so it resolves through
// symlinks; a deleted file under a symlinked checkout directory must resolve
// its longest existing parent the same way instead of keeping the
// symlink spelling, which made Rel return ../ segments (macOS
// /var -> /private/var shape from #7998, reproduced here on every OS with
// an explicit symlink).
func TestRepositoryRelativePathNonexistentFileUnderSymlinkedRoot(t *testing.T) {
	t.Parallel()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp root: %v", err)
	}
	realDir := filepath.Join(base, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatalf("mkdir real dir: %v", err)
	}
	linkedDir := filepath.Join(base, "link")
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatalf("symlink checkout: %v", err)
	}

	mustWriteFile(t, realDir, "present.py")

	tests := []struct {
		name     string
		repoPath string
		filePath string
		want     string
	}{
		{
			name:     "missing file under symlinked root",
			repoPath: linkedDir,
			filePath: filepath.Join(linkedDir, "app.py"),
			want:     "app.py",
		},
		{
			name:     "missing file in missing subdirectory under symlinked root",
			repoPath: linkedDir,
			filePath: filepath.Join(linkedDir, "sub", "missing.go"),
			want:     "sub/missing.go",
		},
		{
			name:     "existing file reached through symlinked root",
			repoPath: linkedDir,
			filePath: filepath.Join(linkedDir, "present.py"),
			want:     "present.py",
		},
		{
			name:     "missing file without symlink",
			repoPath: realDir,
			filePath: filepath.Join(realDir, "app.py"),
			want:     "app.py",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := RepositoryRelativePath(test.repoPath, test.filePath)
			if got != test.want {
				t.Fatalf("RepositoryRelativePath = %q, want %q", got, test.want)
			}
			if slices.Contains(strings.Split(got, "/"), "..") {
				t.Fatalf("RepositoryRelativePath = %q, want no ../ segment", got)
			}
		})
	}
}

func mustWriteFile(t *testing.T, dir string, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
