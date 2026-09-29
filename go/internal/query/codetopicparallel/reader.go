// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codetopicparallel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Partitions is the measured upper bound for concurrent topic probe sessions.
const Partitions = 4

// ProbeRow retains nullable SQL columns when moving bounded probe
// rows between snapshot-sharing readers and PostgreSQL's final aggregation.
type ProbeRow struct {
	SourceKind   string  `json:"source_kind"`
	MatchedTerm  string  `json:"matched_term"`
	RepoID       *string `json:"repo_id"`
	RelativePath *string `json:"relative_path"`
	EntityID     *string `json:"entity_id"`
	EntityName   *string `json:"entity_name"`
	EntityType   *string `json:"entity_type"`
	Language     *string `json:"language"`
	StartLine    *int64  `json:"start_line"`
	EndLine      *int64  `json:"end_line"`
}

// Eligible bounds parallel reads to the measured sixteen-term query and a pool
// that can provide four independent sessions.
func Eligible(termCount, maxOpenConns int) bool {
	return termCount == 16 && (maxOpenConns == 0 || maxOpenConns >= Partitions)
}

// FileBranch is shared by the serial and partitioned probe builders.
// Every placeholder binds the same raw term, retaining PostgreSQL ILIKE rules.
func FileBranch(termArg int, where string, candidateCap int) string {
	// #nosec G201 -- termArg and candidateCap are integers; where contains only
	// static predicates and numbered placeholders built by codeTopicFilters.
	return fmt.Sprintf(`(
		  WITH path_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $%[1]d AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%%' || $%[1]d || '%%'
		    %[2]s LIMIT %[3]d
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $%[1]d AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%%' || $%[1]d || '%%'
		      AND f.relative_path NOT ILIKE '%%' || $%[1]d || '%%'
		    %[2]s LIMIT (SELECT %[3]d - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)`, termArg, where, candidateCap)
}

// ProbeSQL builds the exact per-term candidate read for one partition.
// The caller supplies grant and language filters produced by codeTopicFilters.
func ProbeSQL(req codequery.CodeTopicInvestigationRequest, candidateCap int, filters []string, baseArgs []any) (string, []any) {
	args := append([]any(nil), baseArgs...)
	nextArg := len(args) + 1
	where := ""
	if len(filters) > 0 {
		where = "AND " + strings.Join(filters, " AND ")
	}
	terms := make([]string, len(req.Terms))
	branches := make([]string, len(req.Terms))
	for i, term := range req.Terms {
		terms[i] = fmt.Sprintf("($%d)", nextArg)
		branches[i] = FileBranch(nextArg, where, candidateCap)
		args = append(args, term)
		nextArg++
	}
	// #nosec G201 -- generated placeholders, fixed SQL, and integer cap only.
	query := fmt.Sprintf(`
		WITH terms(term) AS (VALUES %[1]s),
		entity_probe AS (
		  SELECT terms.term AS matched_term, m.repo_id, m.relative_path, m.entity_id,
		         m.entity_name, m.entity_type, m.language, m.start_line, m.end_line
		  FROM terms CROSS JOIN LATERAL (
		    SELECT e.repo_id, e.relative_path, e.entity_id, e.entity_name, e.entity_type,
		           coalesce(e.language, '') AS language, e.start_line, e.end_line
		    FROM content_entities e
		    WHERE (e.entity_name ILIKE '%%' || terms.term || '%%'
		           OR e.source_cache ILIKE '%%' || terms.term || '%%')
		    %[2]s LIMIT %[3]d
		  ) m
		), file_probe AS (%[4]s)
		SELECT 'entity' AS source_kind, matched_term, repo_id, relative_path,
		       entity_id, entity_name, entity_type, language, start_line, end_line
		FROM entity_probe
		UNION ALL
		SELECT 'file' AS source_kind, matched_term, repo_id, relative_path,
		       '' AS entity_id, '' AS entity_name, '' AS entity_type, language, 1 AS start_line, end_line
		FROM file_probe`, strings.Join(terms, ", "), where, candidateCap,
		strings.Join(branches, "\n\t\tUNION ALL\n"))
	return query, args
}

