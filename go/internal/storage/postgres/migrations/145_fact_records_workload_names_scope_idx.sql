-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7242: repositoryWorkloadNames resolves the repository to one scope, then
-- reads non-tombstoned reducer workload identity facts for that scope across
-- retained generations. The existing workload index is global and the observed
-- plan filters thousands of other-scope rows. Lead with scope_id and restrict
-- the index to this fact kind. Build concurrently to keep fact writes available.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_workload_names_scope_idx
    ON fact_records (scope_id)
    WHERE fact_kind = 'reducer_workload_identity'
      AND is_tombstone = FALSE;
