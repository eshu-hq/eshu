-- Isolated PostgreSQL 18.3 fixture for the 16-term, cap-250 probe contract.
-- Run with psql -X -v ON_ERROR_STOP=1 -f persisted_oracle.sql.
-- For seeded RED runs pass exactly one of bad_scope, bad_match, bad_field,
-- bad_duplicate, bad_path_first as 1. The final assertion must fail.
CREATE TEMP TABLE content_entities (
    entity_id text PRIMARY KEY, repo_id text NOT NULL, relative_path text NOT NULL,
    entity_name text NOT NULL, source_cache text NOT NULL, entity_type text NOT NULL,
    language text, start_line integer NOT NULL, end_line integer NOT NULL
);
CREATE TEMP TABLE content_files (
    repo_id text NOT NULL, relative_path text NOT NULL, content text NOT NULL,
    language text, line_count integer, PRIMARY KEY (repo_id, relative_path)
);
CREATE TEMP TABLE search_terms (term text PRIMARY KEY);
INSERT INTO search_terms
SELECT 'term' || lpad(n::text, 2, '0') FROM generate_series(1, 12) AS n;
INSERT INTO search_terms VALUES ('alpha'), ('a_c'), ('a%c'), (E'back\\slash');
CREATE TEMP TABLE allowed_repos (repo_id text PRIMARY KEY);
INSERT INTO allowed_repos VALUES ('repo-a');
CREATE TEMP TABLE scope_config (language text);
INSERT INTO scope_config VALUES (NULL);
CREATE TEMP TABLE probe (
    source_kind text, matched_term text, repo_id text, relative_path text,
    entity_id text, entity_name text, entity_type text, language text,
    start_line integer, end_line integer
);

-- Source truth comes only from persisted rows. Raw ILIKE preserves SQL wildcard
-- and backslash semantics, including when a term matches both name and source.
CREATE TEMP VIEW eligible_entities AS
SELECT t.term, e.repo_id, e.relative_path, e.entity_id, e.entity_name,
       e.entity_type, coalesce(e.language, '') AS language,
       e.start_line, e.end_line
FROM search_terms t CROSS JOIN content_entities e CROSS JOIN scope_config cfg
WHERE EXISTS (SELECT 1 FROM allowed_repos a WHERE a.repo_id = e.repo_id)
  AND (cfg.language IS NULL OR coalesce(e.language, '') = cfg.language)
  AND (e.entity_name ILIKE '%' || t.term || '%'
       OR e.source_cache ILIKE '%' || t.term || '%');
CREATE TEMP VIEW eligible_files AS
SELECT t.term, f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
       least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line,
       f.relative_path ILIKE '%' || t.term || '%' AS path_match
FROM search_terms t CROSS JOIN content_files f CROSS JOIN scope_config cfg
WHERE EXISTS (SELECT 1 FROM allowed_repos a WHERE a.repo_id = f.repo_id)
  AND (cfg.language IS NULL OR coalesce(f.language, '') = cfg.language)
  AND (f.relative_path ILIKE '%' || t.term || '%'
       OR f.content ILIKE '%' || t.term || '%');

CREATE FUNCTION pg_temp.oracle_errors() RETURNS TABLE(reason text)
LANGUAGE plpgsql AS $$
DECLARE
    search_term text;
    eligible_count integer;
    path_count integer;
    content_count integer;
    actual_count integer;
    actual_path_count integer;