// AssemblySQL delegates grouping, collation, cap detection, and page ordering
// to PostgreSQL so the result matches the single-statement read contract.
func AssemblySQL(candidateCap int) string {
	// #nosec G201 -- candidateCap is an internally computed integer.
	return fmt.Sprintf(`
		WITH probe AS MATERIALIZED (
		  SELECT * FROM jsonb_to_recordset($1::jsonb) AS p(
		    source_kind text, matched_term text, repo_id text, relative_path text,
		    entity_id text, entity_name text, entity_type text, language text,
		    start_line integer, end_line integer)
		), entity_matches AS (
		  SELECT 'entity' AS source_kind, repo_id, relative_path, entity_id, entity_name,
		         entity_type, language, start_line, end_line,
		         string_agg(DISTINCT matched_term, E'\x1f' ORDER BY matched_term) AS matched_terms,
		         count(DISTINCT matched_term)::int AS score
		  FROM probe WHERE source_kind = 'entity'
		  GROUP BY repo_id, relative_path, entity_id, entity_name, entity_type,
		           language, start_line, end_line
		), file_matches AS (
		  SELECT 'file' AS source_kind, repo_id, relative_path, '' AS entity_id,
		         '' AS entity_name, '' AS entity_type, language, 1 AS start_line, end_line,
		         string_agg(DISTINCT matched_term, E'\x1f' ORDER BY matched_term) AS matched_terms,
		         count(DISTINCT matched_term)::int AS score
		  FROM probe WHERE source_kind = 'file'
		  GROUP BY repo_id, relative_path, language, end_line
		), pool_status AS (
		  SELECT coalesce(bool_or(term_count >= %[1]d), false) AS capped
		  FROM (SELECT source_kind, matched_term, count(*) AS term_count
		        FROM probe GROUP BY source_kind, matched_term) counts
		)
		SELECT source_kind, repo_id, relative_path, entity_id, entity_name,
		       entity_type, language, start_line, end_line, matched_terms, score,
		       pool_status.capped AS pool_truncated
		FROM (SELECT * FROM entity_matches UNION ALL SELECT * FROM file_matches) matches
		CROSS JOIN pool_status
		ORDER BY score DESC, repo_id, relative_path, entity_name, source_kind
		LIMIT $2 OFFSET $3`, candidateCap)
}

