// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"

	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// workloadMaterializationSubDurations converts the internal per-phase timing
// struct into the Result.SubDurations map so the service layer can emit
// per-phase log attributes alongside handler_duration_seconds. Keys use the
// same names as the workload materialization log attributes without the
// "_duration_seconds" suffix so callers can reconstruct attribute names
// consistently.
func workloadMaterializationSubDurations(t workloadMaterializationTiming) map[string]float64 {
	return map[string]float64{
		"load_inputs":      t.loadInputsDuration.Seconds(),
		"build_projection": t.buildProjectionDuration.Seconds(),
		"graph_write":      t.graphWriteDuration.Seconds(),
		"instance_retract": t.instanceRetract.Seconds(),
		"dep_reconcile":    t.dependencyReconcile.Seconds(),
		"dep_retract":      t.dependencyRetract.Seconds(),
		"dep_write":        t.dependencyWrite.Seconds(),
		"phase_publish":    t.phasePublishDuration.Seconds(),
	}
}

// logWorkloadMaterializationCompleted emits the single structured completion
// log for one workload materialization pass, carrying row counts and
// per-phase durations (including the #5473 superseded-instance retraction
// count/duration) so an operator can attribute slow or unexpectedly large
// passes to a specific phase from logs alone.
func logWorkloadMaterializationCompleted(
	ctx context.Context,
	intent Intent,
	candidates []WorkloadCandidate,
	projection *ProjectionResult,
	materializeResult MaterializeResult,
	timing workloadMaterializationTiming,
	instanceRetractRows int,
	dependencyRetractRows int,
	dependencyWriteRows int,
) {
	workloadRows := 0
	instanceRows := 0
	deploymentSourceRows := 0
	runtimePlatformRows := 0
	endpointRows := 0
	if projection != nil {
		workloadRows = len(projection.WorkloadRows)
		instanceRows = len(projection.InstanceRows)
		deploymentSourceRows = len(projection.DeploymentSourceRows)
		runtimePlatformRows = len(projection.RuntimePlatformRows)
		endpointRows = len(projection.EndpointRows)
	}

	slog.InfoContext(
		ctx, "workload materialization completed",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(DomainWorkloadMaterialization)),
		slog.Int("candidate_count", len(candidates)),
		slog.Int("workload_row_count", workloadRows),
		slog.Int("instance_row_count", instanceRows),
		slog.Int("deployment_source_row_count", deploymentSourceRows),
		slog.Int("runtime_platform_row_count", runtimePlatformRows),
		slog.Int("endpoint_row_count", endpointRows),
		slog.Int("workloads_written", materializeResult.WorkloadsWritten),
		slog.Int("instances_written", materializeResult.InstancesWritten),
		slog.Int("deployment_sources_written", materializeResult.DeploymentSourcesWritten),
		slog.Int("runtime_platforms_written", materializeResult.RuntimePlatformsWritten),
		slog.Int("endpoints_written", materializeResult.EndpointsWritten),
		slog.Int("instance_retract_row_count", instanceRetractRows),
		slog.Int("dependency_retract_row_count", dependencyRetractRows),
		slog.Int("dependency_write_row_count", dependencyWriteRows),
		slog.Float64("load_inputs_duration_seconds", timing.loadInputsDuration.Seconds()),
		slog.Float64("build_projection_duration_seconds", timing.buildProjectionDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.graphWriteDuration.Seconds()),
		slog.Float64("instance_retract_duration_seconds", timing.instanceRetract.Seconds()),
		slog.Float64("workload_graph_write_duration_seconds", materializeResult.WorkloadWriteDuration.Seconds()),
		slog.Float64("instance_graph_write_duration_seconds", materializeResult.InstanceWriteDuration.Seconds()),
		slog.Float64("deployment_source_graph_write_duration_seconds", materializeResult.DeploymentSourceDuration.Seconds()),
		slog.Float64("runtime_platform_graph_write_duration_seconds", materializeResult.RuntimePlatformDuration.Seconds()),
		slog.Float64("endpoint_graph_write_duration_seconds", materializeResult.EndpointWriteDuration.Seconds()),
		slog.Float64("dependency_reconcile_duration_seconds", timing.dependencyReconcile.Seconds()),
		slog.Float64("dependency_retract_duration_seconds", timing.dependencyRetract.Seconds()),
		slog.Float64("dependency_write_duration_seconds", timing.dependencyWrite.Seconds()),
		slog.Float64("phase_publish_duration_seconds", timing.phasePublishDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
