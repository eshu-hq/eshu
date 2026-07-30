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
//
// This type has no redrive/retry mechanism for a "no config repo owns this
// backend" rejection, and deliberately so — history matters here. A bounded
// redrive for that exact rejection was built, reviewed, and removed across
// three rounds in issue #5593: first it fired unconditionally on every
// activation (re-evaluating generations that never needed it); narrowing it
// to fire only on the observed rejection (moving scheduling into
// reducer.TerraformConfigStateDriftHandler) fixed that but exposed that
// Handle() re-running on every replay, combined with the ledger row being
// deleted on exhaustion, made EnsureScheduled's ON CONFLICT DO NOTHING
// insert a FRESH row every cycle — an unbounded ~20-minute retry loop for
// every genuinely operator-owned backend, which
// tfstatebackend.Resolver.ResolveConfigCommitForBackend's own doc comment
// names as the dominant real-world cause of this rejection ("the state may
// be operator owned outside Eshu's repo set"). Each fix traded one failure
// mode for another without shrinking the underlying complexity, which is
// the sign the mechanism was solving the problem at the wrong layer.
//
// Issue #5593's acceptance is an OR of two criteria. This trigger satisfies
// criterion 1 on its own terms: a state_snapshot activation always results
// in a drift evaluation attempt, full stop, no further work needed here.
// Criterion 2 ("the read model reports an explicit not-yet-evaluated
// state, covered by a test") is OUT OF SCOPE for this branch — it is being
// addressed in sibling branch 5594-local-backend-default-path, which adds
// an "unresolved" outcome to the config-state drift read surface. Do not
// attempt to satisfy criterion 2 here by re-adding a redrive or a
// terminal/non-terminal distinction on this rejection; that is what the
// three-round history above already tried and removed.
//
// The runtime trigger here does not need a redrive to close criterion 1
// (evaluate on activation, which it now always does). The race a redrive
// would recover — this generation evaluates before its owning config repo
// has synced — self-heals for free on the next real terraform apply: a new
// apply produces a new state_snapshot generation with a new work_item_id
// (proven by TestConfigStateDriftRuntimeTriggerDistinctGenerationsProduceDistinctWorkItemIDs:
// two intents sharing scope+domain but carrying different GenerationID
// values -- exactly what a new apply's bumped serial produces, per
// go/internal/scope/tfstate.go's GenerationID format -- hash to different
// work_item_id values, so the new generation is never blocked by the old
// one's ON CONFLICT DO NOTHING row), evaluated independently by this same
// trigger. A state that never changes again after racing once is the one
// case that does not recover automatically; re-running bootstrap-index
// (which re-scans every active state_snapshot:* scope regardless of age)
// or criterion 2's read-model outcome (sibling branch
// 5594-local-backend-default-path) are the accepted paths for that
// narrower residual gap.
//
// projector.ConfigStateDriftCatchUpSweeper (issue #5593,
// cmd/reducer/config_state_drift_catchup_sweeper.go) does NOT change this: it
// re-enqueues against the same (domain, scope_id, generation_id)
// work_item_id, and a rejected generation already has that row (status
// succeeded), so ON CONFLICT DO NOTHING correctly skips it every sweep. The
// sweeper's actual job is the DIFFERENT, narrower gap of TriggerConfigStateDrift's
// own Enqueue call failing outright (network/timeout/transient Postgres
// error) BEFORE any row was ever written for that generation -- see
// runConfigStateDriftTriggerHook's doc comment in
// projector_queue_config_state_drift_trigger_hook.go for that path's bound.
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
