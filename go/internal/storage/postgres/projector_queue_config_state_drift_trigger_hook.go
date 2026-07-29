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
// so an error here means the work_item_id row was never written and the
// reducer queue's per-(scope,generation) ON CONFLICT DO NOTHING dedupe does
// NOT block a later attempt -- the next bootstrap-index Phase 3.5 sweep
// (IngestionStore.EnqueueConfigStateDriftIntents) or the next state snapshot
// activation for the same backend still converges on evaluating this drift
// domain.
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
// config-side repo has activated its own terraform_backends fact. Because the
// resolver's "no owner" outcome is a non-fatal SUCCESS, not a retryable
// failure, the reducer queue's per-(scope,generation) ON CONFLICT DO NOTHING
// dedupe would freeze that premature false negative forever: Phase 3.5's
// later, correctly-ordered enqueue attempt for the SAME generation would find
// the work_item_id row already present and silently no-op. Wire this only on
// the runtime ingester's ProjectorQueue (cmd/ingester/wiring.go), where a NEW
// state snapshot activating post-bootstrap almost always finds the
// config-side correlation already resolved from the prior run -- and where
// ConfigStateDriftRuntimeTrigger's Redrive field (issue #5593 P1-1,
// drift_runtime_redrive.go) gives the remaining race a bounded recovery path
// this hook alone does not provide.
func (q ProjectorQueue) runConfigStateDriftTriggerHook(ctx context.Context, work projector.ScopeGenerationWork) {
	if q.ConfigStateDriftTrigger == nil {
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
