-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7279: the file-key companion of migration 139. Generation retention probes
-- it once per candidate (repo_id, relative_path) before pruning content_files
-- and content_file_references, instead of a grouped pass over every live file
-- fact under the batch's scope locks. On the 11 GB fixture the files prune fell
-- from 1.16-1.23 s to 0.05-0.06 s and the references prune from 1.7 s to
-- 0.06 s. The predicate repeats the retention statements' literal filters so a
-- generic plan can prove it. The retention store refuses a cycle until this
-- index is valid. Keep this the only statement in the file (see migration 139).
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_file_key_idx
    ON fact_records ((payload->>'repo_id'), (payload->>'relative_path'))
    WHERE fact_kind = 'file'
      AND is_tombstone = FALSE;
