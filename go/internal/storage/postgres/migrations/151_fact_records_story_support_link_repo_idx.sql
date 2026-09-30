-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7138: the service and repository story target-support row read
-- (go/internal/query/service_story_target_support.go,
-- buildServiceStoryTargetSupportSQL) links a repository to its Jira
-- pull-request and merge-request links through the one durable key the writer
-- emits, work_item.external_link payload->>'linked_repository_id'. Without an
-- index the probe of each active Jira generation reads every active
-- external_link fact through fact_records_story_support_kinds_idx and filters
-- the key in the heap (about 49.5k of 50k rows discarded per probe at 50k
-- links per scope; 13 ms at 50k and 206 ms at 500k).
--
-- Partial over the one kind and non-tombstoned facts, keyed like the scope and
-- generation prefix plus the key expression, so each probe is a direct index
-- descent (1.74 ms at 50k links, 17.4 ms at 500k). The statement carries the
-- same kind literal, tombstone predicate and plain payload->>'linked_repository_id'
-- = $N expression (TestServiceStoryTargetSupportLinkIndexMatchesQuery binds them),
-- so Postgres proves the query implies this predicate in custom and generic plans
-- alike. The plain form matters: a NULLIF(...) wrapper in the query cannot use
-- this expression. Only work_item.external_link facts pay index maintenance.
--
-- CONCURRENTLY so bootstrap never blocks writers; IF NOT EXISTS for rerun
-- safety, matching the 073/086/118/121/122/123/124 precedent. One statement per
-- file so the migration coordinator can run it outside a transaction
-- (coordination.IsSoleConcurrentIndexStatement). A NEW migration file per
-- issue #7002: never edit an applied migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_story_support_link_repo_idx
    ON fact_records (scope_id, generation_id, (payload->>'linked_repository_id'))
    WHERE fact_kind = 'work_item.external_link'
      AND is_tombstone = FALSE;
