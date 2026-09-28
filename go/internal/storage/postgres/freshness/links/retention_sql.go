// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

// Ledger tables that generation retention prunes (#7127 ruling 2.8). They key
// the retention row counts and RowsPruned. changed_since_key_state and
// changed_since_scope_cursor describe each scope's active generation and are
// never pruned by retention.
const (
	TableActivations      = "changed_since_activations"
	TableLinks            = "changed_since_links"
	TableLinkDeltas       = "changed_since_link_deltas"
	TableLinkBucketCounts = "changed_since_link_bucket_counts"
)

// retentionDoomedLinksCTE selects the links retention retires for the pruned
// generations: $1 and $2 are parallel arrays, one entry per candidate, of
// scope ids and generation ids (oldest first). A link is doomed when its
// generation or prior generation is in $2 (#7127 ruling 2.8 and
// arb-7127-3d). rank is the position in $2 of the newer of the two pruned
// generations a link names, so a link naming two of them is charged to the
// newer one, as generation retention charges shared content rows.
//
// It reads the links by the scope_id prefix of their primary key with array
// filters, never by a join or a per-candidate LATERAL: generation ids are
// globally unique (the primary key of scope_generations), so array membership
// is exact, and the plan reads changed_since_links once whatever its
// statistics. With statistics taken at 5 scopes, a per-scope LATERAL (with or
// without OFFSET 0) planned one sequential scan of the links per scope
// (docs/internal/evidence/7127-changed-since-ledger-retention.md, and the
// planted RED of TestRetentionStatementPlanShape).
const retentionDoomedLinksCTE = `
doomed AS MATERIALIZED (
    SELECT link.ctid AS link_ctid, link.scope_id, link.generation_id, link.prior_generation_id, link.delta_rows,
           GREATEST(array_position($2::text[], link.generation_id),
                    array_position($2::text[], link.prior_generation_id)) AS rank
    FROM ` + TableLinks + ` AS link
    WHERE link.scope_id = ANY ($1::text[])
      AND (link.generation_id = ANY ($2::text[]) OR link.prior_generation_id = ANY ($2::text[]))
)`

// retentionRowCountsQuery reports, per pruned generation in $2 and ledger
// table, the rows retentionPruneQuery deletes, so BatchRowLimit and the
// retention events count the ledger (#7127 ruling 2.8). Link deltas are
// counted from changed_since_links.delta_rows, which the link statement sets
// to the rows it inserted in the same statement; counting the delta rows
// themselves read 802k buffers for a 775k-row link against 34-41 for this
// sum.
// Bucket counts are probed per link through the primary key's (scope,
// generation, prior) prefix.
//
// $1 scope ids and $2 generation ids of the pruned generations, parallel,
// oldest first.
const retentionRowCountsQuery = `
WITH ` + retentionDoomedLinksCTE + `,
buckets AS MATERIALIZED (
    SELECT link.rank, count(*) AS row_count
    FROM doomed AS link
    CROSS JOIN LATERAL (
        SELECT 1 FROM ` + TableLinkBucketCounts + ` AS bucket
        WHERE bucket.scope_id = link.scope_id
          AND bucket.generation_id = link.generation_id
          AND bucket.prior_generation_id = link.prior_generation_id
        OFFSET 0
    ) AS bucket
    GROUP BY link.rank
),
activations AS MATERIALIZED (
    SELECT activation.generation_id
    FROM ` + TableActivations + ` AS activation
    WHERE activation.scope_id = ANY ($1::text[])
      AND activation.generation_id = ANY ($2::text[])
)
SELECT pruned.generation_id, counts.table_name, counts.row_count
FROM unnest($2::text[]) WITH ORDINALITY AS pruned(generation_id, rank)
CROSS JOIN LATERAL (VALUES
    ('` + TableLinks + `', (SELECT count(*) FROM doomed WHERE doomed.rank = pruned.rank)),
    ('` + TableLinkDeltas + `', (SELECT COALESCE(sum(doomed.delta_rows), 0)::bigint FROM doomed WHERE doomed.rank = pruned.rank)),
    ('` + TableLinkBucketCounts + `', COALESCE((SELECT buckets.row_count FROM buckets WHERE buckets.rank = pruned.rank), 0)),
    ('` + TableActivations + `', (SELECT count(*) FROM activations WHERE activations.generation_id = pruned.generation_id))
) AS counts(table_name, row_count)
`

