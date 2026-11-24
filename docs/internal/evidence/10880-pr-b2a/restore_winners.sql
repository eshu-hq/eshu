-- restore_winners.sql: undo worst_case_all_winners_fail.sql.
\set ON_ERROR_STOP on
UPDATE supply_chain_impact_finding_supports s SET detection_profile = v.detection_profile
  FROM b2a_worst_saved v WHERE v.fact_id = s.fact_id;
UPDATE supply_chain_impact_canonical_winners w SET detection_profile = s.detection_profile
  FROM supply_chain_impact_finding_supports s WHERE s.fact_id = w.winner_fact_id;
VACUUM (ANALYZE) supply_chain_impact_finding_supports;
VACUUM (ANALYZE) supply_chain_impact_canonical_winners;
