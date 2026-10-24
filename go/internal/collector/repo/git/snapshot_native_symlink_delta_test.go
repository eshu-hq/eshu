// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/discovery"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestSymlinkTargetAdmissionAgreesAcrossSyncModes proves full discovery and
// the --file-targets (delta) path make the same symlink decision (#8809). A
// delta sync that indexed a link full discovery dropped would store text from
// .git, vendor, or an operator-excluded path whenever the delta mode ran.
func TestSymlinkTargetAdmissionAgreesAcrossSyncModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		setup  func(t *testing.T, root string)
		opts   func(opts *discovery.Options)
		link   string
		admits bool
	}{
		{
			name: "pin link into an ignored vendor directory is dropped",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "vendor", "x", ".nvmrc"), "20.11.1\n")
				mustSymlink(t, "vendor/x/.nvmrc", filepath.Join(root, ".nvmrc"))
			},
			link: ".nvmrc",
		},
		{
			name: "config link into .git is dropped",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".git", "x.conf"), "token=not-a-real-token\n")
				mustSymlink(t, ".git/x.conf", filepath.Join(root, "app.conf"))
			},
			link: "app.conf",
		},
		{
			name: "pin link to a credential file is dropped",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".env"), "SECRET=not-a-real-secret\n")
				mustSymlink(t, ".env", filepath.Join(root, ".nvmrc"))
			},
			link: ".nvmrc",
		},
		{
			name: "yaml link to a json config is dropped",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "shared", "app.json"), "{}\n")
				mustSymlink(t, "../shared/app.json", filepath.Join(root, "sub", "app.yaml"))
			},
			link: "sub/app.yaml",
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
			name: "pin link under an operator path glob is dropped",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "excluded", ".nvmrc"), "20.11.1\n")
				mustSymlink(t, "excluded/.nvmrc", filepath.Join(root, ".nvmrc"))
			},
			opts: func(opts *discovery.Options) {
				opts.IgnoredPathGlobs = append(opts.IgnoredPathGlobs,
					discovery.PathGlobRule{Pattern: "excluded/**", Reason: "operator"})
			},
			link: ".nvmrc",
		},
		{
			name: "config link to an ignored extension is dropped",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "keys.secret.conf"), "k=1\n")
				mustSymlink(t, "keys.secret.conf", filepath.Join(root, "app.conf"))
			},
			opts: func(opts *discovery.Options) {
				opts.IgnoredExtensions = append(opts.IgnoredExtensions, ".secret.conf")
			},
			link: "app.conf",
		},
		{
			name: "config link into a hidden directory is dropped when hidden paths are skipped",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".private", "app.conf"), "a=1\n")
				mustSymlink(t, ".private/app.conf", filepath.Join(root, "app.conf"))
			},
			opts: func(opts *discovery.Options) { opts.IgnoreHidden = true },
			link: "app.conf",
		},
		{
			name: "pin link to a pin in the repository stays indexed",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "real", ".nvmrc"), "20.11.1\n")
				mustSymlink(t, "real/.nvmrc", filepath.Join(root, ".nvmrc"))
			},
			link:   ".nvmrc",
			admits: true,
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if resolved, err := filepath.EvalSymlinks(root); err == nil {
				root = resolved
			}
			writeCollectorTestFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
			tt.setup(t, root)
			opts := NativeRepositorySnapshotter{}.discoveryOptions()
			if tt.opts != nil {
				tt.opts(&opts)
			}
			registry := parser.DefaultRegistry()

			fullSet, _, err := resolveNativeSnapshotFileSet(root, registry, opts)
			if err != nil {
				t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
			}
			fullAdmits := fileSetContainsSuffix(fullSet.Files, tt.link)

			var stats discovery.DiscoveryStats
			deltaSet, err := resolveNativeSnapshotFileSetForTargets(
				root, []string{filepath.Join(root, filepath.FromSlash(tt.link))}, registry, opts, &stats)
			if err != nil {
				t.Fatalf("resolveNativeSnapshotFileSetForTargets() error = %v", err)
			}
			deltaAdmits := len(deltaSet.Files) == 1

			if fullAdmits != tt.admits || deltaAdmits != tt.admits {
				t.Fatalf("link %q: full admits = %v, delta admits = %v (delta files %+v), want %v",
					tt.link, fullAdmits, deltaAdmits, deltaSet.Files, tt.admits)
			}
			wantDrops := 0
			if !tt.admits {
				wantDrops = 1
			}
			if got := stats.FilesSkippedByContent[symlinkTargetSkipReason]; got != wantDrops {
				t.Fatalf("delta FilesSkippedByContent[%q] = %d, want %d", symlinkTargetSkipReason, got, wantDrops)
			}
		})
	}
}

// TestResolveNativeSnapshotFileSetForTargetsOutOfRoot pins both sides of the
// out-of-root rule (#8809). An in-root link whose target leaves the repository
// is dropped and counted, as full discovery drops it. A target whose own path
// is outside the repository is still a caller error, link or not.
func TestResolveNativeSnapshotFileSetForTargetsOutOfRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		base = resolved
	}
	root := filepath.Join(base, "repo")
	outside := filepath.Join(base, "outside")
	writeCollectorTestFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(root, ".python-version"), "3.12.2\n")
	writeCollectorTestFile(t, filepath.Join(outside, "app.conf"), "token=not-a-real-token\n")
	mustSymlink(t, "../outside/app.conf", filepath.Join(root, "app.conf"))
	mustSymlink(t, "app.conf", filepath.Join(outside, "alias.conf"))
	opts := NativeRepositorySnapshotter{}.discoveryOptions()
	registry := parser.DefaultRegistry()

	var stats discovery.DiscoveryStats
	fileSet, err := resolveNativeSnapshotFileSetForTargets(root,
		[]string{filepath.Join(root, "app.conf"), filepath.Join(root, ".python-version")}, registry, opts, &stats)
	if err != nil {
		t.Fatalf("in-root link leaving the root: error = %v, want drop", err)
	}
	if len(fileSet.Files) != 1 || filepath.Base(fileSet.Files[0].Path) != ".python-version" {
		t.Fatalf("file targets admitted %+v, want only .python-version", fileSet.Files)
	}
	if got := stats.FilesSkippedByContent[symlinkTargetSkipReason]; got != 1 {
		t.Fatalf("FilesSkippedByContent[%q] = %d, want 1", symlinkTargetSkipReason, got)
	}

	for _, target := range []string{filepath.Join(outside, "app.conf"), filepath.Join(outside, "alias.conf")} {
		var outsideStats discovery.DiscoveryStats
		if _, err := resolveNativeSnapshotFileSetForTargets(root, []string{target}, registry, opts, &outsideStats); err == nil ||
			!strings.Contains(err.Error(), "outside repository root") {
			t.Fatalf("target %q outside the root: error = %v, want outside repository root", target, err)
		}
	}
}
