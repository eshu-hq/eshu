// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// listActiveStateSnapshotScopesQuery lists every state_snapshot:* scope that
// has an active generation. The Phase 3.5 trigger walks the result and
// enqueues one config_state_drift reducer intent per scope so the drift
// handler can claim them after Phase 3 reopens deployment_mapping.
//
// The LIKE predicate is constant-prefix; Postgres uses an index scan when the
// scope_id index exists. The query intentionally does NOT filter on the
// scope's collector_kind because the contract is scope-id-prefix-based per
// scope/tfstate.go:33-40 — adding a second filter would silently drop scopes
// that happened to be authored by a different collector kind.
const listActiveStateSnapshotScopesQuery = `
SELECT scope.scope_id, scope.active_generation_id
FROM ingestion_scopes AS scope
WHERE scope.scope_id LIKE 'state_snapshot:%'
  AND scope.active_generation_id IS NOT NULL
ORDER BY scope.scope_id ASC
`

// driftIntentReason is the audit string stamped onto every drift intent the
// Phase 3.5 helper enqueues. It identifies the producer for operator log
// review.
const driftIntentReason = "bootstrap_phase_3_5_drift_trigger"

// driftIntentSourceSystem labels the producer of the intent for telemetry
// purposes — distinguishes bootstrap-emitted drift intents from the runtime
// delta-trigger (ConfigStateDriftRuntimeTrigger, issue #5593) that emits the
// same domain from ProjectorQueue.Ack.
const driftIntentSourceSystem = "bootstrap_index"

// EnqueueConfigStateDriftIntents implements the Phase 3.5 trigger required by
// the facts-first bootstrap ordering documented in CLAUDE.md. The method
// walks active state_snapshot:* scopes and enqueues one config_state_drift
// intent per scope. The reducer queue dedupes work items by
// (domain, scope_id, generation_id), so re-running bootstrap is idempotent.
//
// Failure modes:
//
//   - Database scan errors are returned as wrapped errors (fatal — Phase 3.5
//     is part of the pipeline contract).
//   - Zero scopes is a no-op; bootstrap can run on repos with no state files.
//   - Enqueue errors fail the whole call; the queue's batch insert is
//     atomic per batch.
func (s IngestionStore) EnqueueConfigStateDriftIntents(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	if s.db == nil {
		return fmt.Errorf("ingestion store db is required")
	}

	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "bootstrap.enqueue_config_state_drift")
		defer span.End()
	}

	start := time.Now()

	intents, err := listActiveStateSnapshotScopes(ctx, s.db)
	if err != nil {
		return err
	}
	if len(intents) == 0 {
		recordDriftEnqueueCounter(ctx, instruments, 0, driftIntentSourceSystem)
		log.Printf("config_state_drift_intents_enqueued count=0 duration_s=%.2f", time.Since(start).Seconds())
		return nil
	}

	// Construct the queue with only the fields the enqueue path actually
	// uses. The reducer queue's enqueue SQL writes NULL for lease_owner and
	// claim_until (see enqueueReducerBatchPrefix); LeaseOwner / LeaseDuration
	// are the claim-side contract and validateEnqueue does not require them.
	queue := ReducerQueue{db: s.db}
	if s.Now != nil {
		queue.Now = s.Now
	}
	result, err := queue.Enqueue(ctx, intents)
	if err != nil {
		return fmt.Errorf("enqueue config_state_drift intents: %w", err)
	}

	// Phase 3.5 succeeded. Record ACTUAL insertions (result.Count, from the
	// INSERT's RowsAffected), not len(intents): the runtime delta-trigger
	// (ConfigStateDriftRuntimeTrigger, issue #5593) can enqueue the identical
	// (scope_id, generation_id, domain) work_item_id before this sweep runs,
	// in which case ON CONFLICT DO NOTHING admits zero rows for that scope
	// here. Reporting len(intents) would double-count that generation across
	// the two producers' combined CorrelationDriftIntentsEnqueued total, which
	// defeats the "decouple trigger-fired from reducer-admitted" purpose this
	// counter exists for (see instruments.go for the label-set rationale).
	recordDriftEnqueueCounter(ctx, instruments, result.Count, driftIntentSourceSystem)
	log.Printf("config_state_drift_intents_enqueued count=%d duration_s=%.2f",
		result.Count, time.Since(start).Seconds())

	return nil
}

// listActiveStateSnapshotScopes scans ingestion_scopes for every
// state_snapshot:* scope with an active generation and translates each row
// into a config_state_drift reducer intent. Returns an empty slice when no
// state-snapshot scope has reached active status yet (common during
// first-collection runs on repos without committed state).
func listActiveStateSnapshotScopes(ctx context.Context, db ExecQueryer) ([]projector.ReducerIntent, error) {
	rows, err := db.QueryContext(ctx, listActiveStateSnapshotScopesQuery)
	if err != nil {
		return nil, fmt.Errorf("list active state_snapshot scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var intents []projector.ReducerIntent
	for rows.Next() {
		var scopeID string
		var generationID string
		if err := rows.Scan(&scopeID, &generationID); err != nil {
			return nil, fmt.Errorf("scan active state_snapshot scope: %w", err)
		}
		scopeID = strings.TrimSpace(scopeID)
		generationID = strings.TrimSpace(generationID)
		if scopeID == "" || generationID == "" {
			continue
		}
		intents = append(intents, projector.ReducerIntent{
			ScopeID:      scopeID,
			GenerationID: generationID,
			Domain:       reducer.DomainConfigStateDrift,
			Reason:       driftIntentReason,
			SourceSystem: driftIntentSourceSystem,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active state_snapshot scopes: %w", err)
	}
	return intents, nil
}

// recordDriftEnqueueCounter advances the CorrelationDriftIntentsEnqueued
// counter by `count` with the bounded label set `pack` + `source`. `source`
// is one of the three producers of this domain: driftIntentSourceSystem
// (bootstrap Phase 3.5), driftRuntimeTriggerSourceSystem
// (ConfigStateDriftRuntimeTrigger, issue #5593), or the reducer's own
// config_state_drift catch-up sweep (projector.ConfigStateDriftCatchUpSweeper,
// source "reducer_catch_up_sweep", recorded from the projector package
// directly since it has no dependency on this postgres-package helper).
// Tolerates a nil instruments handle so callers without telemetry wired
// (early bootstrap test paths) remain operable.
func recordDriftEnqueueCounter(ctx context.Context, instruments *telemetry.Instruments, count int, source string) {
	if instruments == nil || instruments.CorrelationDriftIntentsEnqueued == nil {
		return
	}
	instruments.CorrelationDriftIntentsEnqueued.Add(
		ctx,
		int64(count),
		metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionPack, rules.TerraformConfigStateDriftPackName),
			telemetry.AttrSource(source),
		),
	)
}
