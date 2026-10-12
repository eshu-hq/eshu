// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestNativeRepositorySelectorFilesystemForceReindexesUnchangedCorpus is the
// #7982 regression: eshu index --force and eshu scan --force must re-index an
// unchanged repository through the same reason-recording path as the other
// forced fulls, instead of taking the normal manifest skip.
func TestNativeRepositorySelectorFilesystemForceReindexesUnchangedCorpus(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")

	base := RepoSyncConfig{
		ReposDir:       reposDir,
		SourceMode:     "filesystem",
		FilesystemRoot: filesystemRoot,
		Component:      "collector-git",
		CloneDepth:     1,
		RepoLimit:      4000,
		GitAuthMethod:  "none",
	}
	selector := NativeRepositorySelector{Config: base}
	first, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("first SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(first.Repositories), 1; got != want {
		t.Fatalf("len(first.Repositories) = %d, want %d", got, want)
	}

	plain := NativeRepositorySelector{Config: base}
	second, err := plain.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("second SelectRepositories() error = %v, want nil", err)
	}
	if got := len(second.Repositories); got != 0 {
		t.Fatalf("len(second.Repositories) = %d, want 0 without force", got)
	}

	forced := NativeRepositorySelector{Config: withForceReindex(base)}
	third, err := forced.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("forced SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(third.Repositories), 1; got != want {
		t.Fatalf("len(forced.Repositories) = %d, want %d", got, want)
	}
	repository := third.Repositories[0]
	if !repository.Reconcile {
		t.Fatal("forced repository Reconcile = false, want true")
	}
	if got, want := repository.ReconcileReason, scope.ReconcileReasonRepositoryReindexRequested; got != want {
		t.Fatalf("forced repository ReconcileReason = %q, want %q", got, want)
	}
}

func withForceReindex(config RepoSyncConfig) RepoSyncConfig {
	config.ForceReindex = true
	return config
}
