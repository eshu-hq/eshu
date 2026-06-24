// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
)

func TestBuildBootstrapCollectorWiresDefaultSCIPConfig(t *testing.T) {
	t.Parallel()

	deps, err := buildBootstrapCollector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		func(string) string { return "" },
		nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("buildBootstrapCollector() error = %v, want nil", err)
	}

	source := deps.source.(*collector.GitSource)
	snapshotter := source.Snapshotter.(collector.NativeRepositorySnapshotter)
	if !snapshotter.SCIP.Enabled {
		t.Fatal("buildBootstrapCollector() SCIP enabled by default = false, want true")
	}
}
