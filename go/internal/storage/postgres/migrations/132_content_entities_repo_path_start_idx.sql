-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7237: repository-scoped source substring search orders its bounded page
-- by path and start line. The global path index can walk every earlier
-- repository before finding one row for the requested repository, especially
-- for short or common patterns that cannot use trigram selectivity. Put the
-- repository equality first so that ordered walk stays inside the request's
-- repository. The existing trigram GIN index remains available for rare
-- longer patterns. Build concurrently to preserve content writers.
CREATE INDEX CONCURRENTLY IF NOT EXISTS content_entities_repo_path_start_idx
    ON content_entities (repo_id, relative_path, start_line);
