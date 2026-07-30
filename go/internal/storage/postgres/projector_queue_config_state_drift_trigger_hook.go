// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ConfigStateDriftTrigger is the narrow surface ProjectorQueue.Ack calls to
// enqueue a config_state_drift reducer intent when a state_snapshot:* scope
// generation activates (issue #5593). ConfigStateDriftRuntimeTrigger
// implements it.
type ConfigStateDriftTrigger interface {
	TriggerConfigStateDrift(ctx context.Context, scopeID, generationID string) error
}

// configStateDriftTriggerScopePrefix mirrors driftIntentScopePrefix in
// go/internal/reducer/terraform_config_state_drift.go and
// listActiveStateSnapshotScopesQuery's LIKE predicate in drift_enqueue.go --
// all three MUST agree on the state_snapshot scope shape
// (state_snapshot:<backend_kind>:<locator_hash>, see
// go/internal/scope/tfstate.go:33-40).
const configStateDriftTriggerScopePrefix = "state_snapshot:"

// bootstrapIndexProjectorLeaseOwner is the exact LeaseOwner literal
// cmd/bootstrap-index/wiring.go passes to postgres.NewProjectorQueue
// (`postgres.NewProjectorQueue(instrumentedDB, "bootstrap-index", ...)`).
// runConfigStateDriftTriggerHook uses it as a runtime guard (issue #5593):
// "MUST NOT be wired on bootstrap-index's ProjectorQueue" was
// previously enforced only by this file's doc comment, AGENTS.md, and
// doc.go -- wire it there by accident (e.g. by copying cmd/ingester/wiring.go's
// ConfigStateDriftTrigger assignment) and nothing would have stopped the
// exact Phase-1 ordering race those docs describe. This constant makes the
// constraint mechanical: even a mis-wired ConfigStateDriftTrigger on a
// bootstrap-index-owned queue never fires.
const bootstrapIndexProjectorLeaseOwner = "bootstrap-index"

