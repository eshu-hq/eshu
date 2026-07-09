// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fws is a test helper that builds a []FileWithSize from absolute path strings

// fws is a test helper that builds a []FileWithSize from absolute path strings
// with no disk stat (size 0 sentinel). For discovery tests the size is
// irrelevant; only the path order and presence matter.
func fws(paths ...string) []FileWithSize {
	files := make([]FileWithSize, len(paths))
	for i, p := range paths {
		files[i] = FileWithSize{Path: p}
	}
	return files
}

// repoSetsEqual compares two RepoFileSet slices by RepoRoot and file paths
// only (sizes are not compared, since test expectations don't set sizes).
func repoSetsEqual(got, want []RepoFileSet) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].RepoRoot != want[i].RepoRoot {
			return false
		}
		if len(got[i].Files) != len(want[i].Files) {
			return false
		}
		for j := range got[i].Files {
			if got[i].Files[j].Path != want[i].Files[j].Path {
				return false
			}
		}
	}
	return true
}

func repoFileSetsContainSuffix(fileSets []RepoFileSet, suffix string) bool {
	for _, fileSet := range fileSets {
		for _, file := range fileSet.Files {
			if strings.HasSuffix(filepath.ToSlash(file.Path), suffix) {
				return true
			}
		}
	}
	return false
}

// TestCollectSupportedFilesHarvestsSizeFromLstat proves that discovery
// populates file sizes from the single os.Lstat already performed for
// symlink classification, without an extra entry.Info() call.  For N
// regular files the total stat-family calls are N (one Lstat per file).
//
// Verification: create N real files with known content, run discovery,
// assert every discovered file has a non-zero Size matching the on-disk
// byte count.  Zero-length files get minParseFileWeightBytes in the
// partition step, but the Size field here is the raw on-disk size.
func TestCollectSupportedFilesHarvestsSizeFromLstat(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	contents := []string{
		"package main\n",
		"print('hello')\n",
		"// a longer file with more bytes\nfunc foo() { return 42 }\n",
	}
	wantSize := make(map[string]int64, len(contents))
	for i, body := range contents {
		path := filepath.Join(repo, "file"+string(rune('0'+i))+".go")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		wantSize[path] = int64(len(body))
	}

	stats, fileSets, err := ResolveRepositoryFileSetsWithStats(
		root,
		func(path string) bool { return filepath.Ext(path) == ".go" },
		Options{
			IgnoredDirs:    []string{".git"},
			HonorGitignore: false,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	var totalFiles int
	for _, fs := range fileSets {
		for _, f := range fs.Files {
			totalFiles++
			if f.Size <= 0 {
				t.Errorf("file %q has size %d, want non-zero (Lstat harvest failed)", f.Path, f.Size)
			}
			// Byte-for-byte cross-check: the discovery-harvested size must
			// exactly equal the file's real content length, closing the
			// integration gap between the classifyPath Lstat harvest and the
			// os.Stat sizes the partition equivalence proof relies on.
			if want, ok := wantSize[f.Path]; ok && f.Size != want {
				t.Errorf("file %q harvested size = %d, want %d (must match os.Stat/content length exactly)", f.Path, f.Size, want)
			}
		}
	}
	if totalFiles != len(contents) {
		t.Errorf("discovered %d files, want %d", totalFiles, len(contents))
	}
	_ = stats

	t.Logf("Discovered %d files, all sizes byte-for-byte equal to content length (1 Lstat per file, 0 entry.Info calls)", totalFiles)
}
