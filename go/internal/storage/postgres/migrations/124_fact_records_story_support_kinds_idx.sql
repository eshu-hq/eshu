-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7126: the service and repository story target-support reads
-- (go/internal/query/service_story_target_support.go, the row read and the
-- source-only count) probe fact_records once per (active scope, generation,
-- support kind) through a LATERAL subquery. The only index they could use was
-- fact_records_scope_generation_idx, which spans every fact kind of every
-- generation, so each probe walked a large index. The support kinds
-- (work_item.*, incident_routing.*) are a tiny share of the table.
--
-- Partial over exactly those twelve kinds and non-tombstoned facts, keyed like
-- the scope/generation prefix so each probe is a direct index descent. The
-- statement carries the same twelve kinds as SQL literals beside the bound kind
-- array (TestServiceStoryTargetSupportIndexMatchesQuery binds them), so Postgres
-- proves the query implies this predicate in custom and generic plans. Without
-- the index the statements degrade to their previous cost, not worse.
--
-- No ref predicate here: the row read filters by target refs and the count by
-- absence of refs, so the index carries neither and serves both.
--
-- CONCURRENTLY so bootstrap never blocks writers; IF NOT EXISTS for rerun
-- safety, matching the 073/086/118/121/123 precedent. One statement per file so
-- the migration coordinator can run it outside a transaction
-- (coordination.IsSoleConcurrentIndexStatement). A NEW migration file per
-- issue #7002: never edit an applied migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_story_support_kinds_idx
    ON fact_records (scope_id, generation_id, fact_kind)
    WHERE fact_kind IN ('work_item.record', 'work_item.transition', 'work_item.external_link', 'work_item.project_metadata',
                        'work_item.issue_type_metadata', 'work_item.status_metadata', 'work_item.workflow_metadata',
                        'work_item.field_metadata', 'work_item.metadata_warning',
                        'incident_routing.applied_pagerduty_resource', 'incident_routing.observed_pagerduty_service',
                        'incident_routing.coverage_warning')
      AND is_tombstone = FALSE;