BEGIN
    RETURN QUERY
    SELECT 'unexpected term or kind: ' || coalesce(p.matched_term, '<null>')
    FROM probe p
    WHERE p.source_kind NOT IN ('entity', 'file') OR p.source_kind IS NULL
       OR NOT EXISTS (SELECT 1 FROM search_terms t WHERE t.term = p.matched_term);

    RETURN QUERY
    SELECT 'duplicate identity: ' || coalesce(p.matched_term, '<null>')
    FROM probe p
    GROUP BY p.source_kind, p.matched_term, p.repo_id,
             CASE WHEN p.source_kind = 'entity' THEN p.entity_id ELSE p.relative_path END
    HAVING count(*) > 1;

    RETURN QUERY
    SELECT 'ineligible or malformed entity: ' || coalesce(p.entity_id, '<null>')
    FROM probe p LEFT JOIN eligible_entities e
      ON e.term = p.matched_term AND e.entity_id = p.entity_id
    WHERE p.source_kind = 'entity' AND (
      e.entity_id IS NULL OR p.repo_id IS DISTINCT FROM e.repo_id
      OR p.relative_path IS DISTINCT FROM e.relative_path
      OR p.entity_name IS DISTINCT FROM e.entity_name
      OR p.entity_type IS DISTINCT FROM e.entity_type
      OR p.language IS DISTINCT FROM e.language
      OR p.start_line IS DISTINCT FROM e.start_line
      OR p.end_line IS DISTINCT FROM e.end_line);

    RETURN QUERY
    SELECT 'ineligible or malformed file: ' || coalesce(p.relative_path, '<null>')
    FROM probe p LEFT JOIN eligible_files f
      ON f.term = p.matched_term AND f.repo_id = p.repo_id
     AND f.relative_path = p.relative_path
    WHERE p.source_kind = 'file' AND (
      f.relative_path IS NULL OR p.entity_id IS DISTINCT FROM ''
      OR p.entity_name IS DISTINCT FROM '' OR p.entity_type IS DISTINCT FROM ''
      OR p.language IS DISTINCT FROM f.language
      OR p.start_line IS DISTINCT FROM 1
      OR p.end_line IS DISTINCT FROM f.end_line);

    FOR search_term IN SELECT t.term FROM search_terms t LOOP
      SELECT count(*) INTO eligible_count FROM eligible_entities e
      WHERE e.term = search_term;
      SELECT count(*) INTO actual_count FROM probe p
      WHERE p.source_kind = 'entity' AND p.matched_term = search_term;
      IF actual_count <> least(eligible_count, 250) THEN
        reason := 'entity cardinality: ' || search_term;
        RETURN NEXT;
      END IF;
      IF eligible_count < 250 AND EXISTS (
        SELECT e.entity_id FROM eligible_entities e WHERE e.term = search_term
        EXCEPT SELECT p.entity_id FROM probe p
        WHERE p.source_kind = 'entity' AND p.matched_term = search_term
      ) THEN
        reason := 'uncapped entity omission: ' || search_term;
        RETURN NEXT;
      END IF;

      SELECT count(*) FILTER (WHERE f.path_match),
             count(*) FILTER (WHERE NOT f.path_match)
      INTO path_count, content_count
      FROM eligible_files f WHERE f.term = search_term;
      SELECT count(*), count(*) FILTER (WHERE f.path_match)
      INTO actual_count, actual_path_count
      FROM probe p LEFT JOIN eligible_files f
        ON f.term = p.matched_term AND f.repo_id = p.repo_id
       AND f.relative_path = p.relative_path
      WHERE p.source_kind = 'file' AND p.matched_term = search_term;
      IF actual_count <> least(path_count + content_count, 250)
         OR actual_path_count <> least(path_count, 250) THEN
        reason := 'file path-first allocation: ' || search_term;
        RETURN NEXT;
      END IF;
      IF path_count < 250 AND EXISTS (
        SELECT f.repo_id, f.relative_path FROM eligible_files f
        WHERE f.term = search_term AND f.path_match
        EXCEPT SELECT p.repo_id, p.relative_path FROM probe p
        WHERE p.source_kind = 'file' AND p.matched_term = search_term
      ) THEN
        reason := 'uncapped path omission: ' || search_term;
        RETURN NEXT;
      END IF;
      IF path_count + content_count < 250 AND EXISTS (
        SELECT f.repo_id, f.relative_path FROM eligible_files f
        WHERE f.term = search_term AND NOT f.path_match
        EXCEPT SELECT p.repo_id, p.relative_path FROM probe p
        WHERE p.source_kind = 'file' AND p.matched_term = search_term
      ) THEN
        reason := 'uncapped content omission: ' || search_term;
        RETURN NEXT;
      END IF;
    END LOOP;
END;
$$;

-- Empty state is a real contract case, before any persisted content exists.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_temp.oracle_errors()) THEN
    RAISE EXCEPTION 'empty fixture failed';
  END IF;
END $$;

-- Entity boundaries: 249, 250, 251. Every row has a distinct persisted ID.
INSERT INTO content_entities
SELECT 'e01-' || n, 'repo-a', 'src/e01-' || n || '.go', 'term01', '',
       'Function', NULL, 3, 9 FROM generate_series(1, 249) AS n;
