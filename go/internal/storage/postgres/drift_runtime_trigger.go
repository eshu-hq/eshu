// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// driftRuntimeTriggerReason is the audit string stamped onto every drift
// intent ConfigStateDriftRuntimeTrigger enqueues. It identifies the producer
// for operator log review, mirroring driftIntentReason for the bootstrap
// Phase 3.5 path.
const driftRuntimeTriggerReason = "runtime_state_snapshot_activation_trigger"

// driftRuntimeTriggerSourceSystem labels ConfigStateDriftRuntimeTrigger's
// enqueues for telemetry purposes (issue #5593) — distinguishes them from
// driftIntentSourceSystem (bootstrap Phase 3.5) on the shared
// CorrelationDriftIntentsEnqueued counter.
const driftRuntimeTriggerSourceSystem = "ingester_runtime_trigger"

// ConfigStateDriftRuntimeTrigger implements ProjectorQueue's
// ConfigStateDriftTrigger by enqueueing one config_state_drift reducer
// intent through the same ReducerIntentWriter contract
// IngestionStore.EnqueueConfigStateDriftIntents uses for its Phase 3.5
// sweep. Wiring this on an ingester's ProjectorQueue closes the gap where a
// terraform_state_snapshot landing outside a bootstrap-index pass was never
// drift-evaluated until the next bootstrap-index run (issue #5593).
//
// Queue is typically the same admission-aware ReducerIntentWriter the
// runtime's projector.Runtime.IntentWriter uses (see
// cmd/ingester/wiring.go's ingesterReducerIntentWriter), so this trigger
// observes the same graph-write-pressure backpressure as every other
// reducer intent instead of bypassing it with a raw queue handle.
type ConfigStateDriftRuntimeTrigger struct {
	Queue       projector.ReducerIntentWriter
	Instruments *telemetry.Instruments
}

// TriggerConfigStateDrift enqueues one config_state_drift reducer intent for
// (scopeID, generationID). The reducer queue dedupes work items by
// (domain, scope_id, generation_id) via ON CONFLICT DO NOTHING, so calling
// this for a generation the bootstrap Phase 3.5 sweep already enqueued (or
// vice versa) is a safe no-op, not a duplicate evaluation.
func (t ConfigStateDriftRuntimeTrigger) TriggerConfigStateDrift(
	ctx context.Context,
	scopeID string,
	generationID string,
) error {
	if t.Queue == nil {
		return fmt.Errorf("config state drift runtime trigger requires a reducer intent writer")
	}

	result, err := t.Queue.Enqueue(ctx, []projector.ReducerIntent{{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainConfigStateDrift,
		Reason:       driftRuntimeTriggerReason,
		SourceSystem: driftRuntimeTriggerSourceSystem,
	}})
	if err != nil {
		return fmt.Errorf("enqueue runtime config_state_drift intent: %w", err)
	}

	recordDriftEnqueueCounter(ctx, t.Instruments, result.Count, driftRuntimeTriggerSourceSystem)
	return nil
}
