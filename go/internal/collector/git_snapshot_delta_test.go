// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestNativeRepositorySnapshotterCarriesDeletedOnlyDeltaMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:             repoRoot,
			Delta:                true,
			DeletedRelativePaths: []string{"old/deleted.go"},
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}
	if !got.Delta {
		t.Fatal("Delta = false, want true")
	}
	if got.FileCount != 0 {
		t.Fatalf("FileCount = %d, want 0", got.FileCount)
	}
	if got, want := got.DeltaRelativePaths, []string{"old/deleted.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeltaRelativePaths = %#v, want %#v", got, want)
	}
	if got, want := got.DeletedRelativePaths, []string{"old/deleted.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeletedRelativePaths = %#v, want %#v", got, want)
	}
}

func TestNativeRepositorySnapshotterDeltaTargetsKeepFullPreScanContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	targetFile := filepath.Join(repoRoot, "app.py")
	writeCollectorTestFile(
		t,
		targetFile,
		"from helpers import Helper\n\ndef handler():\n    return Helper()\n",
	)
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "helpers.py"),
		"class Helper:\n    pass\n",
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:    repoRoot,
			FileTargets: []string{targetFile},
			Delta:       true,
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if got, want := got.FileCount, 1; got != want {
		t.Fatalf("FileCount = %d, want %d", got, want)
	}
	if got, want := len(got.FileData), 1; got != want {
		t.Fatalf("len(FileData) = %d, want %d", got, want)
	}
	if got, want := len(got.ContentFileMetas), 1; got != want {
		t.Fatalf("len(ContentFileMetas) = %d, want %d", got, want)
	}
	helperPaths, ok := got.ImportsMap["Helper"]
	if !ok {
		t.Fatalf("ImportsMap missing unchanged Helper entry: %#v", got.ImportsMap)
	}
	if got, want := filepath.Base(helperPaths[0]), "helpers.py"; got != want {
		t.Fatalf("ImportsMap[Helper][0] base = %q, want %q", got, want)
	}
}

func TestNativeRepositorySnapshotterPreservesDeltaMetadataPathWhitespace(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	targetFile := filepath.Join(repoRoot, "dir", " file.go")
	writeCollectorTestFile(t, targetFile, "package dir\n")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:             repoRoot,
			FileTargets:          []string{targetFile},
			Delta:                true,
			DeletedRelativePaths: []string{"dir/deleted .go"},
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if got, want := got.DeltaRelativePaths, []string{"dir/ file.go", "dir/deleted .go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeltaRelativePaths = %#v, want %#v", got, want)
	}
	if got, want := got.DeletedRelativePaths, []string{"dir/deleted .go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeletedRelativePaths = %#v, want %#v", got, want)
	}
}
