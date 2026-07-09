// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestResolveNativeSnapshotFileSetKeepsNestedNPMWorkspaceManifests(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name":"root","workspaces":["packages/*"]}`)
	writeCollectorTestFile(t, filepath.Join(repoRoot, "package-lock.json"), `{"lockfileVersion":3,"packages":{}}`)
	writeCollectorTestFile(t, filepath.Join(repoRoot, "packages", "client", "package.json"), `{"name":"client"}`)
	writeCollectorTestFile(t, filepath.Join(repoRoot, "packages", "client", "package-lock.json"), `{"lockfileVersion":3,"packages":{}}`)

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	registry := parser.DefaultRegistry()
	fileSet, stats, err := resolveNativeSnapshotFileSet(resolvedRepoRoot, registry, NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}

	want := map[string]bool{
		"package.json":                      false,
		"package-lock.json":                 false,
		"packages/client/package.json":      false,
		"packages/client/package-lock.json": false,
	}
	for _, path := range fileSet.Files {
		rel, err := filepath.Rel(resolvedRepoRoot, path.Path)
		if err != nil {
			t.Fatalf("filepath.Rel(%q) error = %v", path.Path, err)
		}
		rel = filepath.ToSlash(rel)
		if _, ok := want[rel]; ok {
			want[rel] = true
		}
	}
	for rel, seen := range want {
		if !seen {
			t.Fatalf("discovered files missing %q; files=%v stats=%+v", rel, fileSet.Files, stats)
		}
	}
	if got := stats.DirsSkippedByName["packages"]; got != 0 {
		t.Fatalf("DirsSkippedByName[packages] = %d, want 0 for authored npm workspace directories", got)
	}
}
