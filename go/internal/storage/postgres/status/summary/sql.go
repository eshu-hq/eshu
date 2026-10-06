// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

// upsertSQL writes one model row. The WHERE clause is the as_of guard: the
// conflict branch updates only when the stored as_of is strictly older than
// the incoming one, so a slower, older pass can never overwrite a newer row and
// a replay with an equal as_of rewrites nothing. The statement affects one row
// when it inserts or advances the row and zero rows when the guard rejects it,
// which is how Upsert reports whether the row advanced. computed_at is the
// database clock at write time, not a caller value.
const upsertSQL = `
INSERT INTO status_summary_snapshots AS existing (
    model_key, schema_version, source_sha256, as_of,
    computed_at, pass_duration_ms, row_count, rows
) VALUES ($1, $2, $3, $4, clock_timestamp(), $5, $6, $7::jsonb)
ON CONFLICT (model_key) DO UPDATE
   SET schema_version   = EXCLUDED.schema_version,
       source_sha256    = EXCLUDED.source_sha256,
       as_of            = EXCLUDED.as_of,
       computed_at      = EXCLUDED.computed_at,
       pass_duration_ms = EXCLUDED.pass_duration_ms,
       row_count        = EXCLUDED.row_count,
       rows             = EXCLUDED.rows
 WHERE existing.as_of < EXCLUDED.as_of
`

// readSQL reads one model row by its primary key. It carries no as_of filter,
// ordering, or limit: the key alone selects the row. On a one-page table the
// planner may read the heap with a sequential scan instead of the primary key
// index, so the live proofs pin the relation and the buffers touched, not the
// plan node.
const readSQL = `
SELECT model_key, schema_version, source_sha256, as_of,
       computed_at, pass_duration_ms, row_count, rows
FROM status_summary_snapshots
WHERE model_key = $1
`

// tryLockSQL takes the writer's transaction-scoped advisory lock without
// waiting. It is released when the transaction ends, so a crashed holder frees
// it with its backend and no lease table or expiry is needed.
const tryLockSQL = `SELECT pg_try_advisory_xact_lock($1)`
