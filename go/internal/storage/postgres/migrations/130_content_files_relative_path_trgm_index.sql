-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7033: InvestigateCodeTopic probes content_files.relative_path without a
-- repository constraint. The existing primary key begins with repo_id, so it
-- cannot serve that unscoped substring predicate. This exact pg_trgm GIN is
-- the measured candidate. It is deferred for cold bootstrap by
-- BootstrapDefinitionsWithoutContentSearchIndexes and built after projection
-- by EnsureContentSearchIndexes. Normal upgrades build it concurrently, so
-- content ingestion remains available while an existing table is indexed.
--
-- This file must remain one concurrent statement: PostgreSQL rejects CREATE
-- INDEX CONCURRENTLY in a transaction block, and the migration coordinator
-- executes sole concurrent-index definitions in autocommit mode. The schema
-- executor clears an invalid same-name index before retrying a failed build.
CREATE INDEX CONCURRENTLY IF NOT EXISTS content_files_relative_path_trgm_idx
    ON content_files USING gin (relative_path gin_trgm_ops);
