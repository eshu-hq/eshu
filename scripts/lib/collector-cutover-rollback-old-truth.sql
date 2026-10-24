SELECT fact.fact_kind, count(*),
    encode(sha256(convert_to(string_agg(fact.stable_fact_key, ',' ORDER BY fact.stable_fact_key, fact.payload::text), 'UTF8')), 'hex'),
    encode(sha256(convert_to(string_agg(fact.stable_fact_key || '=' || encode(sha256(convert_to(fact.payload::text, 'UTF8')), 'hex'),
                   ',' ORDER BY fact.stable_fact_key, fact.payload::text), 'UTF8')), 'hex')
FROM fact_records AS fact
JOIN ingestion_scopes AS scope ON scope.scope_id = fact.scope_id AND scope.active_generation_id = fact.generation_id
WHERE scope.collector_kind = :'kind' AND NOT fact.is_tombstone
GROUP BY fact.fact_kind ORDER BY fact.fact_kind;