INSERT INTO content_entities
SELECT 'e02-' || n, 'repo-a', 'src/e02-' || n || '.go', '', 'term02',
       'Function', 'go', 1, 2 FROM generate_series(1, 250) AS n;
INSERT INTO content_entities
SELECT 'e03-' || n, 'repo-a', 'src/e03-' || n || '.go', 'term03', 'term03',
       'Function', 'go', 1, 2 FROM generate_series(1, 251) AS n;
INSERT INTO content_entities VALUES
  ('overlap', 'repo-a', 'src/overlap.go', 'alpha', 'alpha', 'Function', NULL, 2, 4),
  ('wild-underscore', 'repo-a', 'src/wild.go', 'abc', '', 'Function', NULL, 1, 1),
  ('wild-percent', 'repo-a', 'src/percent.go', 'axxxc', '', 'Function', NULL, 1, 1),
  ('slash', 'repo-a', 'src/slash.go', E'back\\slash', '', 'Function', NULL, 1, 1),
  ('slash-pattern', 'repo-a', 'src/slash-pattern.go', 'backslash', '', 'Function', NULL, 1, 1),
  ('other-repo', 'repo-b', 'src/other.go', 'alpha', '', 'Function', 'go', 1, 1);

-- Files: 249 paths plus content, 250 paths, 251 paths; path hits also contain
-- the term in content so the OR and path-first de-duplication are exercised.
INSERT INTO content_files
SELECT 'repo-a', 'term04/path-' || n, 'term04', NULL, 0
FROM generate_series(1, 249) AS n;
INSERT INTO content_files VALUES
  ('repo-a', 'content-only-04-a', 'term04', 'go', 200),
  ('repo-a', 'content-only-04-b', 'term04', NULL, NULL);
INSERT INTO content_files
SELECT 'repo-a', 'term05/path-' || n, 'term05', 'go', 81
FROM generate_series(1, 250) AS n;
INSERT INTO content_files VALUES ('repo-a', 'content-only-05', 'term05', 'go', 1);
INSERT INTO content_files
SELECT 'repo-a', 'term06/path-' || n, 'term06', 'go', 1
FROM generate_series(1, 251) AS n;
INSERT INTO content_files VALUES
  ('repo-a', 'wild-file-a_c', '', NULL, -1),
  ('repo-a', 'wild-file-axxxc', '', NULL, 1),
  ('repo-a', 'slash-file', E'back\\slash', NULL, 1),
  ('repo-b', 'other-alpha-file', 'alpha', 'go', 1);

-- A deterministic candidate makes the oracle independent of LIMIT's
-- unspecified choice among eligible rows at a filled cap.
INSERT INTO probe
SELECT 'entity', term, repo_id, relative_path, entity_id, entity_name,
       entity_type, language, start_line, end_line
FROM (SELECT e.*, row_number() OVER (PARTITION BY term ORDER BY entity_id) AS rn
      FROM eligible_entities e) ranked WHERE rn <= 250;
INSERT INTO probe
SELECT 'file', term, repo_id, relative_path, '', '', '', language, 1, end_line
FROM (SELECT f.*, row_number() OVER
      (PARTITION BY term ORDER BY NOT path_match, repo_id, relative_path) AS rn
      FROM eligible_files f) ranked WHERE rn <= 250;

