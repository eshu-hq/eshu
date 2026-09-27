// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retract

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Bounded write_phase label values on
// eshu_dp_workload_repository_edge_retractions_total.
const (
	// PhaseDefines is the write_phase label for retracted DEFINES edges.
	PhaseDefines = "defines_retract"
	// PhaseRepositoryEndpoint is the write_phase label for retracted
	// repository-side EXPOSES_ENDPOINT edges.
	PhaseRepositoryEndpoint = "repository_endpoint_retract"
)

// Run identifies the reducer intent one retract ran for, so the completion log
// can be matched to its intent without timestamps.
type Run struct {
	ScopeID      string
	GenerationID string
	IntentID     string
	// EntityKeys are the intent's entity keys. They bound what the intent
	// writes, never what the retract keeps.
	EntityKeys []string
}

// Observe records one completed retract: measured deletes on
// eshu_dp_workload_repository_edge_retractions_total (skipped when the executor
// chain could not count them) and one "workload repository edge retract
// completed" log line with the intent, retract mode, keep sizes, stale edges
// found, deletes, and duration. The keep sizes count the scope generation's
// admitted set, not what this intent wrote. The log is a warning when the
// guard read failed and the unguarded deletes ran, when no reader was wired,
// and when the caller skipped the retract for lack of scope truth.
// instruments may be nil.
func Observe(
	ctx context.Context,
	instruments *telemetry.Instruments,
	run Run,
	keepLists []KeepList,
	result Result,
	duration time.Duration,
) {
	if instruments != nil && instruments.WorkloadRepoEdgeRetractions != nil && result.Counted {
		record(ctx, instruments, PhaseDefines, result.DefinesDeleted)
		record(ctx, instruments, PhaseRepositoryEndpoint, result.EndpointEdgesDeleted)
	}
	keptWorkloads, keptEndpoints := 0, 0
	for _, keep := range keepLists {
		keptWorkloads += len(keep.WorkloadIDs)
		keptEndpoints += len(keep.EndpointIDs)
	}
	level, readError := slog.LevelInfo, ""
	if result.ReadErr != nil {
		// The guard read failed and the unguarded keep-list deletes ran:
		// correct, but each one costs proportional to store size on NornicDB.
		level, readError = slog.LevelWarn, result.ReadErr.Error()
	}
	if result.Mode == ModeUnguardedNoReader {
		// Production always wires a reader, so this mode means the composition
		// root dropped the guard: every run pays the unguarded delete.
		level = slog.LevelWarn
	}
	if result.Mode == ModeSkippedNoScopeTruth {
		// Stale edges stay until a run with scope truth: production wires the
		// correlated loader, so this means the composition root changed.
		level = slog.LevelWarn
	}
	slog.Log(ctx, level, "workload repository edge retract completed",
		"scope_id", run.ScopeID,
		"generation_id", run.GenerationID,
		"intent_id", run.IntentID,
		"entity_keys", run.EntityKeys,
		"retract_mode", result.Mode,
		"repository_count", result.Repositories,
		"kept_workload_count", keptWorkloads,
		"kept_endpoint_count", keptEndpoints,
		"stale_defines", result.StaleDefines,
		"stale_repository_endpoint_edges", result.StaleEndpointEdges,
		"defines_deleted", result.DefinesDeleted,
		"repository_endpoint_edges_deleted", result.EndpointEdgesDeleted,
		"deletes_counted", result.Counted,
		"read_error", readError,
		"duration_s", duration.Seconds(),
	)
}

func record(ctx context.Context, instruments *telemetry.Instruments, phase string, count int64) {
	if count <= 0 {
		return
	}
	instruments.WorkloadRepoEdgeRetractions.Add(ctx, count, metric.WithAttributes(
		telemetry.AttrWritePhase(phase),
	))
}
