// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factwrite

// CanonicalInsertQuery is the shared single-row upsert every reducer-owned
// fact writer uses when it publishes one fact at a time, one ExecContext per
// fact. It binds the same fifteen fact_records columns, in the same order, that
// [BatchInsertQuery] binds first, and its ON CONFLICT (fact_id) DO UPDATE is
// what makes replaying a generation idempotent: a re-run rewrites the row
// rather than inserting a duplicate.
//
// It is NOT interchangeable with the batch statement. [BatchInsertQuery] binds
// a sixteenth column, fencing_token, and guards its conflict update with
// `WHERE fact_records.fencing_token <= EXCLUDED.fencing_token`, so a
// late-arriving older write there cannot clobber a newer one. This statement
// carries no fencing token and updates unconditionally, so last writer wins.
// A writer that needs fencing, or the schema_version column, must use the
// batch or versioned batch statement instead.
//
// The $15::jsonb cast is load-bearing: payload is bound as marshalled JSON
// text, and the cast is what lands it in the jsonb column.
const CanonicalInsertQuery = `
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
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb
)
ON CONFLICT (fact_id) DO UPDATE SET
    fact_kind = EXCLUDED.fact_kind,
    stable_fact_key = EXCLUDED.stable_fact_key,
    collector_kind = EXCLUDED.collector_kind,
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
