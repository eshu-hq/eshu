-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8950: leave 50% free space on new content_files heap pages, so the
-- rollout writer's metadata-only UPDATE (commit_sha and generation_id only;
-- content and indexed_at untouched) rewrites an unchanged-content row under a
-- new generation in place as a HOT update instead of moving it to another
-- page and writing new entries in every index. Evidence:
-- docs/internal/evidence/8950-writer-prototype.md.
--
-- SET (fillfactor) takes SHARE UPDATE EXCLUSIVE and changes only the catalog:
-- existing pages are not rewritten, and only pages filled after it keep the
-- free space. Rows relocate onto the new pages as generations turn them over,
-- so the heap grows toward about 2x heap-alone while TOAST and the indexes
-- stay flat (a HOT rewrite adds no index entry). No VACUUM FULL, CLUSTER, or
-- pg_repack runs here or in automation: an operator-run VACUUM (FULL,
-- ANALYZE) stays a runbook item only, and it takes ACCESS EXCLUSIVE with
-- about 2x disk while it runs (duration unmeasured). Keep this the only
-- statement in the file.
ALTER TABLE content_files SET (fillfactor = 50);
