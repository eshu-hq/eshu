-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Back the #7420 explicit retention deletes on
-- package_registry_identity_keys.
-- Evidence: docs/internal/evidence/7420-key-sidecar-explicit-deletes.md.
--
-- WHAT GOES WRONG TODAY. Same shape as migration 182: the table reaches
-- fact_records only through its per-fact fact_id foreign key, so a retention
-- prune fires one referential-integrity trigger per deleted fact. #7420 PR1
-- adds explicit deletes keyed by (scope_id, generation_id), plus the page's
-- fact ids on the over-limit chunk path; PR2 drops the cascade. The
-- primary key leads with fact_id and the lookup index with (ecosystem,
-- package_name), so neither serves those predicates; the page delete by
-- fact id alone chose a sequential scan of the whole table.
--
-- THE INDEX. (scope_id, generation_id, fact_id), the shape of migration 058's
-- relationship_reference_candidate_keys_partition_idx: the generation delete
-- is one bitmap index range and the page delete one index range with the page
-- ids as the third-column array, with buffers that track the generation, not
-- the table. Build cost 0.8 s and 21 MB at 374k rows; the insert tax has the
-- same per-row shape as migration 182.
--
-- CONCURRENTLY so bootstrap never blocks the key writers; IF NOT EXISTS so
-- every later bootstrap over an install that has it is a no-op. The lone
-- statement in this file so the migration coordinator can run it outside a
-- transaction without the bootstrap lock_timeout (#7004). A new file per
-- #7002: never edit an applied migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS package_registry_identity_keys_partition_idx
    ON package_registry_identity_keys (scope_id, generation_id, fact_id);
