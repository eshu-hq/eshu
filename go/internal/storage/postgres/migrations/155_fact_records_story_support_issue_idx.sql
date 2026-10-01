-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7464: the story target-support read links a Jira work_item.record or
-- work_item.transition to a repository through the same issue's
-- work_item.external_link (go/internal/query/support, JiraIssueLinkSQL). The
-- second hop probes the facts of each linked issue by scope, generation, kind and
-- payload->>'provider_work_item_id'. No index carries that key, so each probe
-- reads every record and transition of the generation through
-- fact_records_story_support_kinds_idx and filters the issue id in the heap
-- (952 ms and 344,516 buffers at 50k links and 50k records/transitions in one
-- scope, 1.04 s and 525,609 buffers with 100 transitions on each of 5,000 linked
-- issues).
--
-- Partial over the two kinds and non-tombstoned facts. The kind is a key column,
-- not only a predicate: with the kind left out each probe returned an issue's
-- record and its hundred transitions together and discarded all but one in the
-- heap. Keyed this way a probe is a direct descent (9.7 ms and 6,959 buffers at
-- 50k links, 37.6 ms at 500k, 25.6k buffers with 100 transitions per issue). The
-- statement carries the same kind literals, tombstone predicate and plain
-- payload->>'provider_work_item_id' expression
-- (TestServiceStoryTargetSupportIssueIndexMatchesQuery binds them), so Postgres
-- proves the query implies this predicate in custom and generic plans alike. Only
-- work_item.record and work_item.transition facts pay index maintenance.
--
-- CONCURRENTLY so bootstrap never blocks writers; IF NOT EXISTS for rerun
-- safety, matching the 073/086/118/121/122/123/124/152 precedent. One statement
-- per file so the migration coordinator can run it outside a transaction
-- (coordination.IsSoleConcurrentIndexStatement). A NEW migration file per issue
-- #7002: never edit an applied migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_story_support_issue_idx
    ON fact_records (scope_id, generation_id, fact_kind, (payload->>'provider_work_item_id'))
    WHERE fact_kind IN ('work_item.record', 'work_item.transition')
      AND is_tombstone = FALSE;
