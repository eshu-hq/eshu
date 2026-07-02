// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newStatusStore constructs the API's read-only StatusStore and wires the
// shared meter-provider instruments onto it. pgstatus.NewStatusStore
// deliberately leaves Instruments nil so the ~30 existing call sites stay
// source-compatible (see the AWSPaginationCheckpointStore pattern); the
// operator status-serving surface sets it explicitly here so the status query
// cache metric (eshu_dp_status_stage_counts_cache_total, recorded in
// internal/storage/postgres/status_stage_counts_cache.go's listStageCounts)
// actually emits.
//
// Extracted from wireAPI — mirroring newWorkflowControlStore (#4459) — so the
// wiring itself, not just construction, is unit testable: a dropped
// store.Instruments assignment would otherwise leave the metric
// contract-complete but silent on the API path with no test to catch the
// regression. instruments may be nil; recording is a no-op in that case.
func newStatusStore(queryer pgstatus.Queryer, instruments *telemetry.Instruments) pgstatus.StatusStore {
	store := pgstatus.NewStatusStore(queryer)
	store.Instruments = instruments
	return store
}
