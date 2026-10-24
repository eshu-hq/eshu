// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestResolveNativeSnapshotFileSetIndexedFileRule pins the indexed-file rule
// that the list_repository_files, get_file_content, get_file_lines, and
// search_file_content tool descriptions, the MCP guide, and the assistant
// guidance state (#8808): a file is indexed only if it matched a
// parser-registry name, prefix, extension, or documentation format, or is a
// .gitmodules or CODEOWNERS file; dotfiles are not hidden. A matching name
// can still be absent when it is a symlink whose target fails admission. If
// discovery changes what it admits, this test goes red and that text must
// change with it.
func TestResolveNativeSnapshotFileSetIndexedFileRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		indexed bool
		// linkTo, when set, makes path a symlink to this repo-relative target
		// instead of a regular file.
		linkTo string
	}{
		{name: "dotfile with registry extension", path: ".eslintrc.js", indexed: true},
		{name: "dot directory workflow", path: ".github/workflows/ci.yml", indexed: true},
		{name: "gitmodules", path: ".gitmodules", indexed: true},
		{name: "github CODEOWNERS", path: ".github/CODEOWNERS", indexed: true},
		{name: "extensionless dotfile with no registry entry", path: ".env", indexed: false},
		{name: "pin name that is a symlink to a target that fails admission", path: ".nvmrc", indexed: false, linkTo: ".env"},
	}

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	for _, tt := range tests {
		if tt.linkTo != "" {
			continue
		}
		writeCollectorTestFile(t, filepath.Join(repoRoot, filepath.FromSlash(tt.path)), "x = 1\n")
	}
	for _, tt := range tests {
		if tt.linkTo != "" {
			mustSymlink(t, tt.linkTo, filepath.Join(repoRoot, filepath.FromSlash(tt.path)))
		}
	}
	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}

	fileSet, _, err := resolveNativeSnapshotFileSet(
		resolvedRepoRoot, parser.DefaultRegistry(), NativeRepositorySnapshotter{}.discoveryOptions(),
	)
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}
	admitted := make(map[string]bool, len(fileSet.Files))
	for _, file := range fileSet.Files {
		rel, relErr := filepath.Rel(resolvedRepoRoot, file.Path)
		if relErr != nil {
			t.Fatalf("Rel(%q) error = %v", file.Path, relErr)
		}
		admitted[filepath.ToSlash(rel)] = true
	}

	for _, tt := range tests {
		if got := admitted[tt.path]; got != tt.indexed {
			t.Errorf("%s (%s): admitted = %t, want %t; admitted set = %v", tt.name, tt.path, got, tt.indexed, admitted)
		}
	}
}
