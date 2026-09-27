-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7088: package-id targets resolve reducer-equivalent consumption names from
-- active registry package identities. The narrow partial index served one
-- lookup at 0.270 ms versus a 30.357 ms scan of 80000 synthetic facts, with
-- identical active rows. Scope/generation stay in the active join, not here:
-- a wider index caused repeated per-scope probes in the measured plan.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_package_registry_package_id_idx
    ON fact_records ((payload->>'package_id'))
    WHERE fact_kind = 'package_registry.package'
      AND is_tombstone = FALSE;
