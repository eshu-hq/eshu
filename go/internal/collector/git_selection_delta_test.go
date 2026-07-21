// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestUpdateRepositoryReturnsChangedAndDeletedFileTargets(t *testing.T) {
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	checkoutMarker := filepath.Join(binDir, "checkout.ok")
	if err := os.WriteFile(fakeGit, []byte(`#!/bin/sh
case "$*" in
	*"symbolic-ref refs/remotes/origin/HEAD"*)
		printf "refs/remotes/origin/main\n"
		;;
	*"fetch --progress"*)
		;;
	*"rev-parse HEAD"*)
		printf "oldsha\n"
		;;
	*"rev-parse refs/remotes/origin/main"*)
		printf "newsha\n"
		;;
	*"cat-file -e oldsha"*)
		exit 0
		;;
	*"diff --name-status -z --find-renames oldsha refs/remotes/origin/main"*)
		printf "M\0cmd/api/main.go\0"
		printf "D\0internal/old/deleted.go\0"
		printf "R100\0internal/old/name.go\0internal/new/name.go\0"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		touch "`+checkoutMarker+`"
		;;
	*)
		;;
esac
`), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	repoPath := t.TempDir()
	updated, delta, _, err := updateRepository(
		context.Background(),
		RepoSyncConfig{CloneDepth: 1, GitAuthMethod: "none"},
		repoPath,
		"",
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		gitSyncLogEventFor("example/private-service", 1, 1),
		"oldsha",
		nil,
	)
	if err != nil {
		t.Fatalf("updateRepository() error = %v, want nil", err)
	}
	if !updated {
		t.Fatal("updateRepository() updated = false, want true")
	}
	if _, err := os.Stat(checkoutMarker); err != nil {
		t.Fatalf("checkout marker missing: %v", err)
	}

	wantChanged := []string{
		filepath.Join(repoPath, "cmd", "api", "main.go"),
		filepath.Join(repoPath, "internal", "new", "name.go"),
	}
	if !reflect.DeepEqual(delta.ChangedFileTargets, wantChanged) {
		t.Fatalf("ChangedFileTargets = %#v, want %#v", delta.ChangedFileTargets, wantChanged)
	}
	wantDeleted := []string{"internal/old/deleted.go", "internal/old/name.go"}
	if !reflect.DeepEqual(delta.DeletedRelativePaths, wantDeleted) {
		t.Fatalf("DeletedRelativePaths = %#v, want %#v", delta.DeletedRelativePaths, wantDeleted)
	}
}

func TestBuildSelectedRepositoriesCarriesGitDeltaFileTargets(t *testing.T) {
	t.Parallel()

	repoPath := filepath.Join(t.TempDir(), "org", "repo")
	config := RepoSyncConfig{SourceMode: "explicit", ReposDir: filepath.Dir(filepath.Dir(repoPath))}
	selection := GitSyncSelection{
		SelectedRepoPaths: []string{repoPath},
		DeltaByRepoPath: map[string]GitSyncDelta{
			repoPath: {
				ChangedFileTargets:   []string{filepath.Join(repoPath, "changed.go")},
				DeletedRelativePaths: []string{"deleted.go"},
			},
		},
	}

	selected := buildSelectedRepositories(config, selection.SelectedRepoPaths, selection.DeltaByRepoPath, selection.ReconcileByRepoPath, nil, nil, nil)
	if len(selected) != 1 {
		t.Fatalf("selected repositories = %d, want 1", len(selected))
	}
	if !selected[0].Delta {
		t.Fatal("SelectedRepository.Delta = false, want true")
	}
	if got, want := selected[0].FileTargets, []string{filepath.Join(repoPath, "changed.go")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("FileTargets = %#v, want %#v", got, want)
	}
	if got, want := selected[0].DeletedRelativePaths, []string{"deleted.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeletedRelativePaths = %#v, want %#v", got, want)
	}
}
