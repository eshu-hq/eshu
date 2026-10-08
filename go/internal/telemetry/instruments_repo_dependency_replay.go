// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Closed reason values for eshu_dp_repo_dependency_replay_skipped_total.
const (
	// RepoDependencyReplaySkipInactiveGeneration marks a workload
	// materialization replay request whose generation is no longer the active
	// generation of its scope, so there is nothing left to replay.
	RepoDependencyReplaySkipInactiveGeneration = "inactive_generation"
)

// Closed reason values for eshu_dp_repo_dependency_generation_anomalies_total.
const (
	// RepoDependencyAnomalySupersededItemOnActiveGeneration marks a replay
	// request on the scope's active generation whose stable
	// workload-materialization work item is terminally superseded. The queue
	// never revives it, so the request fails the cycle closed.
	RepoDependencyAnomalySupersededItemOnActiveGeneration = "superseded_item_on_active_generation"
	// RepoDependencyAnomalyUnscheduledFencedReplayOnActiveGeneration marks a
	// RUNS_ON readiness replay on the scope's active generation that the queue
	// could not schedule (a superseded or dead-lettered stable item). The
	// fenced path reports only a boolean, so the cause is not split further.
	// The cycle fails closed.
	RepoDependencyAnomalyUnscheduledFencedReplayOnActiveGeneration = "unscheduled_fenced_replay_on_active_generation"
	// RepoDependencyAnomalyInactiveAcceptedGeneration marks active repo
	// dependency rows whose accepted generation is no longer the scope's
	// active generation. The rows still project; the counter only makes the
	// stale acceptance visible.
	RepoDependencyAnomalyInactiveAcceptedGeneration = "inactive_accepted_generation"
)

// registerRepoDependencyReplayCounters registers the repo-dependency replay
// skip and generation-anomaly counters (#7670) on inst. The runner owes a
// workload-materialization replay for every scope generation its cycle wrote
// edges for, but the acceptance row it reads is not rewritten when a
// generation is retired, so a retired generation has nothing left to replay.
// The skip counter makes that visible instead of quarantining the lane; the
// anomaly counter marks the states that still fail closed or still project.
func registerRepoDependencyReplayCounters(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.RepoDependencyReplaySkipped, err = meter.Int64Counter(
		"eshu_dp_repo_dependency_replay_skipped_total",
		metric.WithDescription("Repo-dependency workload-materialization replay requests skipped because their generation is no longer the scope's active generation, by reason (inactive_generation) (#7670)"),
	); err != nil {
		return fmt.Errorf("register RepoDependencyReplaySkipped counter: %w", err)
	}
	if inst.RepoDependencyGenerationAnomalies, err = meter.Int64Counter(
		"eshu_dp_repo_dependency_generation_anomalies_total",
		metric.WithDescription("Repo-dependency generation states that need an operator's eye, by reason (superseded_item_on_active_generation and unscheduled_fenced_replay_on_active_generation fail the cycle closed; inactive_accepted_generation counts rows that still project) (#7670)"),
	); err != nil {
		return fmt.Errorf("register RepoDependencyGenerationAnomalies counter: %w", err)
	}
	return nil
}
