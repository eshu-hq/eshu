// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// codeTopicCandidatePoolBudget/Floor bound per-term scans (#7008).
// candidateCap divides the row budget across terms with a floor for short searches.
const (
	codeTopicCandidatePoolBudget = 4000
	codeTopicCandidatePoolFloor  = 150
)

// codeTopicCandidateCap returns the per-term LATERAL LIMIT InvestigateCodeTopic
// applies to each of content_entities and content_files.
func codeTopicCandidateCap(termCount int) int {
	if termCount <= 0 {
		termCount = 1
	}
	candidateCap := codeTopicCandidatePoolBudget / termCount
	if candidateCap < codeTopicCandidatePoolFloor {
		candidateCap = codeTopicCandidatePoolFloor
	}
	return candidateCap
}

// InvestigateCodeTopic ranks term matches in content_entities (name/source)
// and content_files (path/content), scoped by repo or caller grant. It serves
// CodeHandler and change-surface fast paths; a missing store fails closed.
//
// #7008: entity_probe caps matches per term with LATERAL.
// #7033: the lateral probes narrow entity_name first and reads TOASTed
// source_cache only for the remaining per-term budget, excluding name hits.
// A runtime LIKE pattern defeats pg_trgm extraction, so the old name-OR-source
// scan detoasted source_cache per examined row; the split keeps the exact OR
// multiset when uncapped and selects name-priority when capped.
// file_probe uses independent UNION branches: LATERAL serialized content scans
// (>35s for nine terms), while top-level branches measured ~16-23s.
// A materialized path pool sets remaining content capacity; the content branch
// excludes path hits and preserves the uncapped match set. Pool-capped flags
// derive from probe rows and explicitly report candidate truncation.
func (cr *ContentReader) InvestigateCodeTopic(ctx context.Context, req codequery.CodeTopicInvestigationRequest) ([]codequery.CodeTopicEvidenceRow, error) {
	if len(req.Terms) == 0 {
		return nil, nil
	}
	candidateCap := codeTopicCandidateCap(len(req.Terms))
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "investigate_code_topic"),
			attribute.String("db.sql.table", "content_entities,content_files"),
			attribute.Int("code_topic.term_count", len(req.Terms)),
			attribute.Int("code_topic.candidate_cap_per_term", candidateCap),
		),
	)
	defer span.End()
	parallelStore, supportsSnapshotSet := cr.db.(db.ReadSnapshotSetBeginner)
	maxOpenConns := 0
	if supportsSnapshotSet {
		maxOpenConns = parallelStore.MaxReadConnections()
	}
	if supportsSnapshotSet && codetopicparallel.Eligible(len(req.Terms), maxOpenConns) {
		filters, args, _ := codeTopicFilters(req)
		var results []codequery.CodeTopicEvidenceRow
		var err error
		for attempt := 0; attempt < 2; attempt++ {
			results, err = codetopicparallel.Investigate(ctx, parallelStore, span, attempt, req, candidateCap, filters, args, scanCodeTopicEvidenceRows)
			if err == nil || ctx.Err() != nil || attempt > 0 || !codetopicparallel.IsReaderMemberLoss(err) {
				break
			}
			span.SetAttributes(attribute.Int("code_topic.reader_member_retries", 1))
		}
		if err == nil {
			span.SetAttributes(attribute.String("code_topic.execution_mode", "parallel_shared_snapshot"))
			return results, nil
		}
		if errors.Is(err, db.ErrSnapshotExportUnqualified) {
			return nil, codetopicparallel.RecordSnapshotExportDenial(span, err)
		}
		if ctx.Err() != nil || !errors.Is(err, db.ErrSnapshotReservationCapacity) {
			// #7008: the parallel path stays error-only on deadline (no
			// bounded partial). Its probe rows are unscored and unordered
			// until the assembly statement scores and pages them, so a
			// mid-probe deadline has no valid score-ordered prefix to
			// return. Only the single-statement path below streams
			// score-ordered rows and can answer its gathered prefix.
			span.SetAttributes(attribute.String("code_topic.execution_mode", "parallel_shared_snapshot"))
			span.RecordError(err)
			return nil, contentSubstringIndexReadError(err)
		}
		span.SetAttributes(attribute.String("code_topic.parallel_fallback_reason", "reservation_acquire_timeout"))
	}
	span.SetAttributes(attribute.String("code_topic.execution_mode", "single_statement"))
	recordCodeTopicFallbackReason(span, len(req.Terms), supportsSnapshotSet, maxOpenConns)
	filters, args, nextArg := codeTopicFilters(req)
	where := ""
	if len(filters) > 0 {
		where = "AND " + strings.Join(filters, " AND ")
	}
	termValues := make([]string, len(req.Terms))
	fileBranches := make([]string, len(req.Terms))
	measuredScopedOneTerm := strings.TrimSpace(req.RepoID) != "" && len(req.Terms) == 1 && strings.TrimSpace(req.Language) == ""
	span.SetAttributes(attribute.Bool("code_topic.scoped_one_term", measuredScopedOneTerm))
	hideTerm := measuredScopedOneTerm && codequery.CodeTopicTermHasTrigram(req.Terms[0])
	span.SetAttributes(attribute.Bool("code_topic.terms_materialized", hideTerm))
	scopedRepo := codequery.CodeTopicIsRepoScoped(req)
	for i, term := range req.Terms {
		termValues[i] = fmt.Sprintf("($%d)", nextArg)
		fileBranches[i] = codeTopicFileBranch(measuredScopedOneTerm, scopedRepo, term, nextArg, where, candidateCap)
		args = append(args, term)
		nextArg++
	}
	limitArg, offsetArg := nextArg, nextArg+1
	args = append(args, req.Limit, req.Offset)
	// #nosec G201 -- interpolates integer arg indices and the generated
	// per-term UNION branches above, which contain only $N placeholders
	// and static SQL; no user data concatenated
	query := fmt.Sprintf(`
		WITH terms(term) AS %[7]s(
		  VALUES %[1]s
		),
		entity_probe AS (
		  SELECT terms.term AS matched_term, m.repo_id, m.relative_path, m.entity_id,
		         m.entity_name, m.entity_type, m.language, m.start_line, m.end_line
		  FROM terms
		  CROSS JOIN LATERAL (
		    WITH name_pool AS MATERIALIZED (
		      SELECT e.repo_id, e.relative_path, e.entity_id, e.entity_name, e.entity_type,
		             coalesce(e.language, '') AS language, e.start_line, e.end_line
		      FROM content_entities e
		      WHERE e.entity_name ILIKE '%%' || terms.term || '%%'
		      %[2]s
		      LIMIT %[3]d
		    ),
		    source_pool AS (
		      SELECT e.repo_id, e.relative_path, e.entity_id, e.entity_name, e.entity_type,
		             coalesce(e.language, '') AS language, e.start_line, e.end_line
		      FROM content_entities e
		      WHERE e.source_cache ILIKE '%%' || terms.term || '%%'
		        AND e.entity_name NOT ILIKE '%%' || terms.term || '%%'
		      %[2]s
		      LIMIT (SELECT %[3]d - count(*) FROM name_pool)
		    )
		    SELECT * FROM name_pool
		    UNION ALL
		    SELECT * FROM source_pool
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
		  SELECT coalesce(bool_or(term_count >= %[3]d), false) AS capped
		  FROM (SELECT matched_term, count(*) AS term_count FROM entity_probe GROUP BY matched_term) t
		),
		file_probe AS (
		  %[4]s
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
		  SELECT coalesce(bool_or(term_count >= %[3]d), false) AS capped
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
		LIMIT $%[5]d OFFSET $%[6]d
	`, strings.Join(termValues, ", "), where, candidateCap, strings.Join(fileBranches, "\n\t\t  UNION ALL\n"), limitArg, offsetArg, codeTopicTermsMaterialization(hideTerm))

	// CacheDescribe keeps the measured one-term repo query off a generic
	// named plan while retaining parameter types and SQL placeholders.
	if measuredScopedOneTerm {
		args = append([]any{pgx.QueryExecModeCacheDescribe}, args...)
	}
	return cr.runCodeTopicSingleStatement(ctx, span, query, args)
}

