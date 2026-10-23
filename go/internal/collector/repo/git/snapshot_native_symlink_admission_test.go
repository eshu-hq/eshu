// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/discovery"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// discoverSymlinkFixture runs the production native file-set resolution over
// repoRoot and returns the sorted repo-relative slash paths it admitted.
func discoverSymlinkFixture(t *testing.T, repoRoot string) []string {
	t.Helper()
	got, _ := discoverSymlinkFixtureWithStats(t, repoRoot)
	return got
}

// discoverSymlinkFixtureWithStats is discoverSymlinkFixture plus the discovery
// stats, so a test can assert how many links symlinkAdmission itself dropped
// (content skip reason symlink_target) instead of inferring it from absence.
func discoverSymlinkFixtureWithStats(t *testing.T, repoRoot string) ([]string, discovery.DiscoveryStats) {
	t.Helper()
	resolvedRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRoot = repoRoot
	}
	fileSet, stats, err := resolveNativeSnapshotFileSet(
		resolvedRoot, parser.DefaultRegistry(), NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}
	var got []string
	for _, file := range fileSet.Files {
		rel, relErr := filepath.Rel(resolvedRoot, file.Path)
		if relErr != nil {
			t.Fatalf("filepath.Rel(%q) error = %v", file.Path, relErr)
		}
		got = append(got, filepath.ToSlash(rel))
	}
	sort.Strings(got)
	t.Logf("discovered=%v stats=%+v", got, stats)
	return got, stats
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(link), err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

// absoluteUnderRoot returns the absolute, symlink-resolved path of rel under root,
// the form an absolute link target takes on disk.
func absoluteUnderRoot(t *testing.T, root, rel string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolved = root
	}
	return filepath.Join(resolved, filepath.FromSlash(rel))
}

