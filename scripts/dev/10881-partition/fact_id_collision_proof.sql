-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #10881 fact_id identity proof. fact_id is the primary key today, so a
-- GROUP BY fact_id HAVING count(DISTINCT scope_id) > 1 census returns zero by
-- construction. What can be shown is what the production upsert does when two
-- scopes present the same fact_id, and what changes under PRIMARY KEY
-- (scope_id, fact_id). The upsert below is the shape of facts_streaming.go
-- (SET list omits scope_id and generation_id; fencing guard; IS DISTINCT guard).
-- Self-checking: any assertion that fails aborts with an exception.
-- Run: psql -v ON_ERROR_STOP=1 -d scratch -f fact_id_collision_proof.sql
\set ON_ERROR_STOP on
CREATE TEMP TABLE und (fact_id text PRIMARY KEY, scope_id text NOT NULL, generation_id text NOT NULL,
                       payload jsonb NOT NULL, fencing_token bigint NOT NULL);
CREATE TEMP TABLE div (fact_id text, scope_id text NOT NULL, generation_id text NOT NULL,
                       payload jsonb NOT NULL, fencing_token bigint NOT NULL,
                       PRIMARY KEY (scope_id, fact_id)) PARTITION BY HASH (scope_id);
CREATE TEMP TABLE div_p0 PARTITION OF div FOR VALUES WITH (MODULUS 2, REMAINDER 0);
CREATE TEMP TABLE div_p1 PARTITION OF div FOR VALUES WITH (MODULUS 2, REMAINDER 1);

-- 1. Undivided: scope B's write is merged into scope A's row. No error.
INSERT INTO und VALUES ('F1', 'scopeA', 'genA', '{"v":"A"}', 1);
INSERT INTO und VALUES ('F1', 'scopeB', 'genB', '{"v":"B"}', 1)
ON CONFLICT (fact_id) DO UPDATE SET payload = EXCLUDED.payload, fencing_token = EXCLUDED.fencing_token
WHERE und.fencing_token <= EXCLUDED.fencing_token AND und.payload IS DISTINCT FROM EXCLUDED.payload;
DO $$ BEGIN
  IF (SELECT count(*) FROM und) <> 1 THEN RAISE EXCEPTION 'expected one merged row'; END IF;
  IF (SELECT scope_id || '/' || (payload->>'v') FROM und) <> 'scopeA/B' THEN
    RAISE EXCEPTION 'expected scopeA row carrying scopeB payload'; END IF;
  IF (SELECT count(*) FROM und WHERE scope_id = 'scopeB') <> 0 THEN
    RAISE EXCEPTION 'scopeB must have no row (silent loss)'; END IF;
END $$;

-- 2. Divided with the OLD conflict target: planner rejects it, SQLSTATE 42P10.
DO $$ BEGIN
  BEGIN
    INSERT INTO div VALUES ('F1', 'scopeA', 'genA', '{}', 1) ON CONFLICT (fact_id) DO NOTHING;
    RAISE EXCEPTION 'old target must fail on the divided table';
  EXCEPTION WHEN SQLSTATE '42P10' THEN NULL;
  END;
END $$;

-- 3. Divided with the NEW target: two rows, nothing lost.
INSERT INTO div VALUES ('F1', 'scopeA', 'genA', '{"v":"A"}', 1)
ON CONFLICT (scope_id, fact_id) DO UPDATE SET payload = EXCLUDED.payload;
INSERT INTO div VALUES ('F1', 'scopeB', 'genB', '{"v":"B"}', 1)
ON CONFLICT (scope_id, fact_id) DO UPDATE SET payload = EXCLUDED.payload;
DO $$ BEGIN
  IF (SELECT count(*) FROM div) <> 2 THEN RAISE EXCEPTION 'expected two rows'; END IF;
END $$;

-- 4. NEW target on the undivided table without a (scope_id, fact_id) unique
--    index: 42P10 too, so a new binary cannot run ahead of the DDL either.
DO $$ BEGIN
  BEGIN
    INSERT INTO und VALUES ('F2', 'scopeA', 'genA', '{}', 1) ON CONFLICT (scope_id, fact_id) DO NOTHING;
    RAISE EXCEPTION 'new target must fail on the undivided table';
  EXCEPTION WHEN SQLSTATE '42P10' THEN NULL;
  END;
END $$;

-- 5. A fact_id-only delete (retention chunk delete shape) now reaches both scopes.
DELETE FROM div WHERE fact_id = ANY (ARRAY['F1']);
DO $$ BEGIN
  IF (SELECT count(*) FROM div) <> 0 THEN RAISE EXCEPTION 'fact_id-only delete should remove both scopes'; END IF;
END $$;
\echo 'fact_id_collision_proof: all assertions passed'
