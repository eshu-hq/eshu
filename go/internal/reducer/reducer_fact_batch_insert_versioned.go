// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"time"
)

// reducerFactBatchInsertVersionedQuery is the schema_version-carrying sibling
// of reducerFactBatchInsertQuery.
//
// It does NOT carry fencing_token: no governed writer on this path has a fenced
// generation-authoritative retire yet. A governed domain that grows one must add
// the column here the same way reducerFactBatchInsertQuery carries it, or its
// freshly inserted rows will sit at the table default 0 between the insert and
// the retire's stamp and a stalled worker will delete them — see the
// "Why fencing_token is written here" section on reducerFactBatchInsertQuery.
//
// Otherwise it is byte-equivalent, column-for-column and
// conflict-for-conflict, to the versioned single-row upsert every governed
// writer used before issue #5317 (the retired canonicalVersionedReducerFact
// InsertQuery formerly in workload_identity_writer.go, removed once its last
// caller migrated onto this batched path) the same way reducerFactBatchInsertQuery
// mirrors canonicalReducerFactInsertQuery: a writer that publishes a governed
// reducer-derived fact (schema_version set explicitly, e.g.
// facts.ReducerDerivedSchemaVersionV1) MUST use this variant, not
// reducerFactBatchInsertQuery — the unversioned query omits the schema_version
// column entirely, so the table DEFAULT '0.0.0' would silently replace the
// governed version on every insert and would leave an existing row's
// schema_version untouched (not reset to the default) on conflict, which is
// not byte-identical to the per-row loop it replaces.
const reducerFactBatchInsertVersionedQuery = `
INSERT INTO fact_records (
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    schema_version,
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
    schema_version,
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
    $12::text[],
    $13::timestamptz[],
    $14::timestamptz[],
    $15::bool[],
    $16::text[]
) AS t(
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    schema_version,
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
    schema_version    = EXCLUDED.schema_version,
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

// reducerFactVersionedRow is one canonical fact-record row for a batched
// insert of a governed reducer-derived fact. It mirrors reducerFactRow with an
// added SchemaVersion field, matching the positional arguments of the retired
// versioned single-row upsert so a batched writer is a drop-in replacement for
// the per-row loop it replaces.
type reducerFactVersionedRow struct {
	FactID           string
	ScopeID          string
	GenerationID     string
	FactKind         string
	StableFactKey    string
	SchemaVersion    string
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

// reducerBatchInsertVersionedFacts upserts governed reducer-derived fact rows
// in bounded chunks of reducerFactBatchSize using
// reducerFactBatchInsertVersionedQuery. It issues ceil(len(rows)/batchSize)
// ExecContext calls instead of one per row, so a scope with N rows costs
// O(N/batchSize) round-trips rather than O(N). Each chunk is a single
// statement; callers that need all chunks committed atomically must pass a
// transaction as db. An empty rows slice issues no statements.
func reducerBatchInsertVersionedFacts(
	ctx context.Context,
	db workloadIdentityExecer,
	rows []reducerFactVersionedRow,
) error {
	rows = dedupeReducerFactRowsByFactID(rows, func(r reducerFactVersionedRow) string { return r.FactID })
	for start := 0; start < len(rows); start += reducerFactBatchSize {
		end := start + reducerFactBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := execReducerFactVersionedChunk(ctx, db, rows[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// execReducerFactVersionedChunk sends one bounded chunk as a single unnest
// statement.
func execReducerFactVersionedChunk(
	ctx context.Context,
	db workloadIdentityExecer,
	chunk []reducerFactVersionedRow,
) error {
	n := len(chunk)
	factIDs := make([]string, n)
	scopeIDs := make([]string, n)
	generationIDs := make([]string, n)
	factKinds := make([]string, n)
	stableKeys := make([]string, n)
	schemaVersions := make([]string, n)
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
		schemaVersions[i] = row.SchemaVersion
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
		reducerFactBatchInsertVersionedQuery,
		factIDs,
		scopeIDs,
		generationIDs,
		factKinds,
		stableKeys,
		schemaVersions,
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
		return fmt.Errorf("batch insert versioned reducer facts: %w", err)
	}
	return nil
}
