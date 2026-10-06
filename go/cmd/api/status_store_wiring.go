// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel"

	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
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
func newStatusStore(queryer db.Queryer, instruments *telemetry.Instruments, summaryReader *pgstatus.StatusSummaryReader) pgstatus.StatusStore {
	store := pgstatus.NewStatusStore(queryer).WithSummaryReader(summaryReader)
	store.Instruments = instruments
	return store
}

// newStatusSummaryReader resolves the stored-summary reader (#7009) once for
// the process. Every status snapshot transaction builds its own StatusStore
// through newStatusStore, so the settings and the shared live statement live
// here, not in the per-transaction store. An invalid
// ESHU_STATUS_SUMMARY_STALE_AFTER while the reader is on fails startup.
func newStatusSummaryReader(getenv func(string) string) (*pgstatus.StatusSummaryReader, error) {
	reader, err := pgstatus.NewStatusSummaryReader(getenv)
	if err != nil {
		return nil, fmt.Errorf("configure status summary reader: %w", err)
	}
	return reader, nil
}

// newSnapshotStatusReader confines each status read to one guarded snapshot
// transaction and builds a StatusStore for it that shares the process-wide
// summaryReader.
func newSnapshotStatusReader(
	readStore db.ReadStore,
	instruments *telemetry.Instruments,
	summaryReader *pgstatus.StatusSummaryReader,
) status.Reader {
	return pgaccess.NewSnapshotStatusReader(readStore, func(reader db.Queryer) status.Reader {
		return newStatusStore(reader, instruments, summaryReader)
	}, otel.Tracer(telemetry.DefaultSignalName))
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

// mountRuntimeSurfaceWithPostgresAccess gives only trusted admin methods a
// checkpoint source; readiness checks both pools and the fenced status schema.
func mountRuntimeSurfaceWithPostgresAccess(apiHandler http.Handler,
	serviceName string, reader status.Reader, prometheusHandler http.Handler, access *pgaccess.Access, driver neo4jdriver.DriverWithContext,
) (http.Handler, error) {
	if access == nil {
		return nil, errors.New("postgres access is required for the runtime surface")
	}
	probes := internalruntime.ReadinessProbesForDependencies(nil, driver)
	probes = append(probes, internalruntime.ReadinessProbe{Name: "postgres", Check: access.Ping})
	return internalruntime.NewStatusAdminMux(serviceName,
		pgaccess.NewTrustedStatusReader(reader, access), apiHandler,
		internalruntime.WithPrometheusHandler(prometheusHandler),
		internalruntime.WithReadinessProbes(probes...),
	)
}
