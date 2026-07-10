// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBuildReducerServiceWiresCodeValueFlowStaleCleanup(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(
		context.Background(),
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		func(string) string { return "" },
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}
	if service.CodeValueFlowStaleCleanupRunner == nil {
		t.Fatal("buildReducerService() code value-flow stale cleanup runner = nil, want non-nil")
	}
	if service.CodeValueFlowStaleCleanupRunner.LeaseManager == nil {
		t.Fatal("buildReducerService() code value-flow stale cleanup lease manager = nil, want non-nil")
	}
	if got := service.CodeValueFlowStaleCleanupRunner.Config.LeaseOwner; !strings.HasPrefix(got, "code-value-flow-stale-cleanup-runner:") {
		t.Fatalf("buildReducerService() code value-flow stale cleanup lease owner = %q, want per-process owner", got)
	}
	if got := service.CodeValueFlowStaleCleanupRunner.Config.DeleteBatchLimit; got <= 0 {
		t.Fatalf("buildReducerService() code value-flow stale cleanup delete batch limit = %d, want positive", got)
	}
}
