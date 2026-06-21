package reducer

import (
	"context"
	"fmt"
	"time"
)

// reducerFactBatchInsertQuery is a writer-local bulk insert that sends N fact
// rows in a single round-trip using unnest. It is intentionally separate from
// canonicalReducerFactInsertQuery (the shared single-row statement) so the many
// per-row callers of that query are not disturbed; the column list, conflict
// key, and ON CONFLICT update set are kept byte-for-byte equivalent so a batched
// writer produces the same fact rows a per-row loop would.
const reducerFactBatchInsertQuery = `
INSERT INTO fact_records (
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    collector_kind,
    source_confidence,
    source_system,
    source_fact_key,
    source_uri,
    source_record_id,
    observed_at,
    ingested_at,
    is_tombstone,
    payload
)
SELECT
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    collector_kind,
    source_confidence,
    source_system,
    source_fact_key,
    source_uri,
    source_record_id,
    observed_at,
    ingested_at,
    is_tombstone,
    payload::jsonb
FROM unnest(
    $1::text[],
    $2::text[],
    $3::text[],
    $4::text[],
    $5::text[],
    $6::text[],
    $7::text[],
    $8::text[],
    $9::text[],
    $10::text[],
    $11::text[],
    $12::timestamptz[],
    $13::timestamptz[],
    $14::bool[],
    $15::text[]
) AS t(
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    collector_kind,
    source_confidence,
    source_system,
    source_fact_key,
    source_uri,
    source_record_id,
    observed_at,
    ingested_at,
    is_tombstone,
    payload
)
ON CONFLICT (fact_id) DO UPDATE SET
    fact_kind         = EXCLUDED.fact_kind,
    stable_fact_key   = EXCLUDED.stable_fact_key,
    collector_kind    = EXCLUDED.collector_kind,
    source_confidence = EXCLUDED.source_confidence,
    source_system     = EXCLUDED.source_system,
    source_fact_key   = EXCLUDED.source_fact_key,
    source_uri        = EXCLUDED.source_uri,
    source_record_id  = EXCLUDED.source_record_id,
    observed_at       = EXCLUDED.observed_at,
    ingested_at       = EXCLUDED.ingested_at,
    is_tombstone      = EXCLUDED.is_tombstone,
    payload           = EXCLUDED.payload
`

// reducerFactBatchSize bounds how many fact rows are sent per unnest statement.
// fact_records has 15 columns, so each row consumes 15 bind parameters; 1000
// rows uses 15000 parameters, well under Postgres' 65535 parameter ceiling
// while keeping each statement large enough to amortise round-trip cost. The
// bound also caps per-statement memory and lock footprint for a single scope.
const reducerFactBatchSize = 1000

// reducerFactRow is one canonical fact-record row for a batched insert. The
// field order and types mirror the positional arguments of
// canonicalReducerFactInsertQuery so a batched writer is a drop-in replacement
// for the per-row loop it replaces.
type reducerFactRow struct {
	FactID           string
	ScopeID          string
	GenerationID     string
	FactKind         string
	StableFactKey    string
	CollectorKind    string
	SourceConfidence string
	SourceSystem     string
	SourceFactKey    string
	SourceURI        *string
	SourceRecordID   *string
	ObservedAt       time.Time
	IngestedAt       time.Time
	IsTombstone      bool
	Payload          string
}

// reducerBatchInsertFacts upserts rows in bounded chunks of reducerFactBatchSize
// using reducerFactBatchInsertQuery. It issues ceil(len(rows)/batchSize)
// ExecContext calls instead of one per row, so a scope with N rows costs
// O(N/batchSize) round-trips rather than O(N). Each chunk is a single statement;
// callers that need all chunks committed atomically must pass a transaction as
// db. An empty rows slice issues no statements.
func reducerBatchInsertFacts(
	ctx context.Context,
	db workloadIdentityExecer,
	rows []reducerFactRow,
) error {
	for start := 0; start < len(rows); start += reducerFactBatchSize {
		end := start + reducerFactBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := execReducerFactChunk(ctx, db, rows[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// execReducerFactChunk sends one bounded chunk as a single unnest statement.
func execReducerFactChunk(
	ctx context.Context,
	db workloadIdentityExecer,
	chunk []reducerFactRow,
) error {
	n := len(chunk)
	factIDs := make([]string, n)
	scopeIDs := make([]string, n)
	generationIDs := make([]string, n)
	factKinds := make([]string, n)
	stableKeys := make([]string, n)
	collectorKinds := make([]string, n)
	sourceConfidences := make([]string, n)
	sourceSystems := make([]string, n)
	sourceFactKeys := make([]string, n)
	sourceURIs := make([]*string, n)
	sourceRecordIDs := make([]*string, n)
	observedAts := make([]time.Time, n)
	ingestedAts := make([]time.Time, n)
	isTombstones := make([]bool, n)
	payloads := make([]string, n)

	for i, row := range chunk {
		factIDs[i] = row.FactID
		scopeIDs[i] = row.ScopeID
		generationIDs[i] = row.GenerationID
		factKinds[i] = row.FactKind
		stableKeys[i] = row.StableFactKey
		collectorKinds[i] = row.CollectorKind
		sourceConfidences[i] = row.SourceConfidence
		sourceSystems[i] = row.SourceSystem
		sourceFactKeys[i] = row.SourceFactKey
		sourceURIs[i] = row.SourceURI
		sourceRecordIDs[i] = row.SourceRecordID
		observedAts[i] = row.ObservedAt
		ingestedAts[i] = row.IngestedAt
		isTombstones[i] = row.IsTombstone
		payloads[i] = row.Payload
	}

	if _, err := db.ExecContext(
		ctx,
		reducerFactBatchInsertQuery,
		factIDs,
		scopeIDs,
		generationIDs,
		factKinds,
		stableKeys,
		collectorKinds,
		sourceConfidences,
		sourceSystems,
		sourceFactKeys,
		sourceURIs,
		sourceRecordIDs,
		observedAts,
		ingestedAts,
		isTombstones,
		payloads,
	); err != nil {
		return fmt.Errorf("batch insert reducer facts: %w", err)
	}
	return nil
}