// retentionPruneQuery deletes, for the pruned generations, the doomed links
// with their link deltas and bucket counts, and the activation rows of the
// pruned generations (#7127 ruling 2.8). The state table and the cursor are
// untouched. An activation is deleted only for its own generation_id, never
// for its prior_generation_id, which the writer compares as a string and
// never joins.
//
// It is one statement on purpose (arbiter ruling arb-7127-3d): every CTE of a
// WITH shares one snapshot, so the links picked, their deltas, their bucket
// counts and the links themselves are deleted as one unit. Separate
// statements leak: a link committing between the delta delete and the link
// delete loses its link row and keeps its deltas, which no rule reading pairs
// from changed_since_links can find again. A link that commits while this
// statement runs survives whole and is deleted with its own generation.
//
// Deltas and bucket counts are found by a LATERAL probe per doomed link on the
// full (scope, generation, prior) prefix of their primary key, with OFFSET 0
// so the probe stays a parameterized index scan; the plain join form of that
// access can plan a bitmap scan on scope_id alone under stale statistics (gate
// G4 in plan_live_test.go). Rows are then deleted by ctid. Ledger link rows
// are insert-only and a scope's rows are deleted only by retention (under the
// scope lock it holds) or by the orphan-scope purge (only for a scope with no
// ingestion_scopes row), so no concurrent writer moves a doomed row. The
// statement still returns the expected counts beside the deleted ones, and
// the caller fails the batch when they differ, so a broken assumption fails
// closed instead of leaving rows behind.
//
// Result: deleted links, deltas, bucket counts, activations, then the
// expected links, deltas and bucket counts.
//
// $1 scope ids and $2 generation ids of the pruned generations, parallel.
const retentionPruneQuery = `
WITH ` + retentionDoomedLinksCTE + `,
delta_rows AS MATERIALIZED (
    SELECT delta.delta_ctid
    FROM doomed AS link
    CROSS JOIN LATERAL (
        SELECT d.ctid AS delta_ctid FROM ` + TableLinkDeltas + ` AS d
        WHERE d.scope_id = link.scope_id
          AND d.generation_id = link.generation_id
          AND d.prior_generation_id = link.prior_generation_id
        OFFSET 0
    ) AS delta
),
bucket_rows AS MATERIALIZED (
    SELECT bucket.bucket_ctid
    FROM doomed AS link
    CROSS JOIN LATERAL (
        SELECT b.ctid AS bucket_ctid FROM ` + TableLinkBucketCounts + ` AS b
        WHERE b.scope_id = link.scope_id
          AND b.generation_id = link.generation_id
          AND b.prior_generation_id = link.prior_generation_id
        OFFSET 0
    ) AS bucket
),
del_deltas AS (
    DELETE FROM ` + TableLinkDeltas + ` AS t
    WHERE t.ctid = ANY (ARRAY(SELECT delta_ctid FROM delta_rows))
    RETURNING 1
),
del_buckets AS (
    DELETE FROM ` + TableLinkBucketCounts + ` AS t
    WHERE t.ctid = ANY (ARRAY(SELECT bucket_ctid FROM bucket_rows))
    RETURNING 1
),
del_links AS (
    DELETE FROM ` + TableLinks + ` AS t
    WHERE t.ctid = ANY (ARRAY(SELECT link_ctid FROM doomed))
    RETURNING 1
),
del_activations AS (
    DELETE FROM ` + TableActivations + ` AS t
    WHERE t.scope_id = ANY ($1::text[])
      AND t.generation_id = ANY ($2::text[])
    RETURNING 1
)
SELECT (SELECT count(*) FROM del_links),
       (SELECT count(*) FROM del_deltas),
       (SELECT count(*) FROM del_buckets),
       (SELECT count(*) FROM del_activations),
       (SELECT count(*) FROM doomed),
       (SELECT count(*) FROM delta_rows),
       (SELECT count(*) FROM bucket_rows)
`