// runCodeTopicSingleStatement executes the bounded serial topic statement and
// scans its score-ordered rows. #7008: the statement streams score-ordered
// rows, so a deadline surfaces the gathered prefix as a bounded partial
// instead of failing the call. A cancellation or backend fault stays
// fail-closed: callers must never read a truncated result as complete.
func (cr *ContentReader) runCodeTopicSingleStatement(ctx context.Context, span trace.Span, query string, args []any) ([]codequery.CodeTopicEvidenceRow, error) {
	probeStarted := time.Now()
	rows, err := cr.db.QueryContext(ctx, query, args...)
	probeDuration := time.Since(probeStarted)
	if err != nil {
		err = contentSubstringIndexReadError(err)
		if errors.Is(err, context.DeadlineExceeded) {
			recordCodeTopicProbeStage(span, probeDuration.Milliseconds(), 0, 0, 0, false, true)
			return nil, fmt.Errorf("%w before any scored row: %w", codequery.ErrCodeTopicDeadlineTruncated, err)
		}
		span.RecordError(err)
		return nil, fmt.Errorf("investigate code topic: %w", err)
	}
	defer func() { _ = rows.Close() }()

	assemblyStarted := time.Now()
	results, poolTruncated, err := scanCodeTopicEvidenceRows(rows)
	assemblyDuration := time.Since(assemblyStarted)
	entityRows, fileRows := countCodeTopicProbeKinds(results)
	if err != nil {
		if errors.Is(err, codequery.ErrCodeTopicDeadlineTruncated) {
			recordCodeTopicProbeStage(span, probeDuration.Milliseconds(), assemblyDuration.Milliseconds(), entityRows, fileRows, poolTruncated, true)
			return results, err
		}
		span.RecordError(err)
		return results, err
	}
	recordCodeTopicProbeStage(span, probeDuration.Milliseconds(), assemblyDuration.Milliseconds(), entityRows, fileRows, poolTruncated, false)
	return results, nil
}

