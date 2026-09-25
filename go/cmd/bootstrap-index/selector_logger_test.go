// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
)

// TestBuildBootstrapCollectorPassesLoggerToSelector proves the bootstrap
// wiring exposes repository-selection progress (#6746): the native selector
// must receive the bootstrap logger so its clone/fetch progress logs are
// visible before the first scope commit.
func TestBuildBootstrapCollectorPassesLoggerToSelector(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps, err := buildBootstrapCollector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		func(string) string { return "" },
		nil, nil, logger,
	)
	if err != nil {
		t.Fatalf("buildBootstrapCollector() error = %v, want nil", err)
	}
	source, ok := deps.source.(*git.GitSource)
	if !ok {
		t.Fatalf("buildBootstrapCollector() source type = %T, want *git.GitSource", deps.source)
	}
	selector, ok := source.Selector.(git.NativeRepositorySelector)
	if !ok {
		t.Fatalf("buildBootstrapCollector() selector type = %T, want git.NativeRepositorySelector", source.Selector)
	}
	if selector.Logger != logger {
		t.Fatal("buildBootstrapCollector() selector logger not wired, want bootstrap logger so selection progress is visible")
	}
}

// TestBuildBootstrapCollectorSelectorEmitsSelectionProgress proves the wired
// logger yields an operator-visible signal (#6746): running repository
// selection through the bootstrap-wired selector must emit the shard-selection
// progress line to the bootstrap logs before any scope commit.
func TestBuildBootstrapCollectorSelectorEmitsSelectionProgress(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	deps, err := buildBootstrapCollector(
		ctx,
		&fakeBootstrapSQLDB{},
		func(key string) string {
			switch key {
			case "ESHU_REPO_SOURCE_MODE":
				return "explicit"
			case "ESHU_REPO_SHARD_COUNT":
				return "2"
			}
			return ""
		},
		nil, nil, logger,
	)
	if err != nil {
		t.Fatalf("buildBootstrapCollector() error = %v, want nil", err)
	}
	source, ok := deps.source.(*git.GitSource)
	if !ok {
		t.Fatalf("buildBootstrapCollector() source type = %T, want *git.GitSource", deps.source)
	}
	selector, ok := source.Selector.(git.NativeRepositorySelector)
	if !ok {
		t.Fatalf("buildBootstrapCollector() selector type = %T, want git.NativeRepositorySelector", source.Selector)
	}
	selector.DiscoverSelection = func(context.Context, git.RepoSyncConfig, string) (git.RepositorySelection, error) {
		return git.RepositorySelection{RepositoryIDs: []string{"alpha", "beta"}}, nil
	}
	selector.SyncGit = func(context.Context, git.RepoSyncConfig, []string) (git.GitSyncSelection, error) {
		return git.GitSyncSelection{}, nil
	}
	if _, err := selector.SelectRepositories(ctx); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got := logs.String(); !strings.Contains(got, "collector repository shard selected") {
		t.Fatalf("selector emitted no shard progress log, want %q in %q", "collector repository shard selected", got)
	}
}