// TestResolveNativeSnapshotFileSetDropsInRootSymlinkToUnadmittedTarget proves
// the #8809 review finding: an in-root symlink whose own name is admitted must
// not smuggle in a target the registry would never admit, because content is
// read through the link (os.ReadFile follows symlinks) and the content store
// has no write-time redaction.
func TestResolveNativeSnapshotFileSetDropsInRootSymlinkToUnadmittedTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		setup  func(t *testing.T, root string)
		link   string
		admits bool
		// dropped is how many links symlinkAdmission itself must count under
		// the content skip reason symlink_target. Asserting it keeps a row
		// from passing on the walk's own external-link drop, which counts
		// nothing.
		dropped int
	}{
		{
			name: "link to env file",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".env"), "SECRET=not-a-real-secret\n")
				mustSymlink(t, ".env", filepath.Join(root, ".nvmrc"))
			},
			link:    ".nvmrc",
			dropped: 1,
		},
		{
			name: "link into git config",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".git", "config"), "[remote]\nurl = https://u:not-a-real-token@example.test/r.git\n")
				mustSymlink(t, ".git/config", filepath.Join(root, ".nvmrc"))
			},
			link:    ".nvmrc",
			dropped: 1,
		},
		{
			name: "link to npmrc",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".npmrc"), "//r.example.test/:_authToken=not-a-real-token\n")
				mustSymlink(t, ".npmrc", filepath.Join(root, ".node-version"))
			},
			link:    ".node-version",
			dropped: 1,
		},
		{
			name: "link to differently parsed admitted target",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".npmrc.json"), "{\"_authToken\":\"not-a-real-token\"}\n")
				mustSymlink(t, ".npmrc.json", filepath.Join(root, ".nvmrc"))
			},
			link:    ".nvmrc",
			dropped: 1,
		},
		{
			name: "link to terraform state",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "real", "terraform.tfstate"), "{\"version\":4}\n")
				mustSymlink(t, "real/terraform.tfstate", filepath.Join(root, ".nvmrc"))
			},
			link:    ".nvmrc",
			dropped: 1,
		},
		{
			name: "link to gitmodules",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".gitmodules"), "[submodule \"x\"]\n\turl = https://example.test/x.git\n")
				mustSymlink(t, ".gitmodules", filepath.Join(root, ".nvmrc"))
			},
			link:    ".nvmrc",
			dropped: 1,
		},
		{
			name: "link into an ignored directory",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "node_modules", "pkg", ".nvmrc"), "20.11.1\n")
				mustSymlink(t, "node_modules/pkg/.nvmrc", filepath.Join(root, ".nvmrc"))
			},
			link:    ".nvmrc",
			dropped: 1,
		},
		{
			name: "link to directory",
			setup: func(t *testing.T, root string) {
				if err := os.MkdirAll(filepath.Join(root, "real"), 0o755); err != nil {
					t.Fatal(err)
				}
				mustSymlink(t, "real", filepath.Join(root, ".nvmrc"))
			},
			link:    ".nvmrc",
			dropped: 1,
		},
		{
			name: "dangling link",
			setup: func(t *testing.T, root string) {
				mustSymlink(t, "missing/.nvmrc", filepath.Join(root, ".nvmrc"))
			},
			link:    ".nvmrc",
			dropped: 1,
		},
		{
			name: "link cycle",
			setup: func(t *testing.T, root string) {
				mustSymlink(t, ".node-version", filepath.Join(root, ".nvmrc"))
				mustSymlink(t, ".nvmrc", filepath.Join(root, ".node-version"))
			},
			link:    ".nvmrc",
			dropped: 2,
		},
		{
			name: "link to equally admitted file in a subdirectory",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "real", ".nvmrc"), "20.11.1\n")
				mustSymlink(t, "real/.nvmrc", filepath.Join(root, ".nvmrc"))
			},
			link:   ".nvmrc",
			admits: true,
		},
		{
			name: "chain to env file",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".env"), "SECRET=not-a-real-secret\n")
				mustSymlink(t, ".env", filepath.Join(root, ".node-version"))
				mustSymlink(t, ".node-version", filepath.Join(root, ".nvmrc"))
			},
			link:    ".nvmrc",
			dropped: 2,
		},
		{
			name: "chain to a pin",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "real", ".nvmrc"), "20.11.1\n")
				mustSymlink(t, "real/.nvmrc", filepath.Join(root, ".node-version"))
				mustSymlink(t, ".node-version", filepath.Join(root, ".nvmrc"))
			},
			link:   ".nvmrc",
			admits: true,
		},
		{
			name: "absolute link to env file",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".env"), "SECRET=not-a-real-secret\n")
				mustSymlink(t, absoluteUnderRoot(t, root, ".env"), filepath.Join(root, ".nvmrc"))
			},
			link:    ".nvmrc",
			dropped: 1,
		},
		{
			name: "absolute in-root link to a pin",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "real", ".nvmrc"), "20.11.1\n")
				mustSymlink(t, absoluteUnderRoot(t, root, "real/.nvmrc"), filepath.Join(root, ".nvmrc"))
			},
			link:   ".nvmrc",
			admits: true,
		},
		{
			name: "absolute link outside the root",
			setup: func(t *testing.T, root string) {
				outside := t.TempDir()
				writeCollectorTestFile(t, filepath.Join(outside, ".nvmrc"), "20.11.1\n")
				mustSymlink(t, filepath.Join(outside, ".nvmrc"), filepath.Join(root, ".nvmrc"))
			},
			link:    ".nvmrc",
			dropped: 1,
		},
		{
			name: "nested link to a parent pin",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".nvmrc"), "20.11.1\n")
				mustSymlink(t, "../.nvmrc", filepath.Join(root, "sub", ".nvmrc"))
			},
			link:   "sub/.nvmrc",
			admits: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeCollectorTestFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
			tt.setup(t, root)
			got, stats := discoverSymlinkFixtureWithStats(t, root)
			if containsPath(got, tt.link) != tt.admits {
				t.Fatalf("link %q admitted = %v, want %v (discovered %v)", tt.link, !tt.admits, tt.admits, got)
			}
			if dropped := stats.FilesSkippedByContent[symlinkTargetSkipReason]; dropped != tt.dropped {
				t.Fatalf("FilesSkippedByContent[%s] = %d, want %d", symlinkTargetSkipReason, dropped, tt.dropped)
			}
		})
	}
}

