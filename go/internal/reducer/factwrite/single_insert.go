// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factwrite

// SingleInsertQuery is the canonical single-row upsert every reducer-owned
// fact writer uses to persist one durable fact into fact_records, keyed by
// fact_id so reducer retries and duplicate facts converge on one row. It is
// shared across every reducer family writer (workload identity, cloud asset
// resolution, secrets/IAM trust chain, platform materialization, Kubernetes
// correlation, observability coverage correlation, and others) so a schema
// change to the canonical fact-write shape only needs one edit.
const SingleInsertQuery = `
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
