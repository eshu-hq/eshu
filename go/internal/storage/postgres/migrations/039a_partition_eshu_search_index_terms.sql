-- 039a_partition_eshu_search_index_terms.sql
--
-- Existing-install cutover for issue #5005. PostgreSQL cannot convert a
-- regular table into a partitioned table in place, so keep the public table
-- name stable by copying into a shadow partitioned parent, proving exact row
-- equivalence, and then atomically renaming the shadow into place.

DO $$
DECLARE
    diff_count BIGINT;
    partition_remainder INTEGER;
    terms_relkind "char";
BEGIN
    SELECT c.relkind
    INTO terms_relkind
    FROM pg_class c
    WHERE c.oid = to_regclass('eshu_search_index_terms');

    IF terms_relkind IS NULL THEN
        RETURN;
    END IF;

    IF terms_relkind = 'p' THEN
        DROP TABLE IF EXISTS eshu_search_index_terms_shadow CASCADE;
        DROP TABLE IF EXISTS eshu_search_index_terms_unpartitioned CASCADE;
        RETURN;
    END IF;

    IF terms_relkind <> 'r' THEN
        RAISE EXCEPTION 'eshu_search_index_terms has unsupported relkind %', terms_relkind;
    END IF;

    DROP TABLE IF EXISTS eshu_search_index_terms_shadow CASCADE;
    DROP TABLE IF EXISTS eshu_search_index_terms_unpartitioned CASCADE;

    CREATE TABLE eshu_search_index_terms_shadow (
        scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
        generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
        document_id TEXT NOT NULL,
        term_key TEXT NOT NULL,
        term TEXT NOT NULL,
        term_frequency INTEGER NOT NULL
    ) PARTITION BY HASH (scope_id);

    FOR partition_remainder IN 0..63 LOOP
        EXECUTE format(
            'CREATE TABLE eshu_search_index_terms_shadow_p%s PARTITION OF eshu_search_index_terms_shadow FOR VALUES WITH (MODULUS 64, REMAINDER %s)',
            to_char(partition_remainder, 'FM00'),
            partition_remainder
        );
    END LOOP;

    ALTER TABLE eshu_search_index_terms_shadow
        ADD CONSTRAINT eshu_search_index_terms_shadow_pkey
        PRIMARY KEY (scope_id, generation_id, term_key, document_id);

    INSERT INTO eshu_search_index_terms_shadow (
        scope_id,
        generation_id,
        document_id,
        term_key,
        term,
        term_frequency
    )
    SELECT
        scope_id,
        generation_id,
        document_id,
        term_key,
        term,
        term_frequency
    FROM eshu_search_index_terms
    ORDER BY scope_id, generation_id, term_key, document_id;

    LOCK TABLE eshu_search_index_terms IN ACCESS EXCLUSIVE MODE;

    SELECT count(*)
    INTO diff_count
    FROM (
        (
            SELECT scope_id, generation_id, document_id, term_key, term, term_frequency
            FROM eshu_search_index_terms
            EXCEPT ALL
            SELECT scope_id, generation_id, document_id, term_key, term, term_frequency
            FROM eshu_search_index_terms_shadow
        )
        UNION ALL
        (
            SELECT scope_id, generation_id, document_id, term_key, term, term_frequency
            FROM eshu_search_index_terms_shadow
            EXCEPT ALL
            SELECT scope_id, generation_id, document_id, term_key, term, term_frequency
            FROM eshu_search_index_terms
        )
    ) diff;

    IF diff_count <> 0 THEN
        TRUNCATE TABLE eshu_search_index_terms_shadow;

        INSERT INTO eshu_search_index_terms_shadow (
            scope_id,
            generation_id,
            document_id,
            term_key,
            term,
            term_frequency
        )
        SELECT
            scope_id,
            generation_id,
            document_id,
            term_key,
            term,
            term_frequency
        FROM eshu_search_index_terms
        ORDER BY scope_id, generation_id, term_key, document_id;

        SELECT count(*)
        INTO diff_count
        FROM (
            (
                SELECT scope_id, generation_id, document_id, term_key, term, term_frequency
                FROM eshu_search_index_terms
                EXCEPT ALL
                SELECT scope_id, generation_id, document_id, term_key, term, term_frequency
                FROM eshu_search_index_terms_shadow
            )
            UNION ALL
            (
                SELECT scope_id, generation_id, document_id, term_key, term, term_frequency
                FROM eshu_search_index_terms_shadow
                EXCEPT ALL
                SELECT scope_id, generation_id, document_id, term_key, term, term_frequency
                FROM eshu_search_index_terms
            )
        ) locked_diff;

        IF diff_count <> 0 THEN
            RAISE EXCEPTION 'eshu_search_index_terms partition cutover diff_count=%', diff_count;
        END IF;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'eshu_search_index_terms'::regclass
          AND conname = 'eshu_search_index_terms_pkey'
    ) THEN
        ALTER TABLE eshu_search_index_terms
            RENAME CONSTRAINT eshu_search_index_terms_pkey
            TO eshu_search_index_terms_unpartitioned_pkey;
    END IF;

    ALTER TABLE eshu_search_index_terms RENAME TO eshu_search_index_terms_unpartitioned;
    ALTER TABLE eshu_search_index_terms_shadow RENAME TO eshu_search_index_terms;
    ALTER TABLE eshu_search_index_terms
        RENAME CONSTRAINT eshu_search_index_terms_shadow_pkey
        TO eshu_search_index_terms_pkey;

    FOR partition_remainder IN 0..63 LOOP
        EXECUTE format(
            'ALTER TABLE eshu_search_index_terms_shadow_p%s RENAME TO eshu_search_index_terms_p%s',
            to_char(partition_remainder, 'FM00'),
            to_char(partition_remainder, 'FM00')
        );
    END LOOP;

    DROP TABLE eshu_search_index_terms_unpartitioned CASCADE;
END $$;
