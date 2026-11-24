// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSelectionBackpressureSupportsOnlyPerRepositorySyncModes pins the
// allow-list the schedule leg applies to (#10882): a source mode whose sync
// keeps durable per-repository state can defer a repository without losing it.
// Filesystem mode compares one whole-root fingerprint, so deferring a
// repository there is a drop.
func TestSelectionBackpressureSupportsOnlyPerRepositorySyncModes(t *testing.T) {
	t.Parallel()

	want := map[string]bool{
		"explicit":    true,
		"githubOrg":   true,
		"gitlabGroup": true,
		"filesystem":  false,
	}
	for mode, supported := range want {
		if got := selectionBackpressureSupportsMode(mode); got != supported {
			t.Errorf("selectionBackpressureSupportsMode(%q) = %v, want %v", mode, got, supported)
		}
	}
	if selectionBackpressureSupportsMode("") {
		t.Error("selectionBackpressureSupportsMode(\"\") = true, want false (an unset mode never runs a sync)")
	}
}

// TestWireSelectionBackpressureRefusesFilesystemMode proves the startup
// refusal: the schedule leg set against filesystem mode is an error naming both
// variables, never a silent no-op and never a silent drop. With no selection env
// the wire leaves the selector untouched, which is how the golden-corpus gate,
// `eshu scan` and the local supervisor all start.
func TestWireSelectionBackpressureRefusesFilesystemMode(t *testing.T) {
	t.Parallel()

	env := fullBackpressureEnv()
	selector := &NativeRepositorySelector{Config: RepoSyncConfig{SourceMode: "filesystem"}}
	err := selector.WireSelectionBackpressure(&scriptedSlowedStore{}, func(name string) string { return env[name] })
	if err == nil {
		t.Fatal("filesystem mode wired the schedule leg, want a startup error")
	}
	for _, name := range []string{"ESHU_REPO_SOURCE_MODE", GitSelectionMinResyncIntervalEnv} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
	if selector.SelectionBackpressure.Enabled() || selector.SelectionSlowed != nil {
		t.Fatal("refused wire left the leg armed, want an untouched selector")
	}

	untouched := &NativeRepositorySelector{Config: RepoSyncConfig{SourceMode: "filesystem"}}
	if err := untouched.WireSelectionBackpressure(&scriptedSlowedStore{}, func(string) string { return "" }); err != nil {
		t.Fatalf("filesystem mode with no selection env: error = %v, want nil", err)
	}
	if untouched.SelectionBackpressure.Enabled() || untouched.SelectionSlowed != nil {
		t.Fatal("filesystem mode with no selection env armed the leg, want an untouched selector")
	}
}

// TestSelectRepositoriesFilesystemModeNeverDefersAnEditedRepository is the
// selector-level regression for the drop (#10882): with the schedule leg set
// on the struct, an edit inside the resync interval must still be selected on
// the next poll, and the managed copy must carry the edit. Before the fix the
// leg filtered the repository out ahead of the filesystem sync, the sync
// stamped the whole-root manifest anyway, and every later poll read "unchanged".
// Copy mode (managed workspace) and direct mode are both covered; the real
// syncFilesystemRepositories runs on temp directories.
func TestSelectRepositoriesFilesystemModeNeverDefersAnEditedRepository(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		direct bool
	}{
		{name: "copy", direct: false},
		{name: "direct", direct: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			repos := t.TempDir()
			service := filepath.Join(root, "eshu-hq", "service-a", "main.go")
			other := filepath.Join(root, "eshu-hq", "service-b", "main.go")
			writeSelectionTestFile(t, service, "package main\n")
			writeSelectionTestFile(t, other, "package main\n")
			now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
			selector := &NativeRepositorySelector{
				Config: RepoSyncConfig{
					ReposDir: repos, SourceMode: "filesystem", FilesystemRoot: root, FilesystemDirect: tc.direct,
					Component: "collector-git", CloneDepth: 1, RepoLimit: 4000, GitAuthMethod: "none",
				},
				Now:                   func() time.Time { return now },
				SelectionBackpressure: SelectionBackpressureConfig{MinResyncInterval: time.Hour, StretchMultiplier: 4},
				SelectionSlowed:       func(context.Context) (bool, error) { return false, nil },
			}

			first, err := selector.SelectRepositories(context.Background())
			if err != nil || len(first.Repositories) != 2 {
				t.Fatalf("first poll = %d repositories, %v, want 2, nil", len(first.Repositories), err)
			}
			now = now.Add(time.Minute)
			writeSelectionTestFile(t, service, "package main\n\nfunc changed() {}\n")

			second, err := selector.SelectRepositories(context.Background())
			if err != nil {
				t.Fatalf("poll after the edit: %v", err)
			}
			if len(second.Repositories) == 0 {
				t.Fatal("poll after an edit selected 0 repositories inside the resync interval: the edited repository was dropped")
			}
			if tc.direct {
				return
			}
			body, err := os.ReadFile(filepath.Join(repos, "eshu-hq", "service-a", "main.go"))
			if err != nil {
				t.Fatalf("managed copy of the edited repository is missing: %v", err)
			}
			if !strings.Contains(string(body), "changed") {
				t.Fatalf("managed copy does not carry the edit: %q", body)
			}
		})
	}
}
