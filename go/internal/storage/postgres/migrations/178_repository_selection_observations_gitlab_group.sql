-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7765: admit the gitlab_group selector kind to
-- repository_selection_observations on installs that already applied
-- migration 166.
--
-- Fresh bootstraps run 166 (two-kind CHECK) then this file, which widens
-- it; older installs converge once. The guard below is a no-op on every
-- boot after it. That matters because this directory has no
-- applied-migration ledger: BootstrapDefinitions enumerates every file
-- under migrations/ and ApplyDefinitions Execs all of them on EVERY
-- bootstrap.
--
-- github_repo_id keeps its name and stores the selector's provider numeric
-- ID: the GitHub repository ID for github_org selectors, the GitLab project
-- ID for gitlab_group selectors. No reader interprets it beyond the upsert
-- COALESCE, the observation read, and the prior-row carry-forward, so no
-- rename is needed.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'repository_selection_observations_selector_kind_check'
          AND conrelid = 'repository_selection_observations'::regclass
          AND pg_get_constraintdef(oid) NOT LIKE '%gitlab_group%'
    ) THEN
        ALTER TABLE repository_selection_observations
            DROP CONSTRAINT repository_selection_observations_selector_kind_check;
        ALTER TABLE repository_selection_observations
            ADD CONSTRAINT repository_selection_observations_selector_kind_check
            CHECK (selector_kind IN ('github_org', 'explicit', 'gitlab_group'));
    END IF;
END
$$;

COMMENT ON COLUMN repository_selection_observations.github_repo_id IS
'Provider numeric repository ID: the GitHub repository ID for github_org selectors, the GitLab project ID for gitlab_group selectors (#7765).';
