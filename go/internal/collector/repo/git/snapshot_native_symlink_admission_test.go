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
	return got
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
	}{
		{
			name: "link to env file",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".env"), "SECRET=not-a-real-secret\n")
				mustSymlink(t, ".env", filepath.Join(root, ".nvmrc"))
			},
			link: ".nvmrc",
		},
		{
			name: "link into git config",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".git", "config"), "[remote]\nurl = https://u:not-a-real-token@example.test/r.git\n")
				mustSymlink(t, ".git/config", filepath.Join(root, ".nvmrc"))
			},
			link: ".nvmrc",
		},
		{
			name: "link to npmrc",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".npmrc"), "//r.example.test/:_authToken=not-a-real-token\n")
				mustSymlink(t, ".npmrc", filepath.Join(root, ".node-version"))
			},
			link: ".node-version",
		},
		{
			name: "link to differently parsed admitted target",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".npmrc.json"), "{\"_authToken\":\"not-a-real-token\"}\n")
				mustSymlink(t, ".npmrc.json", filepath.Join(root, ".nvmrc"))
			},
			link: ".nvmrc",
		},
		{
			name: "link to directory",
			setup: func(t *testing.T, root string) {
				if err := os.MkdirAll(filepath.Join(root, "real"), 0o755); err != nil {
					t.Fatal(err)
				}
				mustSymlink(t, "real", filepath.Join(root, ".nvmrc"))
			},
			link: ".nvmrc",
		},
		{
			name: "dangling link",
			setup: func(t *testing.T, root string) {
				mustSymlink(t, "missing/.nvmrc", filepath.Join(root, ".nvmrc"))
			},
			link: ".nvmrc",
		},
		{
			name: "link cycle",
			setup: func(t *testing.T, root string) {
				mustSymlink(t, ".node-version", filepath.Join(root, ".nvmrc"))
				mustSymlink(t, ".nvmrc", filepath.Join(root, ".node-version"))
			},
			link: ".nvmrc",
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
			got := discoverSymlinkFixture(t, root)
			if containsPath(got, tt.link) != tt.admits {
				t.Fatalf("link %q admitted = %v, want %v (discovered %v)", tt.link, !tt.admits, tt.admits, got)
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
// --file-targets path already agrees with full discovery: it resolves a
// symlink before asking the registry, so a link to .env is dropped (#8809).
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
		&stats,
	)
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSetForTargets() error = %v", err)
	}
	if len(fileSet.Files) != 1 || filepath.Base(fileSet.Files[0].Path) != ".python-version" {
		t.Fatalf("file targets admitted %+v, want only .python-version", fileSet.Files)
	}
}
