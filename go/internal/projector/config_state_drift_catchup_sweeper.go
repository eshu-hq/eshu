// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const (
	defaultConfigStateDriftCatchUpInterval = 5 * time.Minute
	defaultConfigStateDriftCatchUpLimit    = 500

	// configStateDriftCatchUpReason and configStateDriftCatchUpSourceSystem
	// label every intent this sweeper enqueues, mirroring driftIntentReason /
	// driftIntentSourceSystem (bootstrap Phase 3.5) and
	// driftRuntimeTriggerReason / driftRuntimeTriggerSourceSystem (the runtime
	// delta-trigger) in go/internal/storage/postgres. This is the reducer
	// process's own catch-up sweep: the third of three config_state_drift
	// producers, all deduping against each other through the identical
	// (domain, scope_id, generation_id) work_item_id.
	configStateDriftCatchUpReason       = "config_state_drift_catch_up_sweep"
	configStateDriftCatchUpSourceSystem = "reducer_catch_up_sweep"
)

// PendingConfigStateDriftScope names one state_snapshot:* scope with an
// active generation that ConfigStateDriftCatchUpSweeper re-enqueues for
// config_state_drift evaluation.
type PendingConfigStateDriftScope struct {
	ScopeID      string
	GenerationID string
}

// ActiveStateSnapshotScopeLister lists active state_snapshot:* scopes for the
// config-state-drift catch-up sweep, bounded by limit. Implementations should
// order results deterministically so a bounded limit sweeps the full active
// set over a small number of ticks rather than starving the same tail
// forever.
type ActiveStateSnapshotScopeLister interface {
	ListActiveStateSnapshotScopes(ctx context.Context, limit int) ([]PendingConfigStateDriftScope, error)
}

// ConfigStateDriftCatchUpSweeper periodically re-enqueues one
// config_state_drift reducer intent per active state_snapshot:* scope (issue
// #5593 P1-1). It exists to bound the one convergence gap the Ack post-commit
// runtime trigger (ProjectorQueue.ConfigStateDriftTrigger,
// go/internal/storage/postgres/projector_queue_config_state_drift_trigger_hook.go)
// cannot close on its own: that trigger's Enqueue call is best-effort and
// un-retried by design (see ConfigStateDriftRuntimeTrigger's doc comment in
// go/internal/storage/postgres/drift_runtime_trigger.go for why), so a
// timeout or transient Postgres error on that ONE call permanently loses the
// drift evaluation for that generation unless something else re-derives it.
// Before this sweeper existed, the only other paths were a NEW terraform
// apply on the same backend (may never happen again) or an operator manually
// re-running eshu-bootstrap-index (on demand, not bounded). This sweeper adds
// a third, always-running, bounded path: every Interval (default 5 minutes),
// it re-scans the active state_snapshot:* set and re-enqueues, so a lost
// trigger converges within a small fixed bound instead of an unbounded or
// operator-dependent one.
//
// This is DELIBERATELY NOT the redrive mechanism issue #5593's three review
// rounds built and removed (see go/cmd/reducer/AGENTS.md's "config_state_drift
// has no redrive/retry" entry and ConfigStateDriftRuntimeTrigger's doc
// comment for that history). The removed mechanism inspected
// TerraformConfigStateDriftHandler's per-item OUTCOME
// (tfstatebackend.ErrNoConfigRepoOwnsBackend) and rescheduled that exact
// rejected work item through a ledger row that got deleted and re-inserted
// every cycle, which made its own ON CONFLICT DO NOTHING insert a no-op
// against nothing -- producing a fresh pending row, and therefore a fresh
// Handle() run, every cycle forever for every operator-owned backend. This
// sweeper never inspects an outcome: it lists active scopes and enqueues
// against the SAME (domain, scope_id, generation_id) work_item_id every other
// producer already writes. For any generation that already has a
// fact_work_items row -- whether that row's terminal state is "succeeded
// because drift was evaluated" or "succeeded because
// ErrNoConfigRepoOwnsBackend was rejected" -- ON CONFLICT DO NOTHING is a
// TRUE no-op: no new row, no re-run of Handle(). Only a generation with NO
// existing row at all (the exact "trigger fired and lost" gap this sweeper
// targets, or a generation whose Ack-time trigger call never ran at all, e.g.
// because the ingester was down) produces a fresh insert. It is therefore
// safe to run forever at a fixed interval, unlike the removed mechanism.
//
// Enqueue is idempotent for the same underlying reason: the reducer queue
// keys work items by (domain, scope_id, generation_id) and inserts
// ON CONFLICT DO NOTHING, so re-enqueuing an already-covered scope each tick
// is a no-op, and concurrent reducer replicas each running their own sweeper
// converge on the same idempotent inserts without needing a lease.
type ConfigStateDriftCatchUpSweeper struct {
	Active      ActiveStateSnapshotScopeLister
	Intents     ReducerIntentWriter
	Limit       int
	Interval    time.Duration
	Wait        func(context.Context, time.Duration) error
	Logger      *slog.Logger
	Instruments *telemetry.Instruments
}

