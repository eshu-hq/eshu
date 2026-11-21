-- Persist the winners contract with the winners watermark (#11117 A).
--
-- A reducer roll used to re-run the full winners resweep in every pod: the
-- maintainer remembered "a full ran" only in process memory, so every new
-- process ran its own startup full (about 1 h 50 min each on QA). The full
-- that committed records here the contract version and hash of the rebuild
-- and keyed statements it ran. Each maintainer tick reads them: no row, a
-- NULL or a lower version runs one full across all pods, an equal version
-- runs keyed ticks and the audit only, and a higher version (a newer binary
-- already committed) stands the older binary down.
--
-- Both columns are nullable and have no default, so the ALTER is
-- metadata-only and takes no table rewrite. The migration does NOT backfill a
-- version: the first binary that reads the columns must run one full to
-- record the contract it actually rebuilt with. Older binaries ignore the
-- columns. A rollback to a pre-#11117 binary, or across a contract version
-- bump, must NULL both columns (runbook step), so the next binary that reads
-- them runs a full.
ALTER TABLE supply_chain_impact_winners_materialization
    ADD COLUMN IF NOT EXISTS contract_version INTEGER,
    ADD COLUMN IF NOT EXISTS contract_hash TEXT;
