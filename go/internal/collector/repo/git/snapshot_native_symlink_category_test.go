// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveNativeSnapshotFileSetSymlinkCategoryAndKind pins the second half
// of the #8809 symlink rule: a link must resolve to a regular file in the SAME
// admission category as its own name. Each dropped case would be admitted if
// the category collapsed to "documentation" or if the regular-file check were
// removed; each kept case is a legitimate link that must stay indexed.
func TestResolveNativeSnapshotFileSetSymlinkCategoryAndKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		setup  func(t *testing.T, root string)
		link   string
		admits bool
	}{
		{
			name: "documentation link to gitmodules is dropped",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".gitmodules"), "[submodule \"x\"]\n\turl = https://example.test/x.git\n")
				mustSymlink(t, ".gitmodules", filepath.Join(root, "README.md"))
			},
			link: "README.md",
		},
		{
			name: "documentation link to terraform state is dropped",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "terraform.tfstate"), "{\"version\":4}\n")
				mustSymlink(t, "../terraform.tfstate", filepath.Join(root, "docs", "x.md"))
			},
			link: "docs/x.md",
		},
		{
			name: "codeowners link to a non-candidate path is dropped",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "shared", "CODEOWNERS"), "* @example\n")
				mustSymlink(t, "../shared/CODEOWNERS", filepath.Join(root, "docs", "CODEOWNERS"))
			},
			link: "docs/CODEOWNERS",
		},
		{
			name: "config link to a config file under another path is dropped",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "shared", "app.json"), "{}\n")
				mustSymlink(t, "../shared/app.json", filepath.Join(root, "sub", "app.yaml"))
			},
			link: "sub/app.yaml",
		},
		{
			name: "pin link to a directory whose name matches the same parser is dropped",
			setup: func(t *testing.T, root string) {
				if err := os.MkdirAll(filepath.Join(root, "dir.conf"), 0o755); err != nil {
					t.Fatal(err)
				}
				mustSymlink(t, "dir.conf", filepath.Join(root, ".nvmrc"))
			},
			link: ".nvmrc",
		},
		{
			name: "documentation link to documentation stays indexed",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "docs", "guide.md"), "# Guide\n")
				mustSymlink(t, "guide.md", filepath.Join(root, "docs", "alias.md"))
			},
			link:   "docs/alias.md",
			admits: true,
		},
		{
			name: "github codeowners link to root codeowners stays indexed",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "CODEOWNERS"), "* @example\n")
				mustSymlink(t, "../CODEOWNERS", filepath.Join(root, ".github", "CODEOWNERS"))
			},
			link:   ".github/CODEOWNERS",
			admits: true,
		},
		{
			name: "upper-case pin name linking to the lower-case pin follows the registry",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".nvmrc"), "20.11.1\n")
				mustSymlink(t, ".nvmrc", filepath.Join(root, ".NVMRC"))
			},
			link:   ".NVMRC",
			admits: true,
		},
		{
			name: "upper-case pin name linking to an env file is dropped",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".ENV"), "SECRET=not-a-real-secret\n")
				mustSymlink(t, ".ENV", filepath.Join(root, ".NVMRC"))
			},
			link: ".NVMRC",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeCollectorTestFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
			tt.setup(t, root)
			got := discoverSymlinkFixture(t, root)
			if containsPath(got, tt.link) != tt.admits {
				t.Fatalf("link %q admitted = %v, want %v (discovered %v)", tt.link, !tt.admits, tt.admits, got)
			}
		})
	}
}