-- Pin fixture shape so a seed typo cannot make the oracle false green.
DO $$ BEGIN
  IF (SELECT count(*) FROM eligible_entities WHERE term = 'term01') <> 249
     OR (SELECT count(*) FROM eligible_entities WHERE term = 'term02') <> 250
     OR (SELECT count(*) FROM eligible_entities WHERE term = 'term03') <> 251
     OR (SELECT count(*) FROM probe WHERE source_kind = 'entity'
         AND matched_term = 'term03') <> 250
     OR (SELECT count(*) FROM eligible_files WHERE term = 'term04'
         AND path_match) <> 249
     OR (SELECT count(*) FROM eligible_files WHERE term = 'term05'
         AND path_match) <> 250
     OR (SELECT count(*) FROM eligible_files WHERE term = 'term06'
         AND path_match) <> 251
     OR (SELECT count(*) FROM probe WHERE source_kind = 'file'
         AND matched_term = 'term04') <> 250
     OR (SELECT count(*) FROM eligible_entities WHERE term = 'alpha'
         AND entity_id = 'overlap') <> 1
     OR NOT EXISTS (SELECT 1 FROM eligible_entities WHERE term = 'a_c'
                    AND entity_id = 'wild-underscore')
     OR NOT EXISTS (SELECT 1 FROM eligible_entities WHERE term = 'a%c'
                    AND entity_id = 'wild-percent')
     OR NOT EXISTS (SELECT 1 FROM eligible_entities WHERE term = E'back\\slash'
                    AND entity_id = 'slash-pattern')
     OR EXISTS (SELECT 1 FROM eligible_entities WHERE term = E'back\\slash'
                AND entity_id = 'slash') THEN
    RAISE EXCEPTION 'seed boundary or raw ILIKE assertion failed';
  END IF;
END $$;

\if :{?bad_scope}
INSERT INTO probe VALUES
  ('entity', 'alpha', 'repo-b', 'src/other.go', 'other-repo', 'alpha',
   'Function', 'go', 1, 1);
\endif
\if :{?bad_match}
INSERT INTO probe VALUES
  ('entity', 'term12', 'repo-a', 'src/overlap.go', 'overlap', 'alpha',
   'Function', '', 2, 4);
\endif
\if :{?bad_field}
UPDATE probe SET language = 'wrong' WHERE entity_id = 'overlap';
\endif
\if :{?bad_duplicate}
INSERT INTO probe SELECT * FROM probe WHERE entity_id = 'overlap' LIMIT 1;
\endif
\if :{?bad_path_first}
DELETE FROM probe WHERE source_kind = 'file' AND matched_term = 'term04'
  AND relative_path = 'term04/path-1';
INSERT INTO probe VALUES
  ('file', 'term04', 'repo-a', 'content-only-04-b', '', '', '', '', 1, 1);
\endif

DO $$ DECLARE errors text; BEGIN
  SELECT string_agg(reason, '; ' ORDER BY reason) INTO errors
  FROM pg_temp.oracle_errors();
  IF errors IS NOT NULL THEN
    RAISE EXCEPTION 'oracle rejected candidate: %', errors;
  END IF;
END $$;
SELECT 'ORACLE_GREEN' AS result,
       (SELECT count(*) FROM search_terms) AS terms,
       (SELECT count(*) FROM probe) AS candidate_rows;

-- Re-evaluate the persisted rows under an explicit language filter. Nullable
-- languages must be excluded, while projected output still coalesces NULL to ''.
UPDATE scope_config SET language = 'go';
TRUNCATE probe;
INSERT INTO probe
SELECT 'entity', term, repo_id, relative_path, entity_id, entity_name,
       entity_type, language, start_line, end_line
FROM (SELECT e.*, row_number() OVER (PARTITION BY term ORDER BY entity_id) AS rn
      FROM eligible_entities e) ranked WHERE rn <= 250;
INSERT INTO probe
SELECT 'file', term, repo_id, relative_path, '', '', '', language, 1, end_line
FROM (SELECT f.*, row_number() OVER
      (PARTITION BY term ORDER BY NOT path_match, repo_id, relative_path) AS rn
      FROM eligible_files f) ranked WHERE rn <= 250;
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_temp.oracle_errors())
     OR EXISTS (SELECT 1 FROM probe WHERE language <> 'go')
     OR NOT EXISTS (SELECT 1 FROM probe WHERE matched_term = 'term02') THEN
    RAISE EXCEPTION 'language-filtered oracle failed';
  END IF;
END $$;
SELECT 'ORACLE_LANGUAGE_GREEN' AS result, count(*) AS candidate_rows FROM probe;

-- A scoped caller with no grants cannot receive any persisted row.
TRUNCATE allowed_repos, probe;
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM eligible_entities)
     OR EXISTS (SELECT 1 FROM eligible_files)
     OR EXISTS (SELECT 1 FROM pg_temp.oracle_errors()) THEN
    RAISE EXCEPTION 'empty-grant oracle failed';
  END IF;
END $$;
SELECT 'ORACLE_EMPTY_GRANT_GREEN' AS result;