// recordCodeTopicProbeStage records the single-statement topic read's
// per-stage breakdown (#7008): the probe statement round trip, the Go-side
// row assembly, and the entity/file row split. The entity and file probes are
// CTEs of one statement, so they share one round trip: splitting their
// server-side time would need EXPLAIN ANALYZE, which plans and runs the read
// differently, so the probe duration stays whole and the split is by rows
// gathered. Names mirror the parallel path's probe/assembly attributes.
func recordCodeTopicProbeStage(span trace.Span, probeMs, assemblyMs int64, entityRows, fileRows int, poolTruncated, deadlineTruncated bool) {
	span.SetAttributes(
		attribute.Int64("code_topic.probe_duration_ms", probeMs),
		attribute.Int64("code_topic.assembly_duration_ms", assemblyMs),
		attribute.Int("code_topic.entity_probe_rows", entityRows),
		attribute.Int("code_topic.file_probe_rows", fileRows),
		attribute.Bool("code_topic.pool_truncated", poolTruncated),
		attribute.Bool("code_topic.deadline_truncated", deadlineTruncated),
	)
}

// countCodeTopicProbeKinds splits gathered topic rows into the entity-probe
// and file-probe kinds for the per-stage span breakdown.
func countCodeTopicProbeKinds(rows []codequery.CodeTopicEvidenceRow) (entityRows, fileRows int) {
	for _, row := range rows {
		if row.SourceKind == "file" {
			fileRows++
			continue
		}
		entityRows++
	}
	return entityRows, fileRows
}

// codeTopicFileBranch selects the per-term file probe for the single-statement
// topic read. A repo-scoped search hides each trigram-bearing term's content
// bind behind a MATERIALIZED term_param (#7246 one-term, #7098 multi-term: a
// visible bind lets the planner heap-filter corpus-wide trigram matches before
// the repository predicate (measured 10.1 s against 1.3 s scoped to the
// largest repository on a full-corpus read replica). A term the GIN indexes
// cannot filter keeps the
// visible bind, as do corpus-wide, grant-list, and language-filtered shapes.
func codeTopicFileBranch(measuredScopedOneTerm, scopedRepo bool, term string, termArg int, where string, candidateCap int) string {
	if measuredScopedOneTerm || (scopedRepo && codequery.CodeTopicTermHasTrigram(term)) {
		return scopedCodeTopicFileBranch(termArg, where, candidateCap)
	}
	return codetopicparallel.FileBranch(termArg, where, candidateCap)
}

func scanCodeTopicEvidenceRows(rows db.Rows) ([]codequery.CodeTopicEvidenceRow, bool, error) {
	var results []codequery.CodeTopicEvidenceRow
	poolTruncated := false
	for rows.Next() {
		var row codequery.CodeTopicEvidenceRow
		var matchedTerms string
		if err := rows.Scan(
			&row.SourceKind,
			&row.RepoID,
			&row.RelativePath,
			&row.EntityID,
			&row.EntityName,
			&row.EntityType,
			&row.Language,
			&row.StartLine,
			&row.EndLine,
			&matchedTerms,
			&row.Score,
			&row.PoolTruncated,
		); err != nil {
			return nil, false, fmt.Errorf("scan code topic result: %w", err)
		}
		row.MatchedTerms = splitCodeTopicTerms(matchedTerms)
		if row.PoolTruncated {
			poolTruncated = true
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		// #7008: the statement streams score-ordered rows, so rows gathered
		// before the deadline are a valid page prefix: mark them as a
		// bounded partial instead of failing the call. Cancellations and
		// backend faults keep the plain error so callers stay fail-closed.
		if errors.Is(err, context.DeadlineExceeded) {
			return results, poolTruncated, fmt.Errorf("%w after %d scored rows: %w", codequery.ErrCodeTopicDeadlineTruncated, len(results), err)
		}
		return results, poolTruncated, err
	}
	return results, poolTruncated, nil
}
