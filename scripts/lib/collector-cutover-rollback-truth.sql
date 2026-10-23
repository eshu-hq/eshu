-- eshu-rollback:truth
-- Read-only. Fact truth for one collector kind over each scope's ACTIVE
-- (committed) generation. Fact IDs change when the producer changes, so the
-- comparison uses fact kind, stable_fact_key, and payload, never fact_id.
-- Digests are SHA-256, so the query also runs on a FIPS-mode server.
-- Output per fact kind: fact_kind|count|stable_key_digest|payload_digest
-- Variables: kind.
-- Cost: one pass over the active-generation facts of this collector kind.
-- Run it on a reader or standby when one exists.
SELECT
    fact.fact_kind,
    count(*),
    encode(sha256(convert_to(
        string_agg(fact.stable_fact_key, ',' ORDER BY fact.stable_fact_key, fact.payload::text), 'UTF8')), 'hex'),
    encode(sha256(convert_to(
        string_agg(fact.stable_fact_key || '=' || encode(sha256(convert_to(fact.payload::text, 'UTF8')), 'hex'),
                   ',' ORDER BY fact.stable_fact_key, fact.payload::text), 'UTF8')), 'hex')
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
WHERE scope.collector_kind = :'kind'
  AND NOT fact.is_tombstone
GROUP BY fact.fact_kind
ORDER BY fact.fact_kind;
