package main

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
	"github.com/jackc/pgx/v5"
)

func deterministicProbeSQL(oracle bool) string {
	const columns = `e.repo_id, e.relative_path, e.entity_id, e.entity_name,
		e.entity_type, coalesce(e.language, '') AS language, e.start_line, e.end_line`
	entity := fmt.Sprintf(`SELECT %[1]s FROM content_entities e
		WHERE (e.entity_name ILIKE '%%' || $1 || '%%'
		       OR e.source_cache ILIKE '%%' || $1 || '%%')
		  AND eshu_require_content_substring_indexes_ready()
		ORDER BY e.entity_id LIMIT %[2]d`, columns, candidateCap)
	if !oracle {
		entity = fmt.Sprintf(`WITH name_pool AS MATERIALIZED (
		  SELECT %[1]s FROM content_entities e
		  WHERE e.entity_name ILIKE '%%' || $1 || '%%'
		    AND eshu_require_content_substring_indexes_ready()
		  ORDER BY e.entity_id LIMIT %[2]d
		), source_only AS MATERIALIZED (
		  SELECT %[1]s FROM content_entities e
		  WHERE e.source_cache ILIKE '%%' || $1 || '%%'
		    AND e.entity_name NOT ILIKE '%%' || $1 || '%%'
		    AND eshu_require_content_substring_indexes_ready()
		  ORDER BY e.entity_id LIMIT %[2]d
		)
		SELECT * FROM (SELECT * FROM name_pool UNION ALL SELECT * FROM source_only) matched
		ORDER BY entity_id LIMIT %[2]d`, columns, candidateCap)
	}
	file := fmt.Sprintf(`WITH path_pool AS MATERIALIZED (
		  SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		         least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line
		  FROM content_files f
		  WHERE f.relative_path ILIKE '%%' || $1 || '%%'
		    AND eshu_require_content_substring_indexes_ready()
		  ORDER BY f.repo_id, f.relative_path LIMIT %[1]d
		), content_pool AS MATERIALIZED (
		  SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		         least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line
		  FROM content_files f
		  WHERE f.content ILIKE '%%' || $1 || '%%'
		    AND f.relative_path NOT ILIKE '%%' || $1 || '%%'
		    AND eshu_require_content_substring_indexes_ready()
		  ORDER BY f.repo_id, f.relative_path
		  LIMIT (SELECT %[1]d - count(*) FROM path_pool)
		)
		SELECT * FROM path_pool UNION ALL SELECT * FROM content_pool`, candidateCap)
	return fmt.Sprintf(`SELECT 'entity' AS source_kind, $1::text AS matched_term,
		e.repo_id, e.relative_path, e.entity_id, e.entity_name, e.entity_type,
		e.language, e.start_line, e.end_line FROM (%s) e
		UNION ALL
		SELECT 'file' AS source_kind, $1::text AS matched_term,
		f.repo_id, f.relative_path, '' AS entity_id, '' AS entity_name,
		'' AS entity_type, f.language, 1 AS start_line, f.end_line
		FROM (%s) f`, entity, file)
}

func readDeterministicTerm(ctx context.Context, tx pgx.Tx, sql, term string) ([]codetopicparallel.ProbeRow, time.Duration, error) {
	started := time.Now()
	result, err := tx.Query(ctx, sql, term)
	if err != nil {
		return nil, time.Since(started), err
	}
	defer result.Close()
	rows := make([]codetopicparallel.ProbeRow, 0, 500)
	for result.Next() {
		var row codetopicparallel.ProbeRow
		if err := result.Scan(&row.SourceKind, &row.MatchedTerm, &row.RepoID, &row.RelativePath,
			&row.EntityID, &row.EntityName, &row.EntityType, &row.Language,
			&row.StartLine, &row.EndLine); err != nil {
			return nil, time.Since(started), err
		}
		rows = append(rows, row)
	}
	if err := result.Err(); err != nil {
		return nil, time.Since(started), err
	}
	return rows, time.Since(started), nil
}

func runDeterministicScreen(ctx context.Context, conn *pgx.Conn) error {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin deterministic snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '5s'"); err != nil {
		return fmt.Errorf("bound deterministic statement: %w", err)
	}
	started := time.Now()
	oracleSQL := deterministicProbeSQL(true)
	candidateSQL := deterministicProbeSQL(false)
	oracleRows := make([]codetopicparallel.ProbeRow, 0, 8000)
	candidateRows := make([]codetopicparallel.ProbeRow, 0, 8000)
	for i, term := range terms {
		oraclePart, oracleWall, readErr := readDeterministicTerm(ctx, tx, oracleSQL, term)
		if readErr != nil {
			return fmt.Errorf("term %d oracle: %w", i, readErr)
		}
		candidatePart, candidateWall, readErr := readDeterministicTerm(ctx, tx, candidateSQL, term)
		if readErr != nil {
			return fmt.Errorf("term %d candidate: %w", i, readErr)
		}
		oracleRows = append(oracleRows, oraclePart...)
		candidateRows = append(candidateRows, candidatePart...)
		fmt.Printf("deterministic_term=%d oracle_rows=%d candidate_rows=%d oracle_ms=%.3f candidate_ms=%.3f\n",
			i, len(oraclePart), len(candidatePart), float64(oracleWall.Microseconds())/1000,
			float64(candidateWall.Microseconds())/1000)
	}
	leftOnly, rightOnly, err := exactDifference(ctx, tx, oracleRows, candidateRows)
	if err != nil {
		return err
	}
	oraclePage, err := assemblyHash(ctx, tx, oracleRows)
	if err != nil {
		return err
	}
	candidatePage, err := assemblyHash(ctx, tx, candidateRows)
	if err != nil {
		return err
	}
	fmt.Printf("deterministic_accuracy oracle_rows=%d candidate_rows=%d left_only=%d right_only=%d page_equal=%t age_ms=%d\n",
		len(oracleRows), len(candidateRows), leftOnly, rightOnly, oraclePage == candidatePage,
		time.Since(started).Milliseconds())
	if leftOnly != 0 || rightOnly != 0 || oraclePage != candidatePage {
		return fmt.Errorf("deterministic candidate differs from oracle")
	}
	baselineRows, _, err := readGroups(ctx, tx, 4, false)
	if err != nil {
		return err
	}
	oldOnly, newOnly, err := exactDifference(ctx, tx, baselineRows, candidateRows)
	if err != nil {
		return err
	}
	baselinePage, err := assemblyHash(ctx, tx, baselineRows)
	if err != nil {
		return err
	}
	fmt.Printf("deterministic_behavior_delta shipped_rows=%d candidate_rows=%d old_only=%d new_only=%d page_equal=%t age_ms=%d\n",
		len(baselineRows), len(candidateRows), oldOnly, newOnly, baselinePage == candidatePage,
		time.Since(started).Milliseconds())
	return nil
}
