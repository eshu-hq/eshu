// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
	database      db.ExecQueryer
	identityCache *IdentityEpochCache
	instruments   *telemetry.Instruments
	logger        *slog.Logger
}

// NewFactStore constructs a Postgres-backed fact store without identity caching.
// Use NewFactStoreWithIdentityCache to enable the identity-fact epoch cache.
func NewFactStore(database db.ExecQueryer) *FactStore {
	return &FactStore{database: database}
}

// NewFactStoreWithIdentityCache constructs a Postgres-backed fact store with
// the identity-fact epoch cache enabled.
func NewFactStoreWithIdentityCache(database db.ExecQueryer, cache *IdentityEpochCache) *FactStore {
	return &FactStore{database: database, identityCache: cache}
}

// WithInstruments returns a copy that records manifest tag outcomes on the
// active code-call-symbol loads.
func (s FactStore) WithInstruments(instruments *telemetry.Instruments) *FactStore {
	s.instruments = instruments
	return &s
}

// CountFacts returns the number of facts for a scope generation without loading them.
//
// For a delta generation with a projected base it returns the union upper
// bound (base count plus delta count): the projection loads the overlaid set,
// so the large-generation semaphore must classify on the union, not on the
// delta alone. A delta whose base is missing returns the refusal error so the
// caller fails closed instead of classifying a partial load as small.
func (s FactStore) CountFacts(ctx context.Context, scopeID, generationID string) (int, error) {
	if s.database == nil {
		return 0, fmt.Errorf("fact store database is required")
	}
	baseGenerationID, _, wantUnion, err := s.resolveDeltaUnionBase(ctx, scopeID, generationID)
	if err != nil {
		return 0, err
	}
	if wantUnion {
		baseCount, err := s.countFactsGeneration(ctx, scopeID, baseGenerationID)
		if err != nil {
			return 0, err
		}
		deltaCount, err := s.countFactsGeneration(ctx, scopeID, generationID)
		if err != nil {
			return 0, err
		}
		return baseCount + deltaCount, nil
	}
	return s.countFactsGeneration(ctx, scopeID, generationID)
}

// countFactsGeneration runs countFactsQuery for one (scope, generation).
func (s FactStore) countFactsGeneration(ctx context.Context, scopeID, generationID string) (int, error) {
	rows, err := s.database.QueryContext(ctx, countFactsQuery, scopeID, generationID)
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
	return upsertFacts(ctx, s.database, envelopes)
}

// LoadFacts satisfies the projector fact-store contract.
func (s FactStore) LoadFacts(
	ctx context.Context,
	work projector.ScopeGenerationWork,
) ([]facts.Envelope, error) {
	return s.ListFacts(ctx, work.Scope.ScopeID, work.Generation.GenerationID)
}

// ListFacts loads fact envelopes for one scope generation.
//
// When the generation is a delta with a projected base full generation at
// its baseline commit, the load returns the delta-base union (#7797): the
// delta's facts overlaid on the base generation's facts, so a from-empty
// projection of the delta writes the full repository tree. A delta whose
// base is missing fails closed with projector.DeltaBaseMissingError and
// loads nothing. The kind-filtered ListFactsByKind paths keep
// single-generation behavior: reducer handlers are delta-aware by design.
func (s FactStore) ListFacts(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	loaded, err := s.listFactsGeneration(ctx, scopeID, generationID)
	if err != nil {
		return nil, err
	}
	baseGenerationID, baselineSHA, wantUnion, err := s.resolveDeltaUnionBase(ctx, scopeID, generationID)
	if err != nil {
		// Only a missing base refuses downstream: any other resolve
		// error retries through the generic fail path, so it counts a
		// distinct resolve-failed outcome instead of the refused one.
		var baseMissing *projector.DeltaBaseMissingError
		if errors.As(err, &baseMissing) {
			s.recordDeltaUnionRefused(ctx, scopeID, generationID, len(loaded))
		} else {
			s.recordDeltaUnion(ctx, scopeID, generationID, projector.DeltaUnionResolveFailed, 0, len(loaded), 0)
		}
		return nil, err
	}
	if !wantUnion {
		return loaded, nil
	}
	return s.listFactsUnion(ctx, scopeID, generationID, baseGenerationID, baselineSHA, loaded)
}

// backfill.ScanFactEnvelope lives in the backfill package as ScanFactEnvelope: the
// deferred-backfill fact loaders are its primary consumers and the root fact
// families share it through that seam (issue #7794).

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
func upsertFacts(ctx context.Context, database db.ExecQueryer, envelopes []facts.Envelope) error {
	if database == nil {
		return fmt.Errorf("fact store database is required")
	}

	envelopes = deduplicateEnvelopes(envelopes)

	for i := 0; i < len(envelopes); i += factBatchSize {
		end := i + factBatchSize
		if end > len(envelopes) {
			end = len(envelopes)
		}
		if err := upsertFactBatch(ctx, database, envelopes[i:end]); err != nil {
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
	if !schemaVersionPattern.MatchString(payloadstore.EmptyToDefault(envelope.SchemaVersion, "0.0.0")) {
		return fmt.Errorf("fact %q schema_version must be semantic version", envelope.FactID)
	}
	if envelope.SourceConfidence != "" {
		if err := facts.ValidateSourceConfidence(envelope.SourceConfidence); err != nil {
			return fmt.Errorf("fact %q source_confidence: %w", envelope.FactID, err)
		}
	}

	return nil
}
