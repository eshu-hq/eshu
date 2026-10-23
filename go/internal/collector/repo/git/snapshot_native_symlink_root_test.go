// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/discovery"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// discoverThroughSymlinkedRoot runs the production native file-set resolution
// over repoRoot exactly as given (no EvalSymlinks first) and returns the sorted
// repo-relative slash paths it admitted, measured from the real root.
func discoverThroughSymlinkedRoot(t *testing.T, repoRoot string) []string {
	t.Helper()
	realRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) error = %v", repoRoot, err)
	}
	fileSet, stats, err := resolveNativeSnapshotFileSet(
		repoRoot, parser.DefaultRegistry(), NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}
	var got []string
	for _, file := range fileSet.Files {
		// Resolve the directory only: the file itself may be an admitted link,
		// and the test must see the link's own path, not its target's.
		resolvedDir, evalErr := filepath.EvalSymlinks(filepath.Dir(file.Path))
		if evalErr != nil {
			resolvedDir = filepath.Dir(file.Path)
		}
		resolved := filepath.Join(resolvedDir, filepath.Base(file.Path))
		rel, relErr := filepath.Rel(realRoot, resolved)
		if relErr != nil {
			t.Fatalf("filepath.Rel(%q) error = %v", resolved, relErr)
		}
		got = append(got, filepath.ToSlash(rel))
	}
	sort.Strings(got)
	t.Logf("discovered=%v stats=%+v", got, stats)
	return got
}

// TestResolveNativeSnapshotFileSetSymlinkedRepoRootKeepsSameCategoryLinks
// proves the target category check agrees with the containment check when the
// caller hands in a repo root reached through a symlinked directory (#8809
// review). Targets resolve under the real root, so CODEOWNERS and .gitmodules
// detection must be measured from the real root too. A legitimate same-category
// link stays admitted; a credential link still drops. The --file-targets path
// gets the same root and must reach the same decision.
func TestResolveNativeSnapshotFileSetSymlinkedRepoRootKeepsSameCategoryLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		setup  func(t *testing.T, root string)
		link   string
		admits bool
	}{
		{
			name: "github CODEOWNERS link to root CODEOWNERS",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "CODEOWNERS"), "* @example-team\n")
				mustSymlink(t, "../CODEOWNERS", filepath.Join(root, ".github", "CODEOWNERS"))
			},
			link:   ".github/CODEOWNERS",
			admits: true,
		},
		{
			name: "docs CODEOWNERS link to root CODEOWNERS",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "CODEOWNERS"), "* @example-team\n")
				mustSymlink(t, "../CODEOWNERS", filepath.Join(root, "docs", "CODEOWNERS"))
			},
			link:   "docs/CODEOWNERS",
			admits: true,
		},
		{
			name: "terraform state link to a sibling state file",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "terraform.tfstate"), "{\"version\":4}\n")
				mustSymlink(t, "../terraform.tfstate", filepath.Join(root, "env", "terraform.tfstate"))
			},
			link:   "env/terraform.tfstate",
			admits: true,
		},
		{
			name: "credential link to env file",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".env"), "SECRET=not-a-real-secret\n")
				mustSymlink(t, ".env", filepath.Join(root, ".nvmrc"))
			},
			link: ".nvmrc",
		},
		{
			name: "pin link to gitmodules",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".gitmodules"), "[submodule \"x\"]\n\turl = https://example.test/x.git\n")
				mustSymlink(t, ".gitmodules", filepath.Join(root, ".nvmrc"))
			},
			link: ".nvmrc",
		},
		{
			name: "pin link to CODEOWNERS",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "CODEOWNERS"), "* @example-team\n")
				mustSymlink(t, "CODEOWNERS", filepath.Join(root, ".nvmrc"))
			},
			link: ".nvmrc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			realRoot := filepath.Join(base, "real-repo")
			writeCollectorTestFile(t, filepath.Join(realRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
			tt.setup(t, realRoot)
			aliasRoot := filepath.Join(base, "alias-repo")
			mustSymlink(t, "real-repo", aliasRoot)
			got := discoverThroughSymlinkedRoot(t, aliasRoot)
			if containsPath(got, tt.link) != tt.admits {
				t.Fatalf("link %q admitted = %v, want %v (discovered %v)", tt.link, !tt.admits, tt.admits, got)
			}
			var stats discovery.DiscoveryStats
			deltaSet, err := resolveNativeSnapshotFileSetForTargets(aliasRoot,
				[]string{filepath.Join(aliasRoot, filepath.FromSlash(tt.link))},
				parser.DefaultRegistry(), NativeRepositorySnapshotter{}.discoveryOptions(), &stats)
			if err != nil {
				t.Fatalf("resolveNativeSnapshotFileSetForTargets() error = %v", err)
			}
			if got := len(deltaSet.Files) == 1; got != tt.admits {
				t.Fatalf("delta link %q admitted = %v, want %v (files %+v)", tt.link, got, tt.admits, deltaSet.Files)
			}
		})
	}
}
