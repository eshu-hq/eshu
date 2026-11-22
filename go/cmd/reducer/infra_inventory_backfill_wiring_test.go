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
	// A non-nil port is not enough: a no-op adapter would satisfy it and the
	// reducer would never record the marker. The port must be the Postgres
	// adapter (it calls inventory.RunBackground) over the database the service
	// was built with.
	port, ok := service.InfraInventoryBackfillRunner.Backfiller.(postgresInfraInventoryBackfiller)
	if !ok {
		t.Fatalf("buildReducerService() backfill port = %T, want postgresInfraInventoryBackfiller", service.InfraInventoryBackfillRunner.Backfiller)
	}
	if port.database != database {
		t.Fatalf("backfill port database = %v, want the database passed to buildReducerService", port.database)
	}
}
