CREATE TABLE IF NOT EXISTS eshu_search_index_documents (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    fact_id TEXT NOT NULL,
    repo_id TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    content_hash TEXT NOT NULL DEFAULT '',
    document JSONB NOT NULL,
    document_length INTEGER NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, document_id)
);

ALTER TABLE eshu_search_index_documents
    ADD COLUMN IF NOT EXISTS content_hash TEXT NOT NULL DEFAULT '';

UPDATE eshu_search_index_documents doc
SET content_hash = fact.payload->>'content_hash'
FROM fact_records fact
WHERE doc.content_hash = ''
  AND fact.fact_id = doc.fact_id
  AND fact.payload ? 'content_hash';

CREATE TABLE IF NOT EXISTS eshu_search_index_terms (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    term_key TEXT NOT NULL,
    term TEXT NOT NULL,
    term_frequency INTEGER NOT NULL
) PARTITION BY HASH (scope_id);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_class c
        WHERE c.oid = to_regclass('eshu_search_index_terms')
          AND c.relkind = 'p'
    ) THEN
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p00 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 0);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p01 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 1);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p02 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 2);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p03 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 3);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p04 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 4);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p05 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 5);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p06 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 6);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p07 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 7);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p08 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 8);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p09 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 9);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p10 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 10);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p11 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 11);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p12 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 12);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p13 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 13);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p14 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 14);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p15 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 15);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p16 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 16);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p17 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 17);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p18 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 18);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p19 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 19);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p20 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 20);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p21 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 21);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p22 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 22);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p23 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 23);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p24 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 24);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p25 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 25);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p26 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 26);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p27 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 27);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p28 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 28);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p29 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 29);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p30 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 30);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p31 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 31);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p32 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 32);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p33 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 33);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p34 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 34);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p35 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 35);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p36 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 36);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p37 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 37);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p38 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 38);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p39 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 39);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p40 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 40);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p41 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 41);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p42 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 42);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p43 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 43);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p44 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 44);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p45 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 45);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p46 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 46);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p47 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 47);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p48 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 48);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p49 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 49);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p50 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 50);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p51 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 51);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p52 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 52);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p53 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 53);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p54 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 54);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p55 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 55);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p56 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 56);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p57 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 57);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p58 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 58);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p59 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 59);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p60 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 60);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p61 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 61);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p62 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 62);
        CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p63 PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER 63);
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS eshu_search_index_stats (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_count INTEGER NOT NULL,
    average_document_length DOUBLE PRECISION NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);

ALTER TABLE eshu_search_index_terms
    ADD COLUMN IF NOT EXISTS term_key TEXT;

WITH stale_index_generations AS (
    SELECT DISTINCT scope_id, generation_id
    FROM eshu_search_index_terms
    WHERE term_key IS NULL OR term_key = ''
)
DELETE FROM eshu_search_index_stats stats
USING stale_index_generations stale
WHERE stats.scope_id = stale.scope_id
  AND stats.generation_id = stale.generation_id;

DELETE FROM eshu_search_index_terms
WHERE term_key IS NULL OR term_key = '';

ALTER TABLE eshu_search_index_terms
    ALTER COLUMN term_key SET NOT NULL;

ALTER TABLE eshu_search_index_terms
    DROP CONSTRAINT IF EXISTS eshu_search_index_terms_pkey;

ALTER TABLE eshu_search_index_terms
    ADD CONSTRAINT eshu_search_index_terms_pkey
    PRIMARY KEY (scope_id, generation_id, term_key, document_id);

CREATE INDEX IF NOT EXISTS eshu_search_index_documents_repo_idx
    ON eshu_search_index_documents (scope_id, generation_id, repo_id, source_kind);
