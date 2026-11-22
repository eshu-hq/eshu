// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildReducerServiceWiresInfraInventoryBackfill is the #11196 wiring
// test: the reducer is the product runtime that records the infra read model
// marker, so buildReducerService must hand the service a backfill runner whose
// port is the Postgres backfill. Without it a fresh install never records
// infra_resource_entities_v2 and readers stay on the graph path.
func TestBuildReducerServiceWiresInfraInventoryBackfill(t *testing.T) {
	t.Parallel()

	database := &fakeReducerDB{}
	service, err := buildReducerService(context.Background(), database, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(database), stubCypherReader{}, stubCypherReader{}, obligationConsumerDisabledGetenv(func(string) string { return "" }), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}
	if service.InfraInventoryBackfillRunner == nil || service.InfraInventoryBackfillRunner.Backfiller == nil {
		t.Fatal("buildReducerService() infra inventory backfill runner or its port = nil, want the Postgres backfill")
	}
}
