-- Measurement shim for #11208, not a gate. buildSSMHostActiveFactsSQL output at
-- origin/main 9f22ee51e4 (AllScopes, default limit), dumped by a throwaway Go test;
-- only $1 -> 6001 (cap+1 rows per host) and $2 -> 11 (offset+limit+1 hosts) substituted.

SELECT jsonb_build_object(
    'fact_id', fact_records.fact_id,
    'fact_kind', fact_records.fact_kind,
    'scope_id', fact_records.scope_id,
    'generation_id', fact_records.generation_id,
    'is_tombstone', fact_records.is_tombstone,
    'payload', fact_records.payload
) AS envelope
FROM (
    SELECT fact_records.*,
        ROW_NUMBER() OVER (
            PARTITION BY fact_records.payload->>'partition', fact_records.payload->>'account_id', fact_records.payload->>'region', fact_records.payload->>'instance_id'
            ORDER BY fact_records.fact_id
        ) AS host_row_number,
        DENSE_RANK() OVER (
            ORDER BY fact_records.payload->>'partition', fact_records.payload->>'account_id', fact_records.payload->>'region', fact_records.payload->>'instance_id'
        ) AS host_rank
    FROM fact_records
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact_records.scope_id
     AND scope.active_generation_id = fact_records.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact_records.scope_id
     AND generation.generation_id = fact_records.generation_id
     AND generation.status = 'active'
    WHERE fact_records.fact_kind IN ('aws_ssm_host_application', 'aws_ssm_host_platform')
) AS fact_records
WHERE fact_records.host_row_number <= 6001
  AND fact_records.host_rank <= 11
ORDER BY fact_records.payload->>'partition', fact_records.payload->>'account_id', fact_records.payload->>'region', fact_records.payload->>'instance_id', fact_records.fact_id
