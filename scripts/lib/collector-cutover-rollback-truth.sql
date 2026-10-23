-- eshu-rollback:truth
-- Read-only. Fact truth for one collector kind over each scope's ACTIVE
-- (committed) generation. Fact IDs change when the producer changes, so the
-- comparison uses fact kind, stable_fact_key, and payload, never fact_id.
-- Output per fact kind: fact_kind|count|stable_key_digest|payload_digest
-- Variables: kind.
-- Cost: one pass over the active-generation facts of this collector kind.
-- Run it on a reader or standby when one exists.
SELECT
    fact.fact_kind,
    count(*),
    md5(string_agg(fact.stable_fact_key, ',' ORDER BY fact.stable_fact_key, md5(fact.payload::text))),
    md5(string_agg(fact.stable_fact_key || '=' || md5(fact.payload::text), ',' ORDER BY fact.stable_fact_key, md5(fact.payload::text)))
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
WHERE scope.collector_kind = :'kind'
  AND NOT fact.is_tombstone
GROUP BY fact.fact_kind
ORDER BY fact.fact_kind;
