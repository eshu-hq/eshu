
		WITH terms(term) AS (
		  VALUES ($2), ($3), ($4), ($5), ($6), ($7), ($8), ($9), ($10), ($11), ($12), ($13), ($14), ($15), ($16), ($17)
		),
		entity_probe AS (
		  SELECT terms.term AS matched_term, m.repo_id, m.relative_path, m.entity_id,
		         m.entity_name, m.entity_type, m.language, m.start_line, m.end_line
		  FROM terms
		  CROSS JOIN LATERAL (
		    SELECT e.repo_id, e.relative_path, e.entity_id, e.entity_name, e.entity_type,
		           coalesce(e.language, '') AS language, e.start_line, e.end_line
		    FROM content_entities e
		    WHERE (e.entity_name ILIKE '%' || terms.term || '%'
		           OR e.source_cache ILIKE '%' || terms.term || '%')
		    AND repo_id = $1
		    LIMIT 250
		  ) m
		),
		entity_matches AS (
		  SELECT 'entity' AS source_kind, repo_id, relative_path, entity_id, entity_name,
		         entity_type, language, start_line, end_line,
		         string_agg(DISTINCT matched_term, E'\x1f' ORDER BY matched_term) AS matched_terms,
		         count(DISTINCT matched_term)::int AS score
		  FROM entity_probe
		  GROUP BY repo_id, relative_path, entity_id, entity_name, entity_type,
		           language, start_line, end_line
		),
		entity_pool_capped AS (
		  SELECT coalesce(bool_or(term_count >= 250), false) AS capped
		  FROM (SELECT matched_term, count(*) AS term_count FROM entity_probe GROUP BY matched_term) t
		),
		file_probe AS (
		  (
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $2 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $2 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $2 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $2 || '%'
		      AND f.relative_path NOT ILIKE '%' || $2 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $3 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $3 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $3 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $3 || '%'
		      AND f.relative_path NOT ILIKE '%' || $3 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $4 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $4 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $4 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $4 || '%'
		      AND f.relative_path NOT ILIKE '%' || $4 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $5 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $5 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $5 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $5 || '%'
		      AND f.relative_path NOT ILIKE '%' || $5 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $6 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $6 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $6 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $6 || '%'
		      AND f.relative_path NOT ILIKE '%' || $6 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $7 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $7 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $7 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $7 || '%'
		      AND f.relative_path NOT ILIKE '%' || $7 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $8 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $8 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $8 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $8 || '%'
		      AND f.relative_path NOT ILIKE '%' || $8 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $9 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $9 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $9 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $9 || '%'
		      AND f.relative_path NOT ILIKE '%' || $9 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $10 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $10 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $10 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $10 || '%'
		      AND f.relative_path NOT ILIKE '%' || $10 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $11 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $11 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $11 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $11 || '%'
		      AND f.relative_path NOT ILIKE '%' || $11 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $12 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $12 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $12 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $12 || '%'
		      AND f.relative_path NOT ILIKE '%' || $12 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $13 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $13 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $13 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $13 || '%'
		      AND f.relative_path NOT ILIKE '%' || $13 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $14 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $14 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $14 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $14 || '%'
		      AND f.relative_path NOT ILIKE '%' || $14 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $15 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $15 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $15 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $15 || '%'
		      AND f.relative_path NOT ILIKE '%' || $15 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $16 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $16 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $16 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $16 || '%'
		      AND f.relative_path NOT ILIKE '%' || $16 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		  UNION ALL
(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $17 AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%' || $17 || '%'
		    AND repo_id = $1 LIMIT 250
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $17 AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%' || $17 || '%'
		      AND f.relative_path NOT ILIKE '%' || $17 || '%'
		    AND repo_id = $1 LIMIT (SELECT 250 - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)
		),
		file_matches AS (
		  SELECT 'file' AS source_kind, repo_id, relative_path, '' AS entity_id,
		         '' AS entity_name, '' AS entity_type, language, 1 AS start_line, end_line,
		         string_agg(DISTINCT matched_term, E'\x1f' ORDER BY matched_term) AS matched_terms,
		         count(DISTINCT matched_term)::int AS score
		  FROM file_probe
		  GROUP BY repo_id, relative_path, language, end_line
		),
		file_pool_capped AS (
		  SELECT coalesce(bool_or(term_count >= 250), false) AS capped
		  FROM (SELECT matched_term, count(*) AS term_count FROM file_probe GROUP BY matched_term) t
		),
		pool_status AS (
		  SELECT (SELECT capped FROM entity_pool_capped) OR (SELECT capped FROM file_pool_capped) AS capped
		)
		SELECT source_kind, repo_id, relative_path, entity_id, entity_name,
		       entity_type, language, start_line, end_line, matched_terms, score,
		       pool_status.capped AS pool_truncated
		FROM (
		  SELECT * FROM entity_matches
		  UNION ALL
		  SELECT * FROM file_matches
		) matches
		CROSS JOIN pool_status
		ORDER BY score DESC, repo_id, relative_path, entity_name, source_kind
		LIMIT $18 OFFSET $19
