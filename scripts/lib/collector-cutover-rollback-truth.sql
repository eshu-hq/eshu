-- eshu-rollback:truth
-- Read-only. Fact truth for one collector kind over each scope's ACTIVE
-- (committed) generation. Fact IDs change when the producer changes, so the
-- comparison uses fact kind, stable_fact_key, and payload, never fact_id.
-- Output per fact kind: fact_kind|count|stable_key_digest|payload_digest
-- Variables: kind.
--
-- Each digest is an order-independent set digest: every row is hashed with
-- SHA-256 (a FIPS-approved builtin), two 64-bit lanes of that hash are summed
-- as exact numerics, and the lane sums are printed as "lane1:lane2". There is no
-- sort and no aggregate-size cap, so memory stays flat as the fact count grows.
-- It detects accidental drift (a missing, extra, or changed row); it is not
-- collision resistant against an adversary. Duplicate rows change the sum.
-- Cost: one hash-aggregate pass over the active-generation facts of this
-- collector kind. Run it on a reader or standby when one exists.
SELECT
    fact.fact_kind,
    count(*),
    sum(('x' || encode(substring(row_hash.key_hash FROM 1 FOR 8), 'hex'))::bit(64)::bigint::numeric)::text
        || ':' ||
    sum(('x' || encode(substring(row_hash.key_hash FROM 9 FOR 8), 'hex'))::bit(64)::bigint::numeric)::text,
    sum(('x' || encode(substring(row_hash.payload_hash FROM 1 FOR 8), 'hex'))::bit(64)::bigint::numeric)::text
        || ':' ||
    sum(('x' || encode(substring(row_hash.payload_hash FROM 9 FOR 8), 'hex'))::bit(64)::bigint::numeric)::text
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
CROSS JOIN LATERAL (
    SELECT
        sha256(convert_to(fact.stable_fact_key, 'UTF8')) AS key_hash,
        sha256(convert_to(fact.stable_fact_key || E'\x1f' || fact.payload::text, 'UTF8')) AS payload_hash
) AS row_hash
WHERE scope.collector_kind = :'kind'
  AND NOT fact.is_tombstone
GROUP BY fact.fact_kind
ORDER BY fact.fact_kind;
