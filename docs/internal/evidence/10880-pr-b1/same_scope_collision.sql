-- Two active facts in ONE scope whose Go-side logical identities differ (cve_id '' vs
-- cve_id = advisory_id) but whose SQL canonical key is identical: the key's first
-- component is COALESCE(NULLIF(cve_id,''), NULLIF(advisory_id,''), '').
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES
('fact:c1:x', 'git-repository-scope:repository:sa', 'gen:a', 'reducer_supply_chain_impact_finding', 'fact:c1:x', '1.0.0', 'synthetic', 1, 'observed', 'synthetic', 'fact:c1:x', now(), now(),
 '{"finding_id":"finding:c1","cve_id":"ADV-C1","advisory_id":"ADV-C1","package_id":"pkg:npm/c1@1","repository_id":"repository:c1","impact_status":"affected_exact","observed_version":"1","match_reason":"npm_semver_affected_range","priority_score":61,"priority_bucket":"high","suppression_state":"active","cvss_score":6.1}'),
('fact:c1:y', 'git-repository-scope:repository:sa', 'gen:a', 'reducer_supply_chain_impact_finding', 'fact:c1:y', '1.0.0', 'synthetic', 1, 'observed', 'synthetic', 'fact:c1:y', now(), now(),
 '{"finding_id":"finding:c1","cve_id":"","advisory_id":"ADV-C1","package_id":"pkg:npm/c1@1","repository_id":"repository:c1","impact_status":"affected_exact","observed_version":"1","match_reason":"osv_range_match","priority_score":62,"priority_bucket":"high","suppression_state":"active","cvss_score":6.2}');
