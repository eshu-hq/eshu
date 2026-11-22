-- Candidate read indexes; each is kept only if a measured plan uses it.
CREATE INDEX IF NOT EXISTS supply_chain_impact_finding_supports_scope_idx
    ON supply_chain_impact_finding_supports (scope_id);
CREATE INDEX IF NOT EXISTS supply_chain_impact_finding_supports_repository_idx
    ON supply_chain_impact_finding_supports (repository_id);
CREATE INDEX IF NOT EXISTS supply_chain_impact_finding_supports_finding_idx
    ON supply_chain_impact_finding_supports (finding_id);
CREATE INDEX IF NOT EXISTS supply_chain_impact_finding_supports_operator_idx
    ON supply_chain_impact_finding_supports (canonical_key)
    WHERE scope_id = 'operator:vulnerability_suppressions';
