// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBuildIngesterCollectorServiceWiresExplicitSCIPEnable(t *testing.T) {
	t.Parallel()

	service, err := buildIngesterCollectorService(
		postgres.SQLDB{},
		mapGetenv(map[string]string{"SCIP_INDEXER": "true"}),
		func() (string, error) { return t.TempDir(), nil },
		func() []string { return []string{"PATH=/usr/bin"} },
		nil, // tracer
		nil, // instruments
		nil, // logger
	)
	if err != nil {
		t.Fatalf("buildIngesterCollectorService() error = %v, want nil", err)
	}

	source := service.Source.(*gitrepo.GitSource)
	snapshotter := source.Snapshotter.(gitrepo.NativeRepositorySnapshotter)
	if !snapshotter.SCIP.Enabled {
		t.Fatal("buildIngesterCollectorService() SCIP enabled = false, want true")
	}
}
