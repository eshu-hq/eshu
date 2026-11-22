-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #10881 removal arms. Installed in a rehearsal database (und or div) by
-- removal_bench.sh. The drain mirrors the shipped retention predrain
-- (generation_retention_chunk_predrain_batch.go): page one superseded
-- generation's own fact ids oldest-first through the keyset index, then
-- delete the page by fact_id = ANY(ids), one committed transaction per page
-- (a PROCEDURE with COMMIT, so each page commits like the shipped batch).
--
--   drain_generations(gens, page_rows, scope_aware)
--     scope_aware = false : the shipped SQL, fact_id only
--     scope_aware = true  : the divided-table form, scope_id = $1 AND fact_id = ANY
-- It returns nothing; the caller times it. Rows deleted are counted in
-- bench_drain_log so both shapes can be compared by row count.

CREATE TABLE IF NOT EXISTS bench_drain_log (
    gen text, pages int, rows_deleted bigint, ms numeric
);

CREATE OR REPLACE PROCEDURE drain_generations(gens text[], page_rows int, scope_aware boolean)
LANGUAGE plpgsql AS $$
DECLARE
    g text; sc text; ids text[]; n bigint; total bigint; pages int; t0 timestamptz;
BEGIN
    FOREACH g IN ARRAY gens LOOP
        SELECT scope_id INTO sc FROM scope_generations WHERE generation_id = g;
        total := 0; pages := 0; t0 := clock_timestamp();
        LOOP
            SELECT array_agg(fact_id) INTO ids FROM (
                SELECT fact_id FROM fact_records
                WHERE scope_id = sc AND generation_id = g
                ORDER BY observed_at, fact_id LIMIT page_rows) page;
            EXIT WHEN ids IS NULL;
            IF scope_aware THEN
                DELETE FROM fact_records WHERE scope_id = sc AND fact_id = ANY(ids);
            ELSE
                DELETE FROM fact_records WHERE fact_id = ANY(ids);
            END IF;
            GET DIAGNOSTICS n = ROW_COUNT;
            total := total + n; pages := pages + 1;
            COMMIT;
        END LOOP;
        INSERT INTO bench_drain_log VALUES (g, pages, total,
            round(extract(epoch FROM clock_timestamp() - t0) * 1000, 1));
        COMMIT;
    END LOOP;
END $$;
