// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11226 disposable PostgreSQL 18 proof that the reducer's wired infra backfill runner records the infra read model marker and counts one completed run: infraInventoryBackfillRunnerFor over migrated Postgres with seeded repositories, through the real adapter, inventory.RunBackground and Backfiller.Run; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/testutil"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// seedInfraBackfillRepositories gives each repository one infra-labelled content
// entity, the rows the backfill derives infra_resource_entities from.
func seedInfraBackfillRepositories(t *testing.T, ctx context.Context, database postgres.SQLDB, repos ...string) {
	t.Helper()
	for _, repo := range repos {
		_, err := database.ExecContext(ctx, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, metadata, indexed_at)
VALUES ($1, $2, 'main.tf', 'TerraformResource', 'aws_s3_bucket.logs', 1, 2, '', '{"provider":"aws"}'::jsonb, now())
ON CONFLICT (entity_id) DO NOTHING`, repo+"/r1", repo)
		require.NoError(t, err, "seed content entity for %s", repo)
	}
}

// TestInfraInventoryBackfillRunnerRecordsTheMarkerLive is the reducer-level proof
// the #11210 wiring tests could not give (#11226): the runner the reducer starts
// as a side runner, built by infraInventoryBackfillRunnerFor, records the infra
// read model marker on migrated Postgres and counts exactly one completed run in
// eshu_dp_infra_inventory_backfill_runs_total. With the wiring test and the
// service start test it pins the chain buildReducerService, Service.startSideRunners,
// BackfillRunner, the adapter, inventory.RunBackground, Backfiller.Run.
func TestInfraInventoryBackfillRunnerRecordsTheMarkerLive(t *testing.T) {
	ctx, database := openWalkerDatabase(t)
	seedInfraBackfillRepositories(t, ctx, database, "repository:backfill-a", "repository:backfill-b")

	complete, err := inventory.BackfillComplete(ctx, database)
	require.NoError(t, err)
	require.False(t, complete, "the marker must not exist before the runner runs")

	reader := metric.NewManualReader()
	inst, err := telemetry.NewInstruments(
		metric.NewMeterProvider(metric.WithReader(reader)).Meter("infra-backfill-live-test"))
	require.NoError(t, err)

	runner := infraInventoryBackfillRunnerFor(database, inst, nil)
	require.NoError(t, runner.Run(ctx))

	complete, err = inventory.BackfillComplete(ctx, database)
	require.NoError(t, err)
	require.True(t, complete, "the runner must record the marker")

	var markers int
	require.NoError(t, database.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM infra_resource_entity_backfill_markers WHERE marker_name = $1`,
		inventory.BackfillMarker).Scan(&markers))
	require.Equal(t, 1, markers)

	var derived int
	require.NoError(t, database.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM infra_resource_entities WHERE repo_id IN ('repository:backfill-a','repository:backfill-b')`).Scan(&derived))
	require.Equal(t, 2, derived, "the backfill derives one row per seeded repository")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	require.Equal(t, int64(1), testutil.CounterValue(t, rm,
		"eshu_dp_infra_inventory_backfill_runs_total",
		map[string]string{"outcome": "completed"}))
}
