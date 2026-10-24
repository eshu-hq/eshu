-- eshu-rollback:keys
-- Read-only. One line per active fact for the drift report: the fact kind, a
-- SHA-256 of the payload, and the stable key, separated by tabs. The key is
-- last so it may contain tabs; backslashes and newlines in it are escaped.
-- Variables: kind. Run it only when a baseline captured with --capture-keys
-- exists and the per-kind digests already differ.
SELECT fact.fact_kind
    || E'\t' || encode(sha256(convert_to(fact.payload::text, 'UTF8')), 'hex')
    || E'\t' || replace(replace(fact.stable_fact_key, E'\\', E'\\\\'), E'\n', E'\\n')
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
WHERE scope.collector_kind = :'kind'
  AND NOT fact.is_tombstone;
