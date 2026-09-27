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

// Bounded label values on eshu_dp_reconciliation_drift_retractions_total.
const (
	// MetricDomain is the domain label for stale workload repository edges.
	MetricDomain = "workload_materialization"
	// PhaseDefines is the write_phase label for retracted DEFINES edges.
	PhaseDefines = "defines_retract"
	// PhaseRepositoryEndpoint is the write_phase label for retracted
	// repository-side EXPOSES_ENDPOINT edges.
	PhaseRepositoryEndpoint = "repository_endpoint_retract"
	kindEdge                = "edge"
)

// Observe records one completed retract: measured deletes on
// eshu_dp_reconciliation_drift_retractions_total (skipped when the executor
// chain could not count them) and one "workload repository edge retract
// completed" log line with the scope, generation, retract mode, keep sizes,
// stale edges found, deletes, and duration. The log is a warning when the
// guard read failed and the unguarded deletes ran. instruments may be nil.
func Observe(
	ctx context.Context,
	instruments *telemetry.Instruments,
	scopeID, generationID string,
	keepLists []KeepList,
	result Result,
	duration time.Duration,
) {
	if instruments != nil && instruments.ReconciliationDriftRetractions != nil && result.Counted {
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
	slog.Log(ctx, level, "workload repository edge retract completed",
		"scope_id", scopeID,
		"generation_id", generationID,
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
	instruments.ReconciliationDriftRetractions.Add(ctx, count, metric.WithAttributes(
		telemetry.AttrDomain(MetricDomain),
		telemetry.AttrWritePhase(phase),
		telemetry.AttrKind(kindEdge),
	))
}
