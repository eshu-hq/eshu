package query

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (cr *ContentReader) investigateCodeTopic(
	ctx context.Context,
	req codeTopicInvestigationRequest,
) ([]codeTopicEvidenceRow, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
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
	filters := make([]string, 0, 2)
	args := make([]any, 0, 2)
	nextArg := 1
	if strings.TrimSpace(req.RepoID) != "" {
		filters = append(filters, fmt.Sprintf("repo_id = $%d", nextArg))
		args = append(args, strings.TrimSpace(req.RepoID))
		nextArg++
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
