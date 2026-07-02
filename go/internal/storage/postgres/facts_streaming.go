// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// upsertFactBatch inserts one batch of facts using a multi-row INSERT query.
func upsertFactBatch(ctx context.Context, db ExecQueryer, batch []facts.Envelope) error {
	if len(batch) == 0 {
		return nil
	}

	query, args, err := buildUpsertFactBatchQuery(upsertFactBatchSuffix, batch)
	if err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert fact batch (%d facts): %w", len(batch), err)
	}

	return nil
}

// upsertFactBatchReturningAccepted inserts one batch of facts and reports
// which fact_ids actually inserted-or-updated. A fact_id is "accepted" when
// its row is brand new or its fencing_token cleared the
// upsertFactBatchSuffix WHERE guard; a fact_id whose incoming fencing_token
// lost that guard is fenced out and is absent from the returned set.
//
// This is the only atomic way to learn acceptance: Postgres evaluates the
// RETURNING clause per row at the same commit-visible instant as the guard
// itself, so there is no read-then-decide window for a concurrent batch to
// invalidate (issue #4444 review, codex P1 — reading fencing_token separately
// before or after the write would race). Callers that derive downstream
// evidence from a batch (upsertStreamingFacts's afterBatch) MUST filter the
// batch down to this accepted set before deriving anything from it, or a
// fenced-out envelope's stale payload leaks into repository-catalog and
// relationship-evidence derivation even though its fact_records row was
// correctly protected.
func upsertFactBatchReturningAccepted(
	ctx context.Context,
	db ExecQueryer,
	batch []facts.Envelope,
) (map[string]struct{}, error) {
	if len(batch) == 0 {
		return nil, nil
	}

	query, args, err := buildUpsertFactBatchQuery(upsertFactBatchSuffixReturningFactID, batch)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("upsert fact batch returning accepted (%d facts): %w", len(batch), err)
	}
	defer func() { _ = rows.Close() }()

	accepted := make(map[string]struct{}, len(batch))
	for rows.Next() {
		var factID string
		if err := rows.Scan(&factID); err != nil {
			return nil, fmt.Errorf("scan accepted fact_id: %w", err)
		}
		accepted[factID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("upsert fact batch returning accepted rows: %w", err)
	}

	return accepted, nil
}

// buildUpsertFactBatchQuery renders the shared multi-row INSERT values list
// and argument slice for one fact batch, then appends suffix (either
// upsertFactBatchSuffix or upsertFactBatchSuffixReturningFactID) to select the
// plain or RETURNING-aware statement shape from the same row encoding.
func buildUpsertFactBatchQuery(suffix string, batch []facts.Envelope) (string, []any, error) {
	args := make([]any, 0, len(batch)*columnsPerFactRow)
	var values strings.Builder

	for i, envelope := range batch {
		if err := validateFactEnvelope(envelope); err != nil {
			return "", nil, err
		}

		payloadJSON, err := marshalPayload(envelope.Payload)
		if err != nil {
			return "", nil, fmt.Errorf("marshal payload for fact %q: %w", envelope.FactID, err)
		}

		observedAt := envelope.ObservedAt.UTC()

		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * columnsPerFactRow
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d::jsonb)",
			offset+1, offset+2, offset+3, offset+4, offset+5,
			offset+6, offset+7, offset+8, offset+9, offset+10,
			offset+11, offset+12, offset+13, offset+14, offset+15,
			offset+16, offset+17,
		)

		args = append(
			args,
			envelope.FactID,
			envelope.ScopeID,
			envelope.GenerationID,
			envelope.FactKind,
			envelope.StableFactKey,
			emptyToDefault(envelope.SchemaVersion, "0.0.0"),
			emptyToDefault(envelope.CollectorKind, emptyToDefault(envelope.SourceRef.SourceSystem, "unknown")),
			envelope.FencingToken,
			emptyToDefault(envelope.SourceConfidence, "unknown"),
			envelope.SourceRef.SourceSystem,
			envelope.SourceRef.FactKey,
			emptyToNil(envelope.SourceRef.SourceURI),
			emptyToNil(envelope.SourceRef.SourceRecordID),
			observedAt,
			observedAt,
			envelope.IsTombstone,
			payloadJSON,
		)
	}

	return upsertFactBatchPrefix + values.String() + suffix, args, nil
}

const upsertFactBatchPrefix = `INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, fencing_token, source_confidence,
    source_system, source_fact_key, source_uri, source_record_id,
    observed_at, ingested_at, is_tombstone, payload
) VALUES `

const upsertFactBatchSuffix = `
ON CONFLICT (fact_id) DO UPDATE SET
    fact_kind = EXCLUDED.fact_kind,
    stable_fact_key = EXCLUDED.stable_fact_key,
    schema_version = EXCLUDED.schema_version,
    collector_kind = EXCLUDED.collector_kind,
    fencing_token = EXCLUDED.fencing_token,
    source_confidence = EXCLUDED.source_confidence,
    source_system = EXCLUDED.source_system,
    source_fact_key = EXCLUDED.source_fact_key,
    source_uri = EXCLUDED.source_uri,
    source_record_id = EXCLUDED.source_record_id,
    observed_at = EXCLUDED.observed_at,
    ingested_at = EXCLUDED.ingested_at,
    is_tombstone = EXCLUDED.is_tombstone,
    payload = EXCLUDED.payload
WHERE fact_records.fencing_token <= EXCLUDED.fencing_token
`

