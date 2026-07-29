// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// defaultConfigStateDriftRedriveDelay is the fallback gap between a runtime
// trigger's initial enqueue and its ledger row's first redrive eligibility
// when RedriveDelay is unset. See ConfigStateDriftRedriveMaxAttempts's doc
// comment for the resulting total bounded window.
const defaultConfigStateDriftRedriveDelay = 5 * time.Minute

// ConfigStateDriftRedriveScheduler is the narrow surface
// ConfigStateDriftRuntimeTrigger needs to schedule a bounded redrive.
// ConfigStateDriftRedriveStore implements it.
type ConfigStateDriftRedriveScheduler interface {
	EnsureScheduled(ctx context.Context, scopeID, generationID string, firstAttemptAt time.Time) error
}

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
//
// Redrive, when set, additionally schedules a bounded redrive for this
// (scope, generation) via ConfigStateDriftRedriveStore (issue #5593 P1-1):
// activating a state_snapshot:* scope generation before the config-side repo
// that owns its backend has synced and activated its own terraform_backends
// fact makes TerraformConfigStateDriftHandler.Handle conclude "no config
// repo owns this backend" -- a non-fatal Result{Status: Succeeded} that
// nothing else would otherwise revisit, because the reducer queue's
// (scope, generation, domain) ON CONFLICT DO NOTHING fence makes a later
// re-enqueue of the SAME generation a silent no-op. Nil is safe (no-op): a
// caller that does not wire a scheduler keeps exactly today's terminal
// behavior.
type ConfigStateDriftRuntimeTrigger struct {
	Queue       projector.ReducerIntentWriter
	Instruments *telemetry.Instruments
	Redrive     ConfigStateDriftRedriveScheduler
	// RedriveDelay overrides defaultConfigStateDriftRedriveDelay for the
	// ledger row's first eligible attempt. Zero uses the default.
	RedriveDelay time.Duration
	// Now overrides time.Now for the redrive schedule's first-attempt
	// timestamp. Nil uses time.Now (UTC).
	Now func() time.Time
}

func (t ConfigStateDriftRuntimeTrigger) now() time.Time {
	if t.Now != nil {
		return t.Now().UTC()
	}
	return time.Now().UTC()
}

func (t ConfigStateDriftRuntimeTrigger) redriveDelay() time.Duration {
	if t.RedriveDelay > 0 {
		return t.RedriveDelay
	}
	return defaultConfigStateDriftRedriveDelay
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

	if t.Redrive != nil {
		firstAttemptAt := t.now().Add(t.redriveDelay())
		if err := t.Redrive.EnsureScheduled(ctx, scopeID, generationID, firstAttemptAt); err != nil {
			// The intent itself already enqueued successfully -- the redrive
			// schedule is a best-effort recovery aid, not a precondition for
			// today's evaluation attempt. Return the error so the caller's
			// hook (runConfigStateDriftTriggerHook) logs it rather than
			// silently dropping it, but the primary enqueue above is
			// unaffected.
			return fmt.Errorf("schedule config state drift redrive: %w", err)
		}
	}

	return nil
}
