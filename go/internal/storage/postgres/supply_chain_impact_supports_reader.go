// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	payloadstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Reducer-side readers of the per-support table (#10880 PR-B2). Every reader
// that reads supply-chain impact findings as reducer input from the supports
// table, instead of the finding facts, checks readiness through the one helper
// below and refuses with the one typed refusal
// (supplychaincore.SupplyChainImpactSupportsNotReadyError, a non-counting
// retryable class) while the table is not ready. A support row carries the fact
// payload verbatim (migration 338), so a reader that decodes the payload sees
// the same fields the fact read saw.

// requireSupplyChainImpactSupportsReady returns nil only when the supports table
// is ready: the backfill marker is complete at this binary's contract version,
// no marker for a higher version exists, and the old-writer fence is empty
// (supplyChainImpactSupportsReadySQL, the pinned readiness predicate). A
// not-ready table returns the typed non-counting refusal; a readiness probe that
// fails returns its own error under its own classification, never the not-ready
// class. The outcome lands on eshu_dp_supply_chain_impact_supports_reads_total
// (reader, outcome=not_ready or failed); a read that ran is counted by the
// caller as served after it returns. A nil instruments records nothing.
func requireSupplyChainImpactSupportsReady(
	ctx context.Context,
	queryer db.Queryer,
	instruments *telemetry.Instruments,
	reader string,
) error {
	ready, err := SupplyChainImpactSupportsReady(ctx, queryer)
	if err != nil {
		instruments.RecordSupplyChainImpactSupportsRead(ctx, reader, telemetry.SupplyChainImpactSupportsReadFailed)
		return err
	}
	if !ready {
		instruments.RecordSupplyChainImpactSupportsRead(ctx, reader, telemetry.SupplyChainImpactSupportsReadNotReady)
		return &supplychaincore.SupplyChainImpactSupportsNotReadyError{Reader: reader}
	}
	return nil
}

// RequireSupplyChainImpactSupportsReady is the exported form of the helper for
// readers outside this package that hold only a queryer (the coordinator's
// Go-modules fence): the same predicate, the same typed refusal, the same
// counter.
func RequireSupplyChainImpactSupportsReady(
	ctx context.Context,
	queryer db.Queryer,
	instruments *telemetry.Instruments,
	reader string,
) error {
	return requireSupplyChainImpactSupportsReady(ctx, queryer, instruments, reader)
}

// supplyChainImpactSupportRawFirstCTE wraps one support predicate in the
// raw-first read shape both reducer readers use: the supports are selected by
// their filter first, in a MATERIALIZED CTE, and only the matching rows are
// joined to the active-generation check. The natural single-WHERE form lets the
// planner drive the join from the active scopes instead, a nested loop of one
// (scope, generation) index probe per active scope: 313 ms to 389 ms on a
// 600,000-support corpus over 6,000 active scopes, against 0.1 ms to 4 ms for
// this form (docs/internal/evidence/10880-b2c/reader_plans.md). predicate must
// bind its filter arms and the keyset cursor; the outer query orders and pages.
func supplyChainImpactSupportRawFirstCTE(predicate string) string {
	return `
WITH matched AS MATERIALIZED (
    SELECT support.fact_id, support.scope_id, support.generation_id, support.source_confidence, support.payload
    FROM supply_chain_impact_finding_supports AS support
    WHERE ` + predicate + `
)
SELECT matched.fact_id, matched.scope_id, matched.generation_id, matched.source_confidence, matched.payload
FROM matched
JOIN ingestion_scopes AS scope
  ON scope.scope_id = matched.scope_id
 AND scope.active_generation_id = matched.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = matched.scope_id
 AND generation.generation_id = matched.generation_id
WHERE generation.status = 'active'
ORDER BY matched.fact_id ASC
`
}

// scanSupplyChainImpactSupportEnvelope scans one support row (fact id, scope,
// generation, source confidence, payload: the identity a reducer decision keys
// on and the verbatim payload; the columns the fact envelope also carries are
// not on a support row and no reducer reader of this path consults them) into the fact envelope shape the
// reducer builders consume, carrying the impact finding fact kind.
func scanSupplyChainImpactSupportEnvelope(rows db.Rows) (facts.Envelope, error) {
	var factID, scopeID, generationID, sourceConfidence string
	var rawPayload []byte
	if err := rows.Scan(&factID, &scopeID, &generationID, &sourceConfidence, &rawPayload); err != nil {
		return facts.Envelope{}, err
	}
	payload, err := payloadstore.UnmarshalPayload(rawPayload)
	if err != nil {
		return facts.Envelope{}, err
	}
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.ReducerSupplyChainImpactFindingFactKind,
		SourceConfidence: sourceConfidence,
		Payload:          payload,
	}, nil
}
