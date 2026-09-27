-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7248: a path-scoped repository tree read filters by an anchored LIKE
-- pattern before the 50,000-file cap. The primary-key path index follows the
-- database collation and cannot bound LIKE under non-C collations. Keep the
-- equality key first for repository isolation and use text_pattern_ops for a
-- literal path prefix. Build concurrently so content indexing can continue.
CREATE INDEX CONCURRENTLY IF NOT EXISTS content_files_repo_path_pattern_idx
    ON content_files (repo_id, relative_path text_pattern_ops);