func (s ConfigStateDriftCatchUpSweeper) limit() int {
	if s.Limit <= 0 {
		return defaultConfigStateDriftCatchUpLimit
	}
	return s.Limit
}

func (s ConfigStateDriftCatchUpSweeper) interval() time.Duration {
	if s.Interval <= 0 {
		return defaultConfigStateDriftCatchUpInterval
	}
	return s.Interval
}

// Run sweeps on an interval until the context is canceled.
func (s ConfigStateDriftCatchUpSweeper) Run(ctx context.Context) error {
	wait := s.Wait
	if wait == nil {
		wait = sleepWithContext
	}
	for {
		if _, err := s.RunOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.logError(ctx, err)
		}
		if err := wait(ctx, s.interval()); err != nil {
			return err
		}
	}
}

// RunOnce lists active state_snapshot:* scopes and enqueues one
// config_state_drift intent per scope. It returns the number of intents the
// DB actually inserted (IntentResult.Count -- see that type's doc comment for
// why this is not len(scopes): most ticks re-list scopes another producer
// already covered, and ON CONFLICT DO NOTHING correctly admits none of them).
func (s ConfigStateDriftCatchUpSweeper) RunOnce(ctx context.Context) (int, error) {
	if s.Active == nil || s.Intents == nil {
		return 0, nil
	}
	started := time.Now()
	scopes, err := s.Active.ListActiveStateSnapshotScopes(ctx, s.limit())
	if err != nil {
		return 0, err
	}
	if len(scopes) == 0 {
		return 0, nil
	}
	intents := make([]ReducerIntent, 0, len(scopes))
	for _, pending := range scopes {
		intents = append(intents, ReducerIntent{
			ScopeID:      pending.ScopeID,
			GenerationID: pending.GenerationID,
			Domain:       reducer.DomainConfigStateDrift,
			Reason:       configStateDriftCatchUpReason,
			SourceSystem: configStateDriftCatchUpSourceSystem,
		})
	}
	result, err := s.Intents.Enqueue(ctx, intents)
	if err != nil {
		return 0, err
	}
	s.recordEnqueueCounter(ctx, result.Count)
	s.logSweep(ctx, len(scopes), result.Count, started)
	return result.Count, nil
}

// recordEnqueueCounter advances CorrelationDriftIntentsEnqueued by the actual
// insertion count, mirroring postgres.recordDriftEnqueueCounter's label shape
// (pack=terraform_config_state_drift, source=<producer>) so this sweep's
// activity shows up on the same dashboard as the other two config_state_drift
// producers, distinguished by source=reducer_catch_up_sweep. A nonzero value
// here on a healthy fleet is itself an operator signal: it means the runtime
// trigger or bootstrap sweep missed a generation this sweep had to pick up.
func (s ConfigStateDriftCatchUpSweeper) recordEnqueueCounter(ctx context.Context, count int) {
	if s.Instruments == nil || s.Instruments.CorrelationDriftIntentsEnqueued == nil {
		return
	}
	s.Instruments.CorrelationDriftIntentsEnqueued.Add(
		ctx,
		int64(count),
		metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionPack, rules.TerraformConfigStateDriftPackName),
			telemetry.AttrSource(configStateDriftCatchUpSourceSystem),
		),
	)
}

func (s ConfigStateDriftCatchUpSweeper) logSweep(ctx context.Context, pending int, enqueued int, startedAt time.Time) {
	if s.Logger == nil {
		return
	}
	s.Logger.InfoContext(
		ctx, "config state drift catch-up sweep completed",
		slog.Int("pending_scopes", pending),
		slog.Int("enqueued_intents", enqueued),
		slog.Float64("duration_seconds", time.Since(startedAt).Seconds()),
		log.Domain(string(reducer.DomainConfigStateDrift)),
	)
}

func (s ConfigStateDriftCatchUpSweeper) logError(ctx context.Context, err error) {
	if s.Logger == nil {
		return
	}
	s.Logger.ErrorContext(ctx, "config state drift catch-up sweep failed", log.Err(err))
}
