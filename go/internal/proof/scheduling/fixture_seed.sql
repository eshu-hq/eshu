-- Isolated PostgreSQL 18 integration fixture. Not a performance corpus.
\set ON_ERROR_STOP on

CREATE TABLE content_entities (
    entity_id text PRIMARY KEY,
    repo_id text NOT NULL,
    relative_path text NOT NULL,
    entity_type text NOT NULL,
    entity_name text NOT NULL,
    language text,
    start_line integer NOT NULL,
    end_line integer NOT NULL,
    source_cache text NOT NULL
);

CREATE TABLE content_files (
    repo_id text NOT NULL,
    relative_path text NOT NULL,
    content text NOT NULL,
    language text,
    line_count integer NOT NULL,
    PRIMARY KEY (repo_id, relative_path)
);

CREATE FUNCTION eshu_require_content_substring_indexes_ready()
RETURNS boolean LANGUAGE sql IMMUTABLE AS $$ SELECT true $$;

-- 251 eligible entities: the 250-row cap permits different valid subsets.
INSERT INTO content_entities
SELECT 'entity-config-' || g, 'repo-a', 'src/config-' || g || '.go',
       'Function', 'config', CASE WHEN g % 2 = 0 THEN 'go' ELSE NULL END,
       1, 2, 'config'
FROM generate_series(1, 251) AS g;

-- Path-first split at 249 + 2; second term has exactly 250 path rows.
INSERT INTO content_files
SELECT 'repo-a', 'src/content-' || g || '.go', 'neutral', 'go', 3
FROM generate_series(1, 249) AS g;
INSERT INTO content_files VALUES
('repo-a', 'src/neutral-a.go', 'content', NULL, 0),
('repo-a', 'src/neutral-b.go', 'content', 'go', 120),
('repo-b', 'other/content-third.go', 'neutral', 'go', 1);

INSERT INTO content_files
SELECT 'repo-a', 'src/path-' || g || '.go', 'neutral', NULL, 2
FROM generate_series(1, 250) AS g;

INSERT INTO content_files
SELECT 'repo-a', 'src/file-' || g || '.go', 'neutral', 'go', 2
FROM generate_series(1, 251) AS g;

-- One row for each remaining canonical term, including an overlapping match.
INSERT INTO content_entities
SELECT 'entity-' || term, 'repo-b', 'src/' || term || '.go',
       'Function', term, NULL, 1, 1, term
FROM unnest(ARRAY[
    'deployment', 'environment', 'function', 'handler', 'module',
    'package', 'repo', 'repository', 'resource', 'service',
    'source', 'system'
]) AS term;

ANALYZE content_entities;
ANALYZE content_files;

SELECT 'FIXTURE_READY' AS status,
       (SELECT count(*) FROM content_entities) AS entities,
       (SELECT count(*) FROM content_files) AS files;
