// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestResolveNativeSnapshotFileSetAdmitsVersionPinsAndDropsCredentialDotfiles
// proves the #8809 discovery contract: a runtime-version pin file enters the
// file set, while .env and mise.toml next to it stay out because the content store has
// no write-time redaction. .npmrc is admitted under the metadata policy (#8978).
func TestResolveNativeSnapshotFileSetAdmitsVersionPinsAndDropsCredentialDotfiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".nvmrc"), "20.11.1\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".tool-versions"), "nodejs 20.11.1\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "services", "api", ".python-version"), "3.12.2\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".npmrc"), "//registry.example.test/:_authToken=not-a-real-token\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".env"), "SECRET=not-a-real-secret\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "mise.toml"), "[env]\nSECRET = \"not-a-real-secret\"\n")

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	fileSet, stats, err := resolveNativeSnapshotFileSet(
		resolvedRepoRoot, parser.DefaultRegistry(), NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}

	var got []string
	for _, file := range fileSet.Files {
		rel, relErr := filepath.Rel(resolvedRepoRoot, file.Path)
		if relErr != nil {
			t.Fatalf("filepath.Rel(%q) error = %v", file.Path, relErr)
		}
		got = append(got, filepath.ToSlash(rel))
	}
	sort.Strings(got)
	// .npmrc is admitted by its own definition (#8978); .env and mise.toml stay out.
	want := []string{".npmrc", ".nvmrc", ".tool-versions", "services/api/.python-version"}
	if len(got) != len(want) {
		t.Fatalf("discovered files = %v, want exactly %v; stats=%+v", got, want, stats)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("discovered files = %v, want exactly %v; stats=%+v", got, want, stats)
		}
	}
}
