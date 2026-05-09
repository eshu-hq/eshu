package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

const (
	// factBatchSize is the number of rows per multi-row INSERT batch.
	// 500 rows * 17 columns = 8500 parameters per query, well under the
	// Postgres limit of 65535. This reduces 91k facts from 91k round trips
	// to ~184 queries.
	factBatchSize = 500

	// columnsPerFactRow is the number of columns in the fact_records INSERT.
	columnsPerFactRow = 17
)

// factUpsertStats summarizes one streaming fact persistence pass without
// retaining the full generation in memory.
type factUpsertStats struct {
	Rows    int
	Batches int
}

const countFactsQuery = `SELECT COUNT(*) FROM fact_records WHERE scope_id = $1 AND generation_id = $2`

const listFactsQuery = `
SELECT
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    schema_version,
    collector_kind,
    fencing_token,
    source_confidence,
    source_system,
    source_fact_key,
    COALESCE(source_uri, ''),
    COALESCE(source_record_id, ''),
    observed_at,
    is_tombstone,
    payload
FROM fact_records
WHERE scope_id = $1
  AND generation_id = $2
ORDER BY observed_at ASC, fact_id ASC
`

// FactStore persists and loads fact records from Postgres.
type FactStore struct {
	db ExecQueryer
}

// NewFactStore constructs a Postgres-backed fact store.
func NewFactStore(db ExecQueryer) FactStore {
	return FactStore{db: db}
}

// CountFacts returns the number of facts for a scope generation without loading them.
func (s FactStore) CountFacts(ctx context.Context, scopeID, generationID string) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("fact store database is required")
	}
	rows, err := s.db.QueryContext(ctx, countFactsQuery, scopeID, generationID)
	if err != nil {
		return 0, fmt.Errorf("count facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var count int
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("count facts scan: %w", err)
		}
	}
	return count, rows.Err()
}

// UpsertFacts persists fact envelopes into fact_records.
func (s FactStore) UpsertFacts(ctx context.Context, envelopes []facts.Envelope) error {
	return upsertFacts(ctx, s.db, envelopes)
}

// LoadFacts satisfies the projector fact-store contract.
func (s FactStore) LoadFacts(
	ctx context.Context,
	work projector.ScopeGenerationWork,
) ([]facts.Envelope, error) {
	return s.ListFacts(ctx, work.Scope.ScopeID, work.Generation.GenerationID)
}

// ListFacts loads fact envelopes for one scope generation.
func (s FactStore) ListFacts(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	rows, err := s.db.QueryContext(ctx, listFactsQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var loaded []facts.Envelope
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list facts: %w", err)
	}

	return loaded, nil
}

func scanFactEnvelope(rows Rows) (facts.Envelope, error) {
	var factID string
	var scopeID string
	var generationID string
	var factKind string
	var stableFactKey string
	var schemaVersion string
	var collectorKind string
	var fencingToken int64
	var sourceConfidence string
	var sourceSystem string
	var sourceFactKey string
	var sourceURI string
	var sourceRecordID string
	var observedAt time.Time
	var isTombstone bool
	var rawPayload []byte

	if err := rows.Scan(
		&factID,
		&scopeID,
		&generationID,
		&factKind,
		&stableFactKey,
		&schemaVersion,
		&collectorKind,
		&fencingToken,
		&sourceConfidence,
		&sourceSystem,
		&sourceFactKey,
		&sourceURI,
		&sourceRecordID,
		&observedAt,
		&isTombstone,
		&rawPayload,
	); err != nil {
		return facts.Envelope{}, err
	}

	payload, err := unmarshalPayload(rawPayload)
	if err != nil {
		return facts.Envelope{}, err
	}

	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         factKind,
		StableFactKey:    stableFactKey,
		SchemaVersion:    schemaVersion,
		CollectorKind:    collectorKind,
		FencingToken:     fencingToken,
		SourceConfidence: sourceConfidence,
		ObservedAt:       observedAt.UTC(),
		Payload:          payload,
		IsTombstone:      isTombstone,
		SourceRef: facts.Ref{
			SourceSystem:   sourceSystem,
			ScopeID:        scopeID,
			GenerationID:   generationID,
			FactKey:        sourceFactKey,
			SourceURI:      sourceURI,
			SourceRecordID: sourceRecordID,
		},
	}, nil
}

