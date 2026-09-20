// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildReducerServiceDefersWriterShapeUpgrade proves the #6868 startup
// hang is gone: with a stale writer-shape marker and generations waiting,
// build returns promptly and installs the upgrade runner instead of running
// the drain-waiting refinalize inline. The runner executes once the service
// is serving; the fault-injection kill cells, which boot into driven data
// on a fresh stack, are the live proof.
func TestBuildReducerServiceDefersWriterShapeUpgrade(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{writerShapeUpgradePending: true}
	service, err := buildReducerService(
		context.Background(),
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		func(name string) string { return "" },
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}
	if service.WriterShapeUpgradeRunner == nil {
		t.Fatalf("WriterShapeUpgradeRunner = nil, want a deferred upgrade runner on a stale marker")
	}
}

// TestBuildReducerServiceOmitsWriterShapeUpgradeWhenCurrent proves the
// common path installs no runner: a binary already at the code's writer
// shape boots exactly as before, with no upgrade machinery attached.
func TestBuildReducerServiceOmitsWriterShapeUpgradeWhenCurrent(t *testing.T) {
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
		func(name string) string { return "" },
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}
	if service.WriterShapeUpgradeRunner != nil {
		t.Fatalf("WriterShapeUpgradeRunner != nil, want no runner on a current marker")
	}
}
