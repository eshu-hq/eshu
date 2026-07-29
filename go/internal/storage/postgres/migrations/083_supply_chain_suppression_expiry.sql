ALTER TABLE supply_chain_impact_canonical_winners
    ADD COLUMN IF NOT EXISTS suppression_expires_at TIMESTAMPTZ NULL;

UPDATE supply_chain_impact_canonical_winners AS winner
SET suppression_expires_at = CASE
    WHEN pg_input_is_valid(
        fact.payload #>> '{suppression,expires_at}',
        'timestamp with time zone'
    )
    THEN (fact.payload #>> '{suppression,expires_at}')::timestamptz
    ELSE '-infinity'::timestamptz
END
FROM fact_records AS fact
WHERE winner.winner_scope_id = 'operator:vulnerability_suppressions'
  AND winner.suppression_state IN (
      'not_affected',
      'accepted_risk',
      'false_positive',
      'ignored'
  )
  AND winner.winner_fact_id = fact.fact_id
  AND NULLIF(fact.payload #>> '{suppression,expires_at}', '') IS NOT NULL;
