-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10411 (advisory-group projection): the qualifying-winners reader filters
-- supply_chain_impact_canonical_winners by the coalesced group key
-- CASE WHEN btrim(cve_id) <> '' THEN cve_id ELSE advisory_id END once per
-- advisory_group_projection intent. Without a matching index the read is a
-- sequential scan of the whole winners table per touched group, which is
-- O(groups x winners) on a hot reducer path. This expression index serves
-- exactly that predicate; the remaining suppression/scope predicates are
-- heap filters over the group's few winner rows.

CREATE INDEX IF NOT EXISTS supply_chain_impact_winners_group_key_idx
    ON supply_chain_impact_canonical_winners
       ((CASE WHEN btrim(cve_id) <> '' THEN cve_id ELSE advisory_id END));
