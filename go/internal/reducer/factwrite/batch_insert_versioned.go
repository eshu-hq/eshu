// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factwrite

import (
	"context"
	"fmt"
	"time"
)

// BatchInsertVersionedQuery is the schema_version-carrying sibling
// of BatchInsertQuery.
//
// It carries fencing_token (#5848) the same way BatchInsertQuery
// does for its unversioned callers: the bind ($17, appended last so the
// existing $1..$16 mapping stays untouched) AND the
// `fact_records.fencing_token <= EXCLUDED.fencing_token` conflict guard. Both
// are required together — without the bind every row rests at the table
// default 0 and the guard admits every pass unconditionally; without the
// guard, a stalled worker's upsert overwrites a fresher row's content and
// leaves the fresher token vouching for it. See "Why fencing_token is written
// here" and "Why the conflict clause is guarded rather than merged" on
// BatchInsertQuery for the full rationale; it applies unchanged
// here.
//
// aws_cloud_runtime_drift (#5848) is the first versioned writer to opt in.
// Its identity embeds finding_kind, so a reclassification (e.g.
// orphaned_cloud_resource -> image_version_drift as Terraform state activates
// late, #5837) mints a NEW fact_id rather than colliding on the old one — this
// guard alone does not protect against that shape; it only protects a retry or
// redelivery that DOES collide on the same fact_id. The insert-admission
// watermark in aws_cloud_runtime_drift_admission.go is what rejects a stale
// pass before it reaches this statement at all, and the generation-authoritative
// retire in aws_cloud_runtime_drift_writer_queries.go is what removes a
// superseded row under a DIFFERENT fact_id once a fresher pass lands. All three
// are required; this guard alone would not have closed #5848.
//
// A future versioned domain that grows the same need must add BOTH pieces the
// same way, not just the column here.
//
// Otherwise it is byte-equivalent, column-for-column and
// conflict-for-conflict, to the versioned single-row upsert every governed
// writer used before issue #5317 (the retired canonicalVersionedReducerFact
// InsertQuery formerly in workload_identity_writer.go, removed once its last
// caller migrated onto this batched path) the same way BatchInsertQuery
// mirrors canonicalReducerFactInsertQuery: a writer that publishes a governed
// reducer-derived fact (schema_version set explicitly, e.g.
// facts.ReducerDerivedSchemaVersionV1) MUST use this variant, not
// BatchInsertQuery — the unversioned query omits the schema_version
// column entirely, so the table DEFAULT '0.0.0' would silently replace the
// governed version on every insert and would leave an existing row's
// schema_version untouched (not reset to the default) on conflict, which is
// not byte-identical to the per-row loop it replaces.
const BatchInsertVersionedQuery = `
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
    payload,
    fencing_token
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
    payload::jsonb,
    fencing_token
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
    $16::text[],
    $17::bigint[]
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
    payload,
    fencing_token
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
    payload           = EXCLUDED.payload,
    fencing_token     = EXCLUDED.fencing_token
WHERE fact_records.fencing_token <= EXCLUDED.fencing_token
`

// VersionedRow is one canonical fact-record row for a batched
// insert of a governed reducer-derived fact. It mirrors Row with an
// added SchemaVersion field, matching the positional arguments of the retired
// versioned single-row upsert so a batched writer is a drop-in replacement for
// the per-row loop it replaces.
type VersionedRow struct {
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
	// FencingToken is the row's write-ordering watermark, carried on the INSERT
	// so the conflict guard has something to rank a colliding pass against.
	// Leave it zero unless the domain's intent can be replayed by two workers
	// holding different views of the evidence; see
	// BatchInsertVersionedQuery for why 0 is not a safe resting
	// state for one that can, and Row.FencingToken for the
	// unversioned sibling this mirrors.
	FencingToken int64
}

// BatchInsertVersionedFacts upserts governed reducer-derived fact rows
// in bounded chunks of BatchSize using
// BatchInsertVersionedQuery. It issues ceil(len(rows)/batchSize)
// ExecContext calls instead of one per row, so a scope with N rows costs
// O(N/batchSize) round-trips rather than O(N). Each chunk is a single
// statement; callers that need all chunks committed atomically must pass a
// transaction as db. An empty rows slice issues no statements.
func BatchInsertVersionedFacts(
	ctx context.Context,
	db Execer,
	rows []VersionedRow,
) error {
	rows = DedupeRowsByFactID(rows, func(r VersionedRow) string { return r.FactID })
	for start := 0; start < len(rows); start += BatchSize {
		end := start + BatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := execVersionedChunk(ctx, db, rows[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// execVersionedChunk sends one bounded chunk as a single unnest
// statement.
func execVersionedChunk(
	ctx context.Context,
	db Execer,
	chunk []VersionedRow,
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
	fencingTokens := make([]int64, n)

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
		fencingTokens[i] = row.FencingToken
	}

	if _, err := db.ExecContext(
		ctx,
		BatchInsertVersionedQuery,
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
		fencingTokens,
	); err != nil {
		return fmt.Errorf("batch insert versioned reducer facts: %w", err)
	}
	return nil
}
