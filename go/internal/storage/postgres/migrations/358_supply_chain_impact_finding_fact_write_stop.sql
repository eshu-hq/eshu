-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #10880 PR-C2 k: refuse every INSERT or UPDATE of a reducer_supply_chain_impact_finding
-- fact_records row. From PR-C2 the impact writer stores a finding only in
-- supply_chain_impact_finding_supports; this trigger is the database-side guard that makes the
-- write stop durable. A reducer pod still on an image older than PR-C0 fails its writer
-- transaction with the error below (terminal for such an image: roll old pods to zero before
-- the image that carries this file), and a pod on a PR-C0 or newer image classifies it
-- (supply_chain_impact_fact_write_stopped, retryable, counted on
-- eshu_dp_supply_chain_impact_fact_write_rejected_total).
--
-- The raise names SQLSTATE 55000 AND the constraint fact_records_reject_supply_chain_impact_finding
-- on table fact_records. The PR-C0 classifier fires only on that pair, and the repo's older
-- triggers (088, 092, 250) raise with ERRCODE alone, which leaves the constraint name empty:
-- copying their form would make the classifier silently never fire.
--
-- Unconditional: no setting bypasses it (unlike the old-writer fence of 338). DELETE is not
-- covered: retention removes frozen rows with their generation. Existing rows stay as they are.
-- Rollback switch: ALTER TABLE fact_records DISABLE TRIGGER
-- fact_records_reject_supply_chain_impact_finding, only after every reducer pod runs the old
-- image (see docs/internal/evidence/10880-prc-c2-write-stop.md).
--
-- Lock: CREATE TRIGGER takes SHARE ROW EXCLUSIVE on fact_records for the catalog write only (it
-- waits behind open writer transactions and blocks new ones, at most the runner's lock_timeout per
-- attempt; SELECT is not blocked). The pg_trigger guard makes a re-apply take no lock and keeps a
-- disabled trigger disabled. The function holds no table access, so it needs no search_path pin.

CREATE OR REPLACE FUNCTION reject_supply_chain_impact_finding_fact_record_write()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    RAISE EXCEPTION USING
        ERRCODE = '55000',
        MESSAGE = 'reducer_supply_chain_impact_finding fact_records writes are stopped: findings are stored in supply_chain_impact_finding_supports (#10880)',
        CONSTRAINT = 'fact_records_reject_supply_chain_impact_finding',
        TABLE = 'fact_records';
END;
$function$;

DO $trigger$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgrelid = 'fact_records'::regclass
          AND tgname = 'fact_records_reject_supply_chain_impact_finding'
          AND NOT tgisinternal
    ) THEN
        CREATE TRIGGER fact_records_reject_supply_chain_impact_finding
        BEFORE INSERT OR UPDATE ON fact_records
        FOR EACH ROW
        WHEN (NEW.fact_kind = 'reducer_supply_chain_impact_finding')
        EXECUTE FUNCTION reject_supply_chain_impact_finding_fact_record_write();
    END IF;
END;
$trigger$;
