// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"

	"go.opentelemetry.io/otel"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newStatusStore constructs the API's read-only StatusStore and wires the
// shared meter-provider instruments onto it. pgstatus.NewStatusStore
// deliberately leaves Instruments nil so the ~30 existing call sites stay
// source-compatible (see the AWSPaginationCheckpointStore pattern); the
// operator status-serving surface sets it explicitly here so the per-read status
// metric (eshu_dp_status_snapshot_read_duration_seconds, recorded by
// internal/storage/postgres/status_read_telemetry.go's statusReadQueryer)
// actually emits.
//
// Extracted from wireAPI — mirroring newWorkflowControlStore (#4459) — so the
// wiring itself, not just construction, is unit testable: a dropped
// store.Instruments assignment would otherwise leave the metric
// contract-complete but silent on the API path with no test to catch the
// regression. instruments may be nil; recording is a no-op in that case.
func newStatusStore(queryer db.Queryer, instruments *telemetry.Instruments) pgstatus.StatusStore {
	store := pgstatus.NewStatusStore(queryer)
	store.Instruments = instruments
	return store
}

// statusSnapshotStoreName labels the status snapshot reads on the shared
// eshu_dp_postgres_query_duration_seconds histogram and postgres.query spans.
const statusSnapshotStoreName = "status_snapshot"

// newStatusQueryer returns the Postgres queryer the API's status reader uses.
// With instruments wired it wraps the database in pgstatus.InstrumentedDB so
// each of the status snapshot's sequential reads emits a postgres.query span
// and a store="status_snapshot" duration sample (#6794): without it a slow
// status route showed no storage signal. With nil instruments it returns the
// plain SQLQueryer.
func newStatusQueryer(rawDB *sql.DB, instruments *telemetry.Instruments) db.Queryer {
	if instruments == nil {
		return pgstatus.SQLQueryer{DB: rawDB}
	}
	return &pgstatus.InstrumentedDB{
		Inner:       pgstatus.SQLDB{DB: rawDB},
		Tracer:      otel.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   statusSnapshotStoreName,
	}
}
