// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8155 disposable PostgreSQL 18 proof of the wired config-artifact walker cycle: two never-derived repositories repair through the real dirty join, deriver and walk claim, the backfill marker records on the clean wrap, and a second cycle matches both as already current; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/testutil"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	storageartifacts "github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// openWalkerDatabase returns a disposable database with the full bootstrap
// schema (migrations 170, 186 and 187 included, so the versions table, the
// fence triggers and the version-0 seed exist). Without the DSN it skips.
func openWalkerDatabase(t *testing.T) (context.Context, postgres.SQLDB) {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableDatabase(t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"), 5*time.Minute)
	sqldb := postgres.SQLDB{DB: db}
	if err := postgres.ApplyBootstrap(ctx, sqldb); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	return ctx, sqldb
}

// seedWalkerVersions gives each repository a content version with no state
// row, so the dirty join names every one of them missing.
func seedWalkerVersions(t *testing.T, ctx context.Context, database postgres.SQLDB, repos ...string) {
	t.Helper()
	for _, repo := range repos {
		if _, err := database.ExecContext(ctx, `
INSERT INTO config_artifact_content_versions (repo_id, version, bumped_xid, bumped_at)
VALUES ($1, 1, pg_current_xact_id(), now())
ON CONFLICT (repo_id) DO NOTHING`, repo); err != nil {
			t.Fatalf("seed version for %s: %v", repo, err)
		}
	}
}

// TestConfigArtifactWalkerCycleLive drives one walker cycle through the real
// stack: the wired runner lists the dirty join, derives each repository with
// the production Deriver, advances the shared walk, and records the backfill
// marker when the walk wraps clean. A second cycle finds both repositories
// already current and reports match without writing.
func TestConfigArtifactWalkerCycleLive(t *testing.T) {
	ctx, database := openWalkerDatabase(t)
	seedWalkerVersions(t, ctx, database, "repository:walker-a", "repository:walker-b")

	reader := metric.NewManualReader()
	inst, err := telemetry.NewInstruments(
		metric.NewMeterProvider(metric.WithReader(reader)).Meter("walker-live-test"))
	require.NoError(t, err)
	runner, err := configArtifactWalkerRunnerFor(ctx,
		func(string) string { return "" }, database, nil, inst, nil)
	require.NoError(t, err)
	require.NotNil(t, runner)

	first, err := runner.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, first.Derived)
	require.Equal(t, 2, first.Repaired)
	require.Equal(t, 0, first.Skipped)
	require.True(t, first.WalkWrapped)
	require.True(t, first.MarkerRecorded)

	complete, err := storageartifacts.BackfillComplete(ctx, database)
	require.NoError(t, err)
	require.True(t, complete)

	second, err := runner.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, second.Derived)
	require.Equal(t, 2, second.Matched)
	require.False(t, second.MarkerRecorded)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	require.Equal(t, int64(2), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_derives_total",
		map[string]string{"trigger": "repair", "outcome": "ok"}))
	require.Equal(t, int64(2), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_derives_total",
		map[string]string{"trigger": "backfill", "outcome": "superseded"}))
	require.Equal(t, int64(2), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_reconcile_total",
		map[string]string{"outcome": "repaired"}))
	require.Equal(t, int64(2), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_reconcile_total",
		map[string]string{"outcome": "match"}))
	// The second cycle drained the missing reason back to zero; the gauge
	// carries the zero instead of a stale-high count.
	require.Equal(t, int64(0), testutil.GaugeValue(t, rm,
		"eshu_dp_config_artifact_stale_repos",
		map[string]string{"reason": "missing"}))

	var rowCount int
	require.NoError(t, database.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM config_artifact_repo_state WHERE repo_id IN ('repository:walker-a','repository:walker-b') AND content_version = 1 AND row_count IS NOT NULL`).Scan(&rowCount))
	require.Equal(t, 2, rowCount)
}
