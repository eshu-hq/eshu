// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"context"
	"encoding/json"
	"fmt"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// listStoredCICDRunCorrelationRowsQuery reads the rows a write is about to
// overwrite, by fact id, inside the write transaction. Tombstones are
// returned so a revived row counts as a change. Argument: $1 the fact ids.
const listStoredCICDRunCorrelationRowsQuery = `
SELECT fact_id, is_tombstone, payload
FROM fact_records
WHERE fact_id = ANY($1::text[])
`

// writeWithVerdict upserts rows in one transaction that first reads the rows
// they overwrite and returns whether a consumer-read field changed. When the
// write changed something and the pass carries a completion-owed recorder,
// the marker is recorded in the same transaction, fenced on the pass's lease,
// so the marker exists exactly when the changed rows committed.
func (w PostgresCICDRunCorrelationWriter) writeWithVerdict(
	ctx context.Context,
	write CICDRunCorrelationWrite,
	rows []factwrite.Row,
) (bool, error) {
	tx, err := w.Beginner.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin ci/cd run correlation write: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	newDigests := make(map[string]string, len(rows))
	factIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		var payload map[string]any
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			return false, fmt.Errorf("decode ci/cd run correlation payload: %w", err)
		}
		newDigests[row.FactID] = completionDigest(payload)
		factIDs = append(factIDs, row.FactID)
	}
	stored := make(map[string]storedCompletionRow, len(rows))
	if len(factIDs) > 0 {
		result, err := tx.QueryContext(ctx, listStoredCICDRunCorrelationRowsQuery, factIDs)
		if err != nil {
			return false, fmt.Errorf("read stored ci/cd run correlations: %w", err)
		}
		defer func() { _ = result.Close() }()
		for result.Next() {
			var (
				factID    string
				tombstone bool
				raw       []byte
			)
			if err := result.Scan(&factID, &tombstone, &raw); err != nil {
				return false, fmt.Errorf("scan stored ci/cd run correlation: %w", err)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				return false, fmt.Errorf("decode stored ci/cd run correlation %s: %w", factID, err)
			}
			stored[factID] = storedCompletionRow{tombstone: tombstone, digest: completionDigest(payload)}
		}
		if err := result.Err(); err != nil {
			return false, fmt.Errorf("read stored ci/cd run correlations: %w", err)
		}
	}
	changed := completionChanged(newDigests, stored)

	if err := factwrite.BatchInsertFacts(ctx, tx, rows); err != nil {
		return false, fmt.Errorf("write ci/cd run correlation fact: %w", err)
	}
	if changed && write.CompletionOwed != nil {
		if err := write.CompletionOwed.RecordCompletionOwed(ctx, tx); err != nil {
			return false, fmt.Errorf("record ci/cd completion owed: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit ci/cd run correlation write: %w", err)
	}
	return changed, nil
}

// completionOwedRecorder returns the recorder for the pass's own item, or nil
// when the handler has none wired.
func (h CICDRunCorrelationHandler) completionOwedRecorder(intent reducercontract.Intent) CompletionOwedRecorder {
	if h.CompletionOwed == nil {
		return nil
	}
	return h.CompletionOwed(intent)
}

// completionSubSignals adds the changed verdict to the result sub-signals so
// the ack can gate the completion event, and counts an unchanged pass on
// eshu_dp_cross_scope_completion_suppressed_total. A write with no verdict
// adds nothing, so the ack emits.
func (h CICDRunCorrelationHandler) completionSubSignals(
	ctx context.Context,
	write CICDRunCorrelationWriteResult,
	signals map[string]float64,
) map[string]float64 {
	if write.CompletionChanged == nil {
		return signals
	}
	if signals == nil {
		signals = map[string]float64{}
	}
	if *write.CompletionChanged {
		signals[CompletionChangedSignal] = 1
		return signals
	}
	signals[CompletionChangedSignal] = 0
	if h.Instruments != nil && h.Instruments.CrossScopeCompletionSuppressed != nil {
		h.Instruments.CrossScopeCompletionSuppressed.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrDomain(string(reducercontract.DomainCICDRunCorrelation)),
		))
	}
	return signals
}