// upsertFacts persists fact envelopes using batched multi-row INSERT statements.
// Each batch inserts up to factBatchSize rows in a single query, reducing
// 91k facts from 91k round trips to ~184 queries. This is critical for memory
// because a slow consumer causes streaming workers to pile up generations.
//
// Envelopes are deduplicated by fact_id before batching because Postgres
// rejects ON CONFLICT DO UPDATE when the same key appears twice in a single
// multi-row INSERT. The last occurrence of each fact_id wins, matching the
// overwrite semantics of the old N+1 pattern.
func upsertFacts(ctx context.Context, db ExecQueryer, envelopes []facts.Envelope) error {
	if db == nil {
		return fmt.Errorf("fact store database is required")
	}

	envelopes = deduplicateEnvelopes(envelopes)

	for i := 0; i < len(envelopes); i += factBatchSize {
		end := i + factBatchSize
		if end > len(envelopes) {
			end = len(envelopes)
		}
		if err := upsertFactBatch(ctx, db, envelopes[i:end]); err != nil {
			return err
		}
	}

	return nil
}

// upsertFactBatch inserts one batch of facts using a multi-row INSERT query.
func upsertFactBatch(ctx context.Context, db ExecQueryer, batch []facts.Envelope) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*columnsPerFactRow)
	var values strings.Builder

	for i, envelope := range batch {
		if err := validateFactEnvelope(envelope); err != nil {
			return err
		}

		payloadJSON, err := marshalPayload(envelope.Payload)
		if err != nil {
			return fmt.Errorf("marshal payload for fact %q: %w", envelope.FactID, err)
		}

		observedAt := envelope.ObservedAt.UTC()

		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * columnsPerFactRow
		fmt.Fprintf(&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d::jsonb)",
			offset+1, offset+2, offset+3, offset+4, offset+5,
			offset+6, offset+7, offset+8, offset+9, offset+10,
			offset+11, offset+12, offset+13, offset+14, offset+15,
			offset+16, offset+17,
		)

		args = append(args,
			envelope.FactID,
			envelope.ScopeID,
			envelope.GenerationID,
			envelope.FactKind,
			envelope.StableFactKey,
			emptyToDefault(envelope.SchemaVersion, "unknown.v1"),
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

	query := upsertFactBatchPrefix + values.String() + upsertFactBatchSuffix

	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert fact batch (%d facts): %w", len(batch), err)
	}

	return nil
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
`

// upsertStreamingFacts reads fact envelopes from a channel and persists them
// in batched multi-row INSERT statements. Each batch inserts up to
// factBatchSize rows (500), matching the non-streaming path. Envelopes are
// deduplicated within each batch to avoid Postgres SQLSTATE 21000 on
// ON CONFLICT DO UPDATE; cross-batch duplicates are handled by Postgres
// naturally (later batch overwrites earlier).
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
			if err := upsertFactBatch(ctx, db, batch); err != nil {
				return stats, err
			}
			stats.Rows += len(batch)
			stats.Batches++
			if afterBatch != nil {
				if err := afterBatch(batch); err != nil {
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
		if err := upsertFactBatch(ctx, db, batch); err != nil {
			return stats, err
		}
		stats.Rows += len(batch)
		stats.Batches++
		if afterBatch != nil {
			if err := afterBatch(batch); err != nil {
				return stats, err
			}
		}
	}

	return stats, nil
}

// deduplicateEnvelopes removes duplicate fact_ids, keeping the last occurrence.
// This preserves the overwrite semantics of the old N+1 INSERT pattern.
func deduplicateEnvelopes(envelopes []facts.Envelope) []facts.Envelope {
	if len(envelopes) == 0 {
		return envelopes
	}
	seen := make(map[string]int, len(envelopes))
	for i, e := range envelopes {
		seen[e.FactID] = i
	}
	if len(seen) == len(envelopes) {
		return envelopes // no duplicates
	}
	deduped := make([]facts.Envelope, 0, len(seen))
	for i, e := range envelopes {
		if seen[e.FactID] == i {
			deduped = append(deduped, e)
		}
	}
	return deduped
}

func validateFactEnvelope(envelope facts.Envelope) error {
	observedAt := envelope.ObservedAt.UTC()
	if observedAt.IsZero() {
		return fmt.Errorf("fact %q observed_at must not be zero", envelope.FactID)
	}

	return nil
}
