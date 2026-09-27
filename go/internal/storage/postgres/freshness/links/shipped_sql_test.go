// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

// shippedIncrementalLinkSQL is IncrementalLinkSQL as shipped before the G8
// fix (#7127 PR-3a, arbiter ruling arb-7127-g8): its del CTE joins the state
// table to the diff CTE on the key, which the planner inverts into a nested
// loop that rescans diff per state row when the scope's state rows are
// estimated at one. It is frozen here (captured from the package before the
// edit; the trailing-whitespace hook trimmed line ends, which SQL ignores) as
// the RED writer of the plan-shape and timing
// gates; production code never uses it.
const shippedIncrementalLinkSQL = `
WITH
cur AS MATERIALIZED (
    SELECT cat, stable_fact_key,
           MIN(fact_kind) FILTER (WHERE NOT is_tombstone) AS kind,
           MIN(fact_kind) AS any_kind,
           sha256(string_agg(h, ''::bytea ORDER BY h) FILTER (WHERE NOT is_tombstone)) AS state,
           bool_or(is_tombstone) AS tombstoned,
           MIN(source_uri) FILTER (WHERE NOT is_tombstone) AS owner_uri
    FROM (
        SELECT CASE WHEN fact_kind = 'file' THEN 'files'
                    WHEN fact_kind = 'content_entity' THEN 'content_entities'
                    ELSE 'facts' END AS cat,
               stable_fact_key, fact_kind, is_tombstone, source_uri,
               CASE WHEN is_tombstone THEN NULL
                    ELSE sha256(convert_to((CASE WHEN fact_kind = 'content_entity' AND jsonb_typeof(payload) = 'object' THEN payload - 'indexed_at' ELSE payload END)::text, 'UTF8')) END AS h
        FROM fact_records
        WHERE scope_id = $1 AND generation_id = $2 AND fact_kind NOT LIKE 'reducer\_%'
        OFFSET 0
    ) AS r
    GROUP BY cat, stable_fact_key
),
diff AS MATERIALIZED (
    SELECT COALESCE(s.fact_category, c.cat) AS cat,
           COALESCE(s.stable_fact_key, c.stable_fact_key) AS k,
           s.fact_kind AS prior_kind, COALESCE(c.kind, c.any_kind) AS current_kind,
           s.state AS prior_state, c.state AS current_state,
           COALESCE(c.tombstoned, FALSE) AS tombstoned, c.owner_uri
    FROM (SELECT * FROM changed_since_key_state WHERE scope_id = $1) AS s
    FULL JOIN cur AS c ON c.cat = s.fact_category AND c.stable_fact_key = s.stable_fact_key
    WHERE s.state IS DISTINCT FROM c.state
       OR (c.state IS NOT NULL AND s.fact_kind IS DISTINCT FROM c.kind)
       OR COALESCE(c.tombstoned, FALSE)
),
ins AS (
    INSERT INTO changed_since_link_deltas
        (scope_id, generation_id, prior_generation_id, fact_category, classification, stable_fact_key,
         prior_fact_kind, current_fact_kind, prior_state, current_state, current_tombstoned)
    SELECT $1, $2, $3, d.cat,
           CASE WHEN d.prior_state IS NULL AND d.current_state IS NULL THEN 'dropped'
                WHEN d.prior_state IS NULL THEN 'added'
                WHEN d.current_state IS NULL AND d.tombstoned THEN 'retired'
                WHEN d.current_state IS NULL THEN 'superseded'
                WHEN d.prior_state <> d.current_state THEN 'updated'
                ELSE 'unchanged' END,
           d.k, d.prior_kind, d.current_kind, d.prior_state, d.current_state, d.tombstoned
    FROM diff AS d
    RETURNING fact_category, classification
),
del AS (
    DELETE FROM changed_since_key_state AS t
    USING diff AS d
    WHERE t.scope_id = $1 AND t.fact_category = d.cat AND t.stable_fact_key = d.k
      AND d.current_state IS NULL
    RETURNING 1
),
ups AS (
    INSERT INTO changed_since_key_state (scope_id, fact_category, stable_fact_key, fact_kind, state, owner_uri)
    SELECT $1, cat, k, current_kind, current_state, owner_uri FROM diff WHERE current_state IS NOT NULL
    ON CONFLICT (scope_id, fact_category, stable_fact_key)
    DO UPDATE SET fact_kind = EXCLUDED.fact_kind, state = EXCLUDED.state, owner_uri = EXCLUDED.owner_uri
    RETURNING 1
),
bk AS (
    INSERT INTO changed_since_link_bucket_counts
        (scope_id, generation_id, prior_generation_id, fact_category, classification, key_count)
    SELECT $1, $2, $3, fact_category, classification, count(*)
    FROM ins
    GROUP BY fact_category, classification
    RETURNING 1
),
lnk AS (
    INSERT INTO changed_since_links
        (scope_id, generation_id, prior_generation_id, link_kind, digest_version, delta_rows,
         files_keys, content_entities_keys, facts_keys, computed_at)
    SELECT $1, $2, $3, 'incremental', $4, (SELECT count(*) FROM ins),
           count(*) FILTER (WHERE state IS NOT NULL AND cat = 'files'),
           count(*) FILTER (WHERE state IS NOT NULL AND cat = 'content_entities'),
           count(*) FILTER (WHERE state IS NOT NULL AND cat = 'facts'),
           $5
    FROM cur
    RETURNING delta_rows, files_keys, content_entities_keys, facts_keys
)
SELECT delta_rows, files_keys, content_entities_keys, facts_keys,
       (SELECT count(*) FROM del), (SELECT count(*) FROM ups), (SELECT count(*) FROM bk)
FROM lnk
`
