-- worst_case_all_winners_fail.sql: every winner fails the precise filter (its support is
-- stamped comprehensive) while the key's other supports keep their stamps, so branch B
-- probes every key. Run after the matrix; restore_winners.sql undoes it.
\set ON_ERROR_STOP on
CREATE TABLE IF NOT EXISTS b2a_worst_saved AS SELECT fact_id, detection_profile FROM supply_chain_impact_finding_supports WHERE false;
TRUNCATE b2a_worst_saved;
INSERT INTO b2a_worst_saved SELECT s.fact_id, s.detection_profile FROM supply_chain_impact_finding_supports s
  JOIN supply_chain_impact_canonical_winners w ON w.winner_fact_id = s.fact_id;
UPDATE supply_chain_impact_finding_supports s SET detection_profile = 'comprehensive'
  FROM supply_chain_impact_canonical_winners w WHERE w.winner_fact_id = s.fact_id;
UPDATE supply_chain_impact_canonical_winners SET detection_profile = 'comprehensive';
VACUUM (ANALYZE) supply_chain_impact_finding_supports;
VACUUM (ANALYZE) supply_chain_impact_canonical_winners;
