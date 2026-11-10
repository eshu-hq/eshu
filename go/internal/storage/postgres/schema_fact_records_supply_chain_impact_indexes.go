// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const supplyChainImpactFactRecordReadIndexesSQL = `
-- #10408: the advisory arm of the holder fan-out lookup. A re-key retires
-- group and member ids no replacement row names; a holder keyed by such an
-- id (an advisory-keyed GHSA-only row, a holder whose deciding record names
-- the fragment) matches payload->>'advisory_id' when payload->>'cve_id'
-- misses. Same shape as the CVE arm so the planner can serve both arms
-- from matching partial indexes.
CREATE INDEX IF NOT EXISTS fact_records_supply_chain_impact_advisory_lookup_idx
    ON fact_records (
        (payload->>'advisory_id'),
        (payload->>'impact_status'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;

-- #10408: the member-set arm of the holder fan-out lookup. A holder whose
-- group id the committing pass never saw (a private advisory id under a
-- divergent alias closure) still carries the shared member in its stored
-- advisory_group_members, so the lookup matches the stored set against the
-- fanned-out ids with ?|. GIN like the sibling
-- fact_records_security_alert_cve_ids_idx array arm.
CREATE INDEX IF NOT EXISTS fact_records_supply_chain_impact_group_members_idx
    ON fact_records USING GIN ((payload #> '{provenance,advisory_group_members}'))
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;

-- #9975: the keyed winners resweep probe. The package triple plus the
-- coalesced c0 (cve_id, else advisory_id) and advisory_id expressions the
-- probe matches per dirty identifier, so all five equalities are index
-- conditions and the heap fetches only kept facts instead of every candidate
-- of a touched triple. fact_id and generation_id ride along like the sibling
-- impact indexes. Migration 291 builds it concurrently on existing
-- deployments; the expressions must stay byte-identical to the probe in
-- supply_chain_impact_winners_keyed_sql.go or the planner cannot use it.
CREATE INDEX IF NOT EXISTS fact_records_supply_chain_impact_winners_keyed_idx
    ON fact_records (
        (payload->>'package_id'),
        (payload->>'repository_id'),
        (payload->>'subject_digest'),
        (COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), '')),
        (COALESCE(payload->>'advisory_id', '')),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;
`