// runConfigStateDriftTriggerHook calls the wired ConfigStateDriftTrigger
// AFTER Ack's own transaction has committed, mirroring
// runCrossplaneRedriveHook's ordering rationale (issue #5476): the
// generation is already durably active by the time this runs, and enqueueing
// one reducer intent is a single bounded INSERT (not unbounded fan-out), but
// keeping it outside Ack's transaction still avoids growing Ack's fixed
// five-statement critical section for the many scope kinds that never match
// the state_snapshot prefix.
//
// A hook failure is deliberately never returned to the caller: by the time
// this runs, the generation is already correctly activated (Ack committed),
// and that real, valid work must not be reverted or reported as failed
// because a best-effort downstream enqueue attempt errored. Unlike the
// Crossplane sweep, a failed TriggerConfigStateDrift call leaves no durable
// partial state to resume: ReducerQueue.Enqueue is one atomic batch INSERT,
// so an error here means the work_item_id row was never written, and this
// call is NEVER retried on its own -- Ack's own five-statement transaction
// already committed and must not be blocked or looped waiting on reducer
// admission a second time (that is what the perf evidence in this branch
// measured and bounded; see docs/internal/evidence/5593-config-state-drift-ack-latency.md).
//
// Convergence for that lost call is bounded, not reliant on either producer
// firing again by chance (issue #5593):
// projector.ConfigStateDriftCatchUpSweeper, started from
// cmd/reducer/config_state_drift_catchup_sweeper.go and running continuously
// in the steady-state reducer process, re-scans every active
// state_snapshot:* scope on a fixed interval (default 5 minutes) and
// re-enqueues through the identical (domain, scope_id, generation_id)
// work_item_id. Because that work_item_id has no row at all for a generation
// this hook's Enqueue call failed on, ON CONFLICT DO NOTHING admits the
// sweep's retry normally, closing the gap within one sweep interval instead
// of waiting for an unrelated bootstrap-index re-run
// (IngestionStore.EnqueueConfigStateDriftIntents) or the next state snapshot
// activation for the same backend, either of which may be arbitrarily far
// away or never happen again in a continuously running deployment. This
// counter (ConfigStateDriftRuntimeTriggerFailures{outcome="trigger_error"})
// plus the ERROR log above are the trace that a specific generation took
// this path rather than the direct one; the sweep's own
// CorrelationDriftIntentsEnqueued{source="reducer_catch_up_sweep"} advancing
// above zero is the trace that the sweep actually had to pick something up.
//
// MUST NOT be wired on the ProjectorQueue bootstrap-index constructs
// (cmd/bootstrap-index/wiring.go): bootstrap's Phase 3.5 deliberately runs
// after "wait for source-local projector drain" has activated every scope in
// the finite bootstrap corpus, including the config-side repo that owns a
// given state snapshot's backend --
// reducer.TerraformConfigStateDriftHandler's backend resolver
// (tfstatebackend.Resolver.ResolveConfigCommitForBackend) reads that
// config-side repo's OWN active-generation terraform_backends parser fact
// directly (go/internal/storage/postgres/tfstate_backend_canonical.go), with
// no dependency on this generation's own cross-repo correlation having run.
// Firing this trigger during bootstrap's own Phase 1 -- before every scope in
// the corpus has necessarily activated -- risks the identical race the
// runtime path has: evaluating a state_snapshot scope before its owning
// config-side repo has activated its own terraform_backends fact.
//
// A "no owner" outcome from that race IS durably terminal for that one
// generation (issue #5593: a bounded runtime redrive for this exact
// rejection was built, reviewed, and removed -- see
// ConfigStateDriftRuntimeTrigger's doc comment in drift_runtime_trigger.go
// for the full history and why retrying it automatically caused more harm
// than the race it was meant to fix). That is a real, but bounded and
// self-healing, cost: a NEW terraform apply on the SAME backend produces a
// NEW state_snapshot generation, which this trigger evaluates independently
// of the prior generation's outcome. On the runtime ingester's ProjectorQueue
// (cmd/ingester/wiring.go), a NEW state snapshot activating post-bootstrap
// almost always finds the config-side correlation already resolved from the
// prior run, so this race is rare there. Firing during bootstrap's own
// Phase 1 -- before every repo in the corpus has necessarily activated --
// would make the race common instead of rare, and there is no redrive to
// recover it: wiring this trigger on bootstrap-index would durably and
// silently under-evaluate config_state_drift for the affected generations
// until their next real terraform apply. Wire this only on the runtime
// ingester's ProjectorQueue.
func (q ProjectorQueue) runConfigStateDriftTriggerHook(ctx context.Context, work projector.ScopeGenerationWork) {
	if q.ConfigStateDriftTrigger == nil {
		return
	}
	if q.LeaseOwner == bootstrapIndexProjectorLeaseOwner {
		// Structural guard, not just documentation: refuse to fire even if a
		// future edit mis-wires ConfigStateDriftTrigger on bootstrap-index's
		// queue. Loud on purpose (ERROR log + counter) so the misconfiguration
		// is visible immediately instead of showing up weeks later as
		// under-evaluated config_state_drift generations from the Phase-1
		// ordering race this guard exists to prevent.
		slog.ErrorContext(
			ctx, "config state drift runtime trigger is wired on bootstrap-index's ProjectorQueue; refusing to fire",
			"scope_id", work.Scope.ScopeID,
			"generation_id", work.Generation.GenerationID,
		)
		if q.Instruments != nil && q.Instruments.ConfigStateDriftRuntimeTriggerFailures != nil {
			q.Instruments.ConfigStateDriftRuntimeTriggerFailures.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrOutcome("bootstrap_wiring_rejected"),
			))
		}
		return
	}
	if !strings.HasPrefix(work.Scope.ScopeID, configStateDriftTriggerScopePrefix) {
		return
	}
	if err := q.ConfigStateDriftTrigger.TriggerConfigStateDrift(ctx, work.Scope.ScopeID, work.Generation.GenerationID); err != nil {
		slog.ErrorContext(
			ctx, "config state drift runtime trigger failed",
			"scope_id", work.Scope.ScopeID,
			"generation_id", work.Generation.GenerationID,
			"error", err,
		)
		if q.Instruments != nil && q.Instruments.ConfigStateDriftRuntimeTriggerFailures != nil {
			q.Instruments.ConfigStateDriftRuntimeTriggerFailures.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrOutcome("trigger_error"),
			))
		}
	}
}
