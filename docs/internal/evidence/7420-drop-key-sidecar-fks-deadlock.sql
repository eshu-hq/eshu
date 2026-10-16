-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
-- #7420 PR2: why migrations 188-190 are not a plain ALTER TABLE ... DROP
-- CONSTRAINT. Two psql sessions on a scratch database at migration 183 (the
-- foreign keys present), with scope s1 and generation g1 seeded.
--
-- Session I (ingestion order: the fact, then its key):
--   BEGIN;
--   INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
--       source_system, source_fact_key, observed_at, ingested_at, payload)
--   VALUES ('f1', 's1', 'g1', 'content', 'k1', 'git', 'k1', now(), now(), '{}');
--   SELECT pg_sleep(2);
--   INSERT INTO relationship_reference_candidate_keys
--       (fact_id, scope_id, generation_id, source_repo_id, reference_key)
--   VALUES ('f1', 's1', 'g1', 'r', '|x|');
--   COMMIT;
--
-- Session M, started 0.5 s after session I (the plain form):
--   SET lock_timeout = '5s';
--   ALTER TABLE relationship_reference_candidate_keys
--       DROP CONSTRAINT IF EXISTS relationship_reference_candidate_keys_fact_id_fkey;
--
-- PostgreSQL 18.6 result: M takes ACCESS EXCLUSIVE on the key table, then
-- waits for ACCESS EXCLUSIVE on fact_records (held ROW EXCLUSIVE by I). I's key
-- insert then waits for the key table, and after deadlock_timeout:
--   ERROR:  deadlock detected
--   DETAIL: Process I waits for RowExclusiveLock on relation (key table);
--           blocked by process M.
--           Process M waits for AccessExclusiveLock on relation (fact_records);
--           blocked by process I.
-- Either session can be the victim. When M is the victim the migrator fails:
-- it retries only 55P03. TestFactKeyForeignKeyDropMigrationsAreDeadlockFreeLive
-- reproduced 40P01 for all three tables with the plain form.
--
-- Session M with the shipped form (the body of migration 188): it waits for
-- fact_records while holding nothing, I's key insert proceeds, I commits, and
-- M drops the key (1.58 s, mostly I's sleep). Against a session that holds
-- the key table first, M fails in 1.9 ms with 55P03, which the migrator
-- retries.
DO $$
DECLARE
    foreign_key record;
BEGIN
    FOR foreign_key IN
        SELECT conname FROM pg_constraint
        WHERE contype = 'f'
          AND conrelid = to_regclass('relationship_reference_candidate_keys')
          AND confrelid = to_regclass('fact_records')
    LOOP
        LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE;
        LOCK TABLE relationship_reference_candidate_keys IN ACCESS EXCLUSIVE MODE NOWAIT;
        EXECUTE format('ALTER TABLE relationship_reference_candidate_keys DROP CONSTRAINT IF EXISTS %I',
            foreign_key.conname);
    END LOOP;
END $$;
