// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// InvestigateCodeTopic scores entities and files in content_entities and
// content_files against req.Terms (name/source-cache substring match for
// entities, path/content substring match for files), ranked by distinct
// term hits and scoped by req.RepoID or, for a corpus-wide search, by
// req.AllowedRepositoryIDs. It is the batched fast path
// codeTopicContentInvestigator exposes to CodeHandler.codeTopicRows and
// changeSurfaceTopicRows; the fallback those callers take without a
// satisfying store returns an error rather than a slower equivalent result.
func (cr *ContentReader) InvestigateCodeTopic(
	ctx context.Context,
	req codeTopicInvestigationRequest,
) ([]codeTopicEvidenceRow, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "investigate_code_topic"),
			attribute.String("db.sql.table", "content_entities,content_files"),
		),
	)
	defer span.End()

	filters, args, nextArg := codeTopicFilters(req)
	where := ""
	if len(filters) > 0 {
		where = "WHERE " + strings.Join(filters, " AND ")
	}
	// #nosec G201 -- interpolates integer arg indices and `where` which contains only $N placeholder clauses from codeTopicFilters; no user data concatenated into SQL
	query := fmt.Sprintf(`
		WITH terms AS (
		  SELECT unnest(string_to_array($%d, E'\x1f')) AS term
		),
		entity_matches AS (
		  SELECT
		    'entity' AS source_kind,
		    e.repo_id,
		    e.relative_path,
		    e.entity_id,
		    e.entity_name,
		    e.entity_type,
		    coalesce(e.language, '') AS language,
		    e.start_line,
		    e.end_line,
		    string_agg(DISTINCT terms.term, E'\x1f' ORDER BY terms.term) AS matched_terms,
		    count(DISTINCT terms.term)::int AS score
		  FROM content_entities e
		  JOIN terms ON e.entity_name ILIKE '%%' || terms.term || '%%'
		    OR e.source_cache ILIKE '%%' || terms.term || '%%'
		  %s
		  GROUP BY e.repo_id, e.relative_path, e.entity_id, e.entity_name, e.entity_type,
		           e.language, e.start_line, e.end_line
		),
		file_matches AS (
		  SELECT
		    'file' AS source_kind,
		    f.repo_id,
		    f.relative_path,
		    '' AS entity_id,
		    '' AS entity_name,
		    '' AS entity_type,
		    coalesce(f.language, '') AS language,
		    1 AS start_line,
		    least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line,
		    string_agg(DISTINCT terms.term, E'\x1f' ORDER BY terms.term) AS matched_terms,
		    count(DISTINCT terms.term)::int AS score
		  FROM content_files f
		  JOIN terms ON f.relative_path ILIKE '%%' || terms.term || '%%'
		    OR f.content ILIKE '%%' || terms.term || '%%'
		  %s
		  GROUP BY f.repo_id, f.relative_path, f.language, f.line_count
		)
		SELECT source_kind, repo_id, relative_path, entity_id, entity_name,
		       entity_type, language, start_line, end_line, matched_terms, score
		FROM (
		  SELECT * FROM entity_matches
		  UNION ALL
		  SELECT * FROM file_matches
		) matches
		ORDER BY score DESC, repo_id, relative_path, entity_name, source_kind
		LIMIT $%d OFFSET $%d
	`, nextArg, where, where, nextArg+1, nextArg+2)
	args = append(args, strings.Join(req.Terms, "\x1f"), req.Limit, req.Offset)

	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		err = contentSubstringIndexReadError(err)
		span.RecordError(err)
		return nil, fmt.Errorf("investigate code topic: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []codeTopicEvidenceRow
	for rows.Next() {
		var row codeTopicEvidenceRow
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
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan code topic result: %w", err)
		}
		row.MatchedTerms = splitCodeTopicTerms(matchedTerms)
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

func codeTopicFilters(req codeTopicInvestigationRequest) ([]string, []any, int) {
	filters := make([]string, 0, 3)
	args := make([]any, 0, 3)
	nextArg := 1
	if strings.TrimSpace(req.RepoID) != "" {
		filters = append(filters, fmt.Sprintf("repo_id = $%d", nextArg))
		args = append(args, strings.TrimSpace(req.RepoID))
		nextArg++
	} else {
		filters = append(filters, "eshu_require_content_substring_indexes_ready()")
		// #5167 W3 P1: bind a corpus-wide search to the caller's grant at the SQL
		// WHERE so the LIMIT/OFFSET page is taken from the granted set, not a
		// cross-tenant-polluted page that could push authorized rows past the
		// limit. Only set for a scoped caller (populated by the change-surface
		// caller); a nil/empty list leaves the search unrestricted.
		filters, args, nextArg = appendRepositoryGrantFilter(filters, args, nextArg, req.AllowedRepositoryIDs)
	}
	if strings.TrimSpace(req.Language) != "" {
		filters = append(filters, fmt.Sprintf("coalesce(language, '') = $%d", nextArg))
		args = append(args, strings.TrimSpace(req.Language))
		nextArg++
	}
	return filters, args, nextArg
}

func splitCodeTopicTerms(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, "\x1f")
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			terms = append(terms, part)
		}
	}
	return terms
}

// appendRepositoryGrantFilter binds a corpus-wide content read to the caller's
// granted repository ids at the SQL WHERE, so the statement's own LIMIT/OFFSET
// page is taken from the granted set rather than from a cross-tenant-polluted
// one (#5167 W3 P1 filter-before-limit). It is the single grant predicate
// shared by codeTopicFilters, symbolSearchFilters, hardcodedSecretFilters and
// structuralInventoryWhere -- four builders that had drifted into the same
// `if repoID != "" { ... }`-with-no-else shape.
//
// An empty list is a no-op, which is correct for the unscoped shared, admin,
// and local callers that pass one. A grantless SCOPED caller must never reach
// here: an empty list leaves the scan unrestricted, so codeContentGrantScope
// (code_repository_selector.go) fails that caller closed before the read.
//
// nextArg is the next free $N placeholder index and must equal len(args)+1.
// The returned index is the next free one after the predicate is appended.
func appendRepositoryGrantFilter(
	filters []string,
	args []any,
	nextArg int,
	allowedRepositoryIDs []string,
) ([]string, []any, int) {
	if len(allowedRepositoryIDs) == 0 {
		return filters, args, nextArg
	}
	filters = append(filters, fmt.Sprintf("repo_id = ANY($%d)", nextArg))
	args = append(args, pgarray.Array(allowedRepositoryIDs))
	return filters, args, nextArg + 1
}