// RunPartitions cancels sibling reads on failure and joins all sessions before
// returning so their transactions can release connections.
func RunPartitions(ctx context.Context, count int, run func(context.Context, int) ([]ProbeRow, error)) ([][]ProbeRow, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([][]ProbeRow, count)
	errorsByIndex := make([]error, count)
	var workers sync.WaitGroup
	for i := range results {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			results[index], errorsByIndex[index] = run(ctx, index)
			if errorsByIndex[index] != nil {
				cancel()
			}
		}(i)
	}
	workers.Wait()
	for _, err := range errorsByIndex {
		if err != nil && !errors.Is(err, context.Canceled) {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func scanCodeTopicProbeRows(rows *sql.Rows) ([]ProbeRow, error) {
	var results []ProbeRow
	for rows.Next() {
		var row ProbeRow
		if err := rows.Scan(&row.SourceKind, &row.MatchedTerm, &row.RepoID, &row.RelativePath,
			&row.EntityID, &row.EntityName, &row.EntityType, &row.Language,
			&row.StartLine, &row.EndLine); err != nil {
			return nil, fmt.Errorf("scan code topic probe: %w", err)
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read code topic probe: %w", err)
	}
	return results, nil
}

func readCodeTopicProbe(ctx context.Context, tx *sql.Tx, req codequery.CodeTopicInvestigationRequest, cap int, filters []string, baseArgs []any) ([]ProbeRow, error) {
	query, args := ProbeSQL(req, cap, filters, baseArgs)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("code topic probe: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanCodeTopicProbeRows(rows)
}

// Investigate reads four per-term partitions on one exported snapshot, then
// asks PostgreSQL to assemble the ordered page with the original SQL rules.
func Investigate(ctx context.Context, db *sql.DB, span trace.Span, req codequery.CodeTopicInvestigationRequest, cap int, filters []string, baseArgs []any, scan func(*sql.Rows) ([]codequery.CodeTopicEvidenceRow, bool, error)) ([]codequery.CodeTopicEvidenceRow, error) {
	reservationStarted := time.Now()
	conns, err := reserveConnections(ctx, db, Partitions)
	span.SetAttributes(
		attribute.Int("code_topic.requested_connections", Partitions),
		attribute.Int64("code_topic.connection_reservation_wait_ms", time.Since(reservationStarted).Milliseconds()),
	)
	if err != nil {
		span.SetAttributes(attribute.Bool("code_topic.connection_reservation_canceled", errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)))
		return nil, err
	}
	span.SetAttributes(attribute.Int("code_topic.reserved_connections", len(conns)))
	defer closeConnections(conns)
	probeStarted := time.Now()
	options := &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	exporter, err := conns[0].BeginTx(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("begin code topic snapshot: %w", err)
	}
	defer func() { _ = exporter.Rollback() }()
	var snapshot string
	if err := exporter.QueryRowContext(ctx, "SELECT pg_export_snapshot()").Scan(&snapshot); err != nil {
		return nil, fmt.Errorf("export code topic snapshot: %w", err)
	}
	for _, ch := range snapshot {
		if (ch < '0' || ch > '9') && (ch < 'A' || ch > 'F') && ch != '-' {
			return nil, fmt.Errorf("invalid exported snapshot identifier")
		}
	}
	if snapshot == "" {
		return nil, fmt.Errorf("empty exported snapshot identifier")
	}
	probes, err := RunPartitions(ctx, Partitions, func(workerCtx context.Context, index int) ([]ProbeRow, error) {
		group := req
		group.Terms = nil
		for termIndex := index; termIndex < len(req.Terms); termIndex += Partitions {
			group.Terms = append(group.Terms, req.Terms[termIndex])
		}
		if index == 0 {
			return readCodeTopicProbe(workerCtx, exporter, group, cap, filters, baseArgs)
		}
		tx, beginErr := conns[index].BeginTx(workerCtx, options)
		if beginErr != nil {
			return nil, fmt.Errorf("begin code topic worker %d: %w", index, beginErr)
		}
		defer func() { _ = tx.Rollback() }()
		// #nosec G201 -- snapshot is a validated server-generated identifier.
		if _, setErr := tx.ExecContext(workerCtx, "SET TRANSACTION SNAPSHOT '"+snapshot+"'"); setErr != nil {
			return nil, fmt.Errorf("import code topic snapshot worker %d: %w", index, setErr)
		}
		return readCodeTopicProbe(workerCtx, tx, group, cap, filters, baseArgs)
	})
	if err != nil {
		return nil, err
	}
	probeDone := time.Since(probeStarted)
	assemblyStarted := time.Now()
	candidates := make([]ProbeRow, 0)
	for _, group := range probes {
		candidates = append(candidates, group...)
	}
	payload, err := json.Marshal(candidates)
	if err != nil {
		return nil, fmt.Errorf("encode code topic probes: %w", err)
	}
	rows, err := exporter.QueryContext(ctx, AssemblySQL(cap), string(payload), req.Limit, req.Offset)
	if err != nil {
		return nil, fmt.Errorf("assemble code topic page: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result, capped, err := scan(rows)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.Int("code_topic.probe_rows", len(candidates)),
		attribute.Int("code_topic.probe_payload_bytes", len(payload)),
		attribute.Int64("code_topic.probe_duration_ms", probeDone.Milliseconds()),
		attribute.Int64("code_topic.assembly_duration_ms", time.Since(assemblyStarted).Milliseconds()),
		attribute.Bool("code_topic.pool_truncated", capped),
	)
	return result, nil
}
