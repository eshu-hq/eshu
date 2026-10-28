// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factkeys

// OrphanCensusSQL is the operator census for the three key sidecars once
// their per-fact foreign keys are gone (#7420, migrations 188 to 190). It
// returns one row (table_name, scope_id, generation_id, orphan_rows) for
// every generation that holds key rows with no matching fact, ordered by
// those three columns, and no rows on a clean database. A key row is an
// orphan when no fact_records row has its (fact_id, scope_id,
// generation_id), the same triple every key reader joins on, so the census
// counts exactly the rows the readers can never see.
//
// Retention deletes key rows explicitly before their facts, so the census
// reads zero unless a fact delete bypassed facts/keys. A nonzero row names
// the scope and generation to inspect. Readers never see the rows, so they
// are a storage leak, not a wrong answer; remove them with OrphanCleanupSQL,
// never with the whole-generation statements in Deletes, which would also
// remove the live keys of a live generation. The census must read zero
// before the foreign keys are put back
// (go/internal/storage/postgres/testdata/7420-drop-key-sidecar-fks-reverse.sql).
//
// It is read-only and takes ACCESS SHARE only. On the #7420 fixture (3M
// facts, 3.75M key rows) it ran in 1.5 s as one hash anti-join per table
// over parallel sequential scans of fact_records, so its cost grows with
// fact_records; run it off-peak or on a replica at production scale.
const OrphanCensusSQL = `SELECT 'relationship_reference_candidate_keys' AS table_name,
    keys.scope_id, keys.generation_id, count(*) AS orphan_rows
FROM relationship_reference_candidate_keys AS keys
WHERE NOT EXISTS (
    SELECT 1 FROM fact_records AS fact
    WHERE fact.fact_id = keys.fact_id AND fact.scope_id = keys.scope_id AND fact.generation_id = keys.generation_id
)
GROUP BY keys.scope_id, keys.generation_id
UNION ALL
SELECT 'package_manifest_consumption_keys', keys.scope_id, keys.generation_id, count(*)
FROM package_manifest_consumption_keys AS keys
WHERE NOT EXISTS (
    SELECT 1 FROM fact_records AS fact
    WHERE fact.fact_id = keys.fact_id AND fact.scope_id = keys.scope_id AND fact.generation_id = keys.generation_id
)
GROUP BY keys.scope_id, keys.generation_id
UNION ALL
SELECT 'package_registry_identity_keys', keys.scope_id, keys.generation_id, count(*)
FROM package_registry_identity_keys AS keys
WHERE NOT EXISTS (
    SELECT 1 FROM fact_records AS fact
    WHERE fact.fact_id = keys.fact_id AND fact.scope_id = keys.scope_id AND fact.generation_id = keys.generation_id
)
GROUP BY keys.scope_id, keys.generation_id
ORDER BY 1, 2, 3
`

// OrphanCleanupSQL deletes the orphan key rows of one generation that
// OrphanCensusSQL reported: $1 scope_id, $2 generation_id. It uses the
// census's (fact_id, scope_id, generation_id) anti-join, so it keeps every
// live key of the generation; a fact_id-only anti-join would miss a key that
// points at a fact of another generation. It names the tables in Deletes
// order and returns one row with the rows deleted per table. Run it per
// reported generation, then rerun the census.
const OrphanCleanupSQL = `WITH relationship AS (
    DELETE FROM relationship_reference_candidate_keys AS keys
    WHERE keys.scope_id = $1 AND keys.generation_id = $2
      AND NOT EXISTS (
          SELECT 1 FROM fact_records AS fact
          WHERE fact.fact_id = keys.fact_id AND fact.scope_id = keys.scope_id AND fact.generation_id = keys.generation_id
      )
    RETURNING 1
), manifest AS (
    DELETE FROM package_manifest_consumption_keys AS keys
    WHERE keys.scope_id = $1 AND keys.generation_id = $2
      AND NOT EXISTS (
          SELECT 1 FROM fact_records AS fact
          WHERE fact.fact_id = keys.fact_id AND fact.scope_id = keys.scope_id AND fact.generation_id = keys.generation_id
      )
    RETURNING 1
), registry AS (
    DELETE FROM package_registry_identity_keys AS keys
    WHERE keys.scope_id = $1 AND keys.generation_id = $2
      AND NOT EXISTS (
          SELECT 1 FROM fact_records AS fact
          WHERE fact.fact_id = keys.fact_id AND fact.scope_id = keys.scope_id AND fact.generation_id = keys.generation_id
      )
    RETURNING 1
)
SELECT (SELECT count(*) FROM relationship) AS relationship_reference_candidate_keys,
    (SELECT count(*) FROM manifest) AS package_manifest_consumption_keys,
    (SELECT count(*) FROM registry) AS package_registry_identity_keys
`
