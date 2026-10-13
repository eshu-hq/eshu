// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
)

// TestApplyBootstrapArgOverridesForceMarksReindex proves the #7982 wiring:
// --force reaches the filesystem selector as ForceReindex, which the selector
// turns into a repository_reindex_requested full.
func TestApplyBootstrapArgOverridesForceMarksReindex(t *testing.T) {
	t.Parallel()

	config := git.RepoSyncConfig{SourceMode: "filesystem", FilesystemRoot: "/work/root"}
	applyBootstrapArgOverrides(&config, bootstrapArgs{Force: true})
	if !config.ForceReindex {
		t.Fatal("ForceReindex = false after --force, want true")
	}
}

// TestApplyBootstrapArgOverridesPathImpliesFilesystem proves the --path argv
// contract eshu index relies on: a path value names the filesystem root even
// when the environment left the default githubOrg mode.
func TestApplyBootstrapArgOverridesPathImpliesFilesystem(t *testing.T) {
	t.Parallel()

	config := git.RepoSyncConfig{SourceMode: "githubOrg"}
	applyBootstrapArgOverrides(&config, bootstrapArgs{Path: "/work/root"})
	if got, want := config.FilesystemRoot, "/work/root"; got != want {
		t.Fatalf("FilesystemRoot = %q, want %q", got, want)
	}
	if got, want := config.SourceMode, "filesystem"; got != want {
		t.Fatalf("SourceMode = %q, want %q", got, want)
	}
}

// TestBuildBootstrapCollectorWiresForceToSelector proves the full argv path:
// --force on the bootstrap command line reaches the native selector config.
func TestBuildBootstrapCollectorWiresForceToSelector(t *testing.T) {
	t.Parallel()

	deps, err := buildBootstrapCollector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		func(key string) string {
			if key == "ESHU_REPO_SOURCE_MODE" {
				return "filesystem"
			}
			if key == "ESHU_FILESYSTEM_ROOT" {
				return t.TempDir()
			}
			return ""
		},
		nil, nil, nil,
		bootstrapArgs{Force: true},
	)
	if err != nil {
		t.Fatalf("buildBootstrapCollector() error = %v, want nil", err)
	}
	source, ok := deps.source.(*git.GitSource)
	if !ok {
		t.Fatalf("source type = %T, want *git.GitSource", deps.source)
	}
	selector, ok := source.Selector.(git.NativeRepositorySelector)
	if !ok {
		t.Fatalf("selector type = %T, want git.NativeRepositorySelector", source.Selector)
	}
	if !selector.Config.ForceReindex {
		t.Fatal("selector Config.ForceReindex = false, want true")
	}
}

// TestBuildBootstrapCollectorWarnsWhenForceLeavesFilesystemMode proves the
// #7982 hostile-read case: --force with a non-filesystem source mode must not
// stay silent, since ForceReindex only drives the filesystem selector.
func TestBuildBootstrapCollectorWarnsWhenForceLeavesFilesystemMode(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	_, err := buildBootstrapCollector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		func(string) string { return "" },
		nil, nil, logger,
		bootstrapArgs{Force: true},
	)
	if err != nil {
		t.Fatalf("buildBootstrapCollector() error = %v, want nil", err)
	}
	if !strings.Contains(logs.String(), "--force applies to filesystem source mode only") {
		t.Fatalf("logs missing force-mode warning; got %q", logs.String())
	}
}
