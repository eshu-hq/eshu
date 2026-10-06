// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"

	"go.opentelemetry.io/otel"

	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newStatusStore constructs the MCP server's read-only StatusStore and wires
// the shared meter-provider instruments onto it. pgstatus.NewStatusStore
// deliberately leaves Instruments nil so the ~30 existing call sites stay
// source-compatible (see the AWSPaginationCheckpointStore pattern); the
// operator status-serving surface sets it explicitly here so the per-read status
// metric (eshu_dp_status_snapshot_read_duration_seconds, recorded by
// internal/storage/postgres/status_read_telemetry.go's statusReadQueryer)
// actually emits.
//
// Extracted from wireMCP — mirroring newWorkflowControlStore (#4459) — so the
// wiring itself, not just construction, is unit testable: a dropped
// store.Instruments assignment would otherwise leave the metric
// contract-complete but silent on the MCP path with no test to catch the
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