// TestSnapshotRepositoryStoresNoContentThroughCredentialSymlink drives the real
// snapshotter and fact streamer over `.nvmrc -> .env` and
// `.node-version -> .git/config` (#8809 review). Neither link may produce a
// content fact, and the planted text must appear in no fact at all.
func TestSnapshotRepositoryStoresNoContentThroughCredentialSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(root, ".git", "config"), "[remote]\nurl = https://u:planted-git-token@example.test/r.git\n")
	writeCollectorTestFile(t, filepath.Join(root, ".env"), "SECRET=planted-env-secret\n")
	writeCollectorTestFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeCollectorTestFile(t, filepath.Join(root, ".python-version"), "3.12.2\n")
	mustSymlink(t, ".env", filepath.Join(root, ".nvmrc"))
	mustSymlink(t, ".git/config", filepath.Join(root, ".node-version"))

	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolved = root
	}
	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	snapshot, err := NativeRepositorySnapshotter{Now: func() time.Time { return now }}.
		SnapshotRepository(context.Background(), SelectedRepository{RepoPath: resolved})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}
	collected := buildStreamingGeneration(
		resolved, testCollectorRepositoryMetadata(resolved), "run-8809", now, snapshot, false, "")
	contentPaths := map[string]bool{}
	for _, envelope := range drainFactChannel(collected.Facts) {
		raw := fmt.Sprintf("%v", envelope.Payload)
		if strings.Contains(raw, "planted-env-secret") || strings.Contains(raw, "planted-git-token") {
			t.Fatalf("planted credential text reached a %s fact: %s", envelope.FactKind, envelope.FactKind)
		}
		if envelope.FactKind == "content" {
			path, _ := envelope.Payload["content_path"].(string)
			contentPaths[path] = true
		}
	}
	for _, denied := range []string{".nvmrc", ".node-version"} {
		if contentPaths[denied] {
			t.Fatalf("%s produced a content fact through a credential symlink; all=%v", denied, contentPaths)
		}
	}
	if !contentPaths[".python-version"] {
		t.Fatalf("regular .python-version missing from content facts; all=%v", contentPaths)
	}
}

// TestResolveNativeSnapshotFileSetForTargetsDropsCredentialSymlink proves the
// --file-targets path drops a link to .env through the same symlinkAdmission
// rule full discovery applies, while a regular pin target stays (#8809).
func TestResolveNativeSnapshotFileSetForTargetsDropsCredentialSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolved = root
	}
	writeCollectorTestFile(t, filepath.Join(resolved, ".env"), "SECRET=planted-env-secret\n")
	writeCollectorTestFile(t, filepath.Join(resolved, ".python-version"), "3.12.2\n")
	mustSymlink(t, ".env", filepath.Join(resolved, ".nvmrc"))

	var stats discovery.DiscoveryStats
	fileSet, err := resolveNativeSnapshotFileSetForTargets(
		resolved,
		[]string{filepath.Join(resolved, ".nvmrc"), filepath.Join(resolved, ".python-version")},
		parser.DefaultRegistry(),
		NativeRepositorySnapshotter{}.discoveryOptions(),
		&stats,
	)
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSetForTargets() error = %v", err)
	}
	if len(fileSet.Files) != 1 || filepath.Base(fileSet.Files[0].Path) != ".python-version" {
		t.Fatalf("file targets admitted %+v, want only .python-version", fileSet.Files)
	}
}
