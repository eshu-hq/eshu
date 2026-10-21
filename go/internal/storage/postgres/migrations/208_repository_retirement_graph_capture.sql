-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Graph-retraction capture columns for operator-driven repository
-- retirement (#8659, Deliverable 2 step 2c).
--
-- The graph retraction runs while the purge has not yet deleted the
-- repository's scope rows, but any resume after a crash (or after 2d/2e
-- have deleted content_files) must still key the G-statements and the
-- residue census on the same ids. These columns are that stable copy:
--   - graph_repo_root is the repository root read once at 2c entry, with
--     the precedence BuildMaterialization uses (repository fact path, else
--     scope metadata repo_path). It is written once and never overwritten,
--     so a re-run of 2c after G6 cleared Repository.path still has it. An
--     empty root must fail the retirement closed at the runner: refusing
--     the capture is runner work (as with empty scopes below), never
--     enforced by this schema.
--   - graph_scope_ids is the JSON array of scope ids the purge locked for
--     this repository (default-branch scope plus @ref scopes), captured at
--     the same seam. The documentation retraction (G3) and the residue
--     census (2z) read it, never the live ingestion_scopes rows, once the
--     purge begins.
ALTER TABLE repository_retirements
    ADD COLUMN IF NOT EXISTS graph_repo_root TEXT NULL;
ALTER TABLE repository_retirements
    ADD COLUMN IF NOT EXISTS graph_scope_ids JSONB NOT NULL DEFAULT '[]'::jsonb;