// upsertFactBatchSuffixReturningFactID is upsertFactBatchSuffix with a
// RETURNING clause appended. Postgres only reports RETURNING rows for the
// subset of a multi-row INSERT ... ON CONFLICT DO UPDATE statement whose
// fencing_token guard actually passed (a fresh INSERT always returns its
// row; a DO UPDATE whose WHERE predicate is false is skipped for that row
// and returns nothing for it). This is the only atomic way to learn which
// fact_ids actually won the fencing race — reading fencing_token separately
// before or after the write would race against concurrent batches (issue
// #4444 review, codex P1). Kept as a distinct constant (not the default
// shape) so the hot, heavily-tested non-streaming upsertFactBatch path
// (upsertFacts, which has no downstream derived-evidence consumer) keeps its
// existing plain ExecContext call and query-shape test contracts unchanged;
// only upsertStreamingFacts, which does derive downstream evidence, needs the
// accepted-row identity.
const upsertFactBatchSuffixReturningFactID = upsertFactBatchSuffix + `
RETURNING fact_id
`

// upsertStreamingFacts reads fact envelopes from a channel and persists them
// in batched multi-row INSERT statements. Each batch inserts up to
// factBatchSize rows (500), matching the non-streaming path. Envelopes are
// deduplicated within each batch to avoid Postgres SQLSTATE 21000 on
// ON CONFLICT DO UPDATE. Cross-batch duplicates are resolved by fencing token,
// not commit order: upsertFactBatchSuffix's
// "WHERE fact_records.fencing_token <= EXCLUDED.fencing_token" guard (issue
// #4444) makes the higher-fencing-token fact win regardless of which batch's
// INSERT commits last, so an out-of-order or retried stale batch cannot
// resurrect a fact a newer batch already superseded.
//
// The write itself uses upsertFactBatchReturningAccepted, not upsertFactBatch:
// afterBatch derives repository-catalog and relationship-evidence side
// effects from the batch (see IngestionStore.commitScopeGeneration's afterBatch
// closure in ingestion.go), so this function MUST NOT hand it envelopes whose
// fact_records write was fenced out — that would let a stale batch's payload
// leak into derived graph truth even though its own row was correctly
// protected (issue #4444 review, codex P1). filterAcceptedEnvelopes narrows
// each batch to the accepted subset before every afterBatch call.
//
// Per-envelope validation ensures scope_id and generation_id match the
// expected values, replacing the upfront validateProjectionInput check
// that the slice-based path used.
//
// After each batch is committed, the batch slice is zeroed so Payload maps
// (which contain content_body strings — raw file source) become GC-eligible
// immediately rather than after the entire generation commits.
func upsertStreamingFacts(
	ctx context.Context,
	db ExecQueryer,
	factStream <-chan facts.Envelope,
	scopeID string,
	generationID string,
	afterBatch func([]facts.Envelope) error,
) (factUpsertStats, error) {
	var stats factUpsertStats
	if db == nil {
		return stats, fmt.Errorf("fact store database is required")
	}
	if factStream == nil {
		return stats, nil
	}

	batch := make([]facts.Envelope, 0, factBatchSize)

	for envelope := range factStream {
		if envelope.ScopeID != scopeID {
			return stats, fmt.Errorf(
				"fact %q scope_id %q does not match scope %q",
				envelope.FactID, envelope.ScopeID, scopeID,
			)
		}
		if envelope.GenerationID != generationID {
			return stats, fmt.Errorf(
				"fact %q generation_id %q does not match generation %q",
				envelope.FactID, envelope.GenerationID, generationID,
			)
		}

		batch = append(batch, envelope)

		if len(batch) >= factBatchSize {
			batch = deduplicateEnvelopes(batch)
			accepted, err := upsertFactBatchReturningAccepted(ctx, db, batch)
			if err != nil {
				return stats, err
			}
			stats.Rows += len(batch)
			stats.Batches++
			if afterBatch != nil {
				if err := afterBatch(filterAcceptedEnvelopes(batch, accepted)); err != nil {
					return stats, err
				}
			}
			for i := range batch {
				batch[i] = facts.Envelope{}
			}
			batch = batch[:0]
		}
	}

	// Flush remaining
	if len(batch) > 0 {
		batch = deduplicateEnvelopes(batch)
		accepted, err := upsertFactBatchReturningAccepted(ctx, db, batch)
		if err != nil {
			return stats, err
		}
		stats.Rows += len(batch)
		stats.Batches++
		if afterBatch != nil {
			if err := afterBatch(filterAcceptedEnvelopes(batch, accepted)); err != nil {
				return stats, err
			}
		}
	}

	return stats, nil
}

// filterAcceptedEnvelopes returns the subset of batch whose fact_id is in
// accepted, preserving batch order. A fact_id fenced out by
// upsertFactBatchSuffixReturningFactID's WHERE guard is absent from accepted
// and therefore contributes nothing to afterBatch's downstream repository
// catalog and relationship-evidence derivation (issue #4444 review, codex
// P1). Without this filter, a stale batch that lost the fact_records write
// race would still leak its stale payload into that derived evidence.
//
// Callers MUST pass a batch that already went through deduplicateEnvelopes
// (both upsertStreamingFacts call sites do), so batch has no duplicate
// fact_id values and the len(accepted) == len(batch) fast path below is a
// valid "every row accepted" check rather than an undercount from collapsed
// duplicates.
func filterAcceptedEnvelopes(batch []facts.Envelope, accepted map[string]struct{}) []facts.Envelope {
	if len(accepted) == len(batch) {
		return batch // common case: every row in the batch was accepted
	}
	if len(accepted) == 0 {
		return nil
	}
	filtered := make([]facts.Envelope, 0, len(accepted))
	for _, envelope := range batch {
		if _, ok := accepted[envelope.FactID]; ok {
			filtered = append(filtered, envelope)
		}
	}
	return filtered
}
