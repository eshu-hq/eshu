-- 325_supply_chain_impact_winners_payload.sql: carry the winner
-- payload and its source_confidence on the canonical winners read
-- model (#10880 slice 1), so the impact-findings list read can stop
-- joining fact_records for page payloads. The resweep (full and
-- keyed) backfills both columns from the winning fact on its first
-- pass after deploy; the write-on-change comparison covers them, so
-- a payload-only change rewrites the winner row.

ALTER TABLE supply_chain_impact_canonical_winners
    ADD COLUMN IF NOT EXISTS payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS source_confidence TEXT NOT NULL DEFAULT 'unknown';
