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

// deduplicateEnvelopes removes duplicate fact_ids, keeping the last occurrence.
// This preserves the overwrite semantics of the old N+1 INSERT pattern.
//
// Keeping the last array position — rather than the highest FencingToken — is
// deliberately safe against the within-batch analogue of the issue #4444
// cross-batch fencing race, and that safety is an invariant of how fact_ids
// and fencing tokens are produced, not a coincidence:
//
//   - fact_id is a deterministic hash that always folds in scope_id and
//     generation_id. Every collector's fact-id helper takes
//     (factKind, stableKey, scopeID, generationID) — see git factEnvelope
//     (collector/git_followup_facts.go) and awsFactID
//     (collector/awscloud/envelope.go). Two envelopes that share a fact_id
//     therefore share the same (scope_id, generation_id).
//   - FencingToken is a property of the workflow claim, which is 1:1 with a
//     (scope, generation) acquisition, and is stamped uniformly onto every
//     envelope of that generation
//     (boundary.FencingToken == claim.FencingToken == item.CurrentFencingToken;
//     see collector/awscloud/checkpoint/types.go and awsruntime/source.go). A
//     stale reclaim receives a *higher* token, so tokens diverge only ACROSS
//     generations, never within one.
//
// Consequently every duplicate fact_id inside a single flushed batch carries
// the identical FencingToken, so last-position and highest-token select the
// same envelope. The only production caller, upsertStreamingFacts, additionally
// rejects any envelope whose generation_id differs from the batch's generation,
// so a batch can never mix generations (and thus never mix tokens). The
// descending-token-within-one-batch case that would make last-position wrong is
// therefore unconstructible today. If a future collector ever assigns per-fact
// fencing tokens within one generation, prefer the higher token here (falling
// back to last position on ties to preserve the common zero/equal-token case)
// and add a regression test.
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
