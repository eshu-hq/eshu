// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"io"
	"log/slog"
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
