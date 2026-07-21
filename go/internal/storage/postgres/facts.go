// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

var schemaVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

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
	db            ExecQueryer
	identityCache *identityEpochCache
}

// NewFactStore constructs a Postgres-backed fact store without identity caching.
// Use NewFactStoreWithIdentityCache to enable the identity-fact epoch cache.
func NewFactStore(db ExecQueryer) *FactStore {
	return &FactStore{db: db}
}

// NewFactStoreWithIdentityCache constructs a Postgres-backed fact store with
// the identity-fact epoch cache enabled.
func NewFactStoreWithIdentityCache(db ExecQueryer, cache *identityEpochCache) *FactStore {
	return &FactStore{db: db, identityCache: cache}
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
// multi-row INSERT. The highest fencing token wins; ties keep the last
// occurrence to preserve the zero/equal-token overwrite semantics of the old
// N+1 pattern.
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

// deduplicateEnvelopes removes duplicate fact_ids, keeping the envelope with
// the highest fencing token. When tokens tie, the last occurrence wins to
// preserve the overwrite semantics of the old N+1 INSERT pattern and the
// common zero-token collector path.
//
// Production batches normally carry one token for every envelope in a claim
// epoch; cross-batch ordering is still guarded in SQL by
// upsertFactBatchSuffix's fencing predicate. Comparing the token here closes
// the remaining in-memory case before a multi-row INSERT can collapse a newer
// duplicate out of the batch.
func deduplicateEnvelopes(envelopes []facts.Envelope) []facts.Envelope {
	if len(envelopes) == 0 {
		return envelopes
	}
	seen := make(map[string]int, len(envelopes))
	for i, e := range envelopes {
		if previous, ok := seen[e.FactID]; !ok || e.FencingToken >= envelopes[previous].FencingToken {
			seen[e.FactID] = i
		}
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
	if !schemaVersionPattern.MatchString(emptyToDefault(envelope.SchemaVersion, "0.0.0")) {
		return fmt.Errorf("fact %q schema_version must be semantic version", envelope.FactID)
	}
	if envelope.SourceConfidence != "" {
		if err := facts.ValidateSourceConfidence(envelope.SourceConfidence); err != nil {
			return fmt.Errorf("fact %q source_confidence: %w", envelope.FactID, err)
		}
	}

	return nil
}
