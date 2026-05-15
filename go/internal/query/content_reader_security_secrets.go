package query

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const hardcodedSecretSQLPattern = `(password|passwd|pwd|api[_-]?key|apikey|token|secret|client[_-]?secret|private[_-]?key|authorization)[[:space:]]*[:=][[:space:]]*['"]?[A-Za-z0-9_./+=:@!#$%^-]{6,}|AKIA[0-9A-Z]{16}|sk_live_[A-Za-z0-9]{8,}|xox[baprs]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----`

func (cr *ContentReader) investigateHardcodedSecrets(
	ctx context.Context,
	req hardcodedSecretInvestigationRequest,
) ([]hardcodedSecretFindingRow, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "investigate_hardcoded_secrets"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	filters, args, nextArg := hardcodedSecretFilters(req)
	where := ""
	if len(filters) > 0 {
		where = "AND " + strings.Join(filters, " AND ")
	}
	kindFilterArg := nextArg + 1
	limitArg := nextArg + 2
	offsetArg := nextArg + 3
	query := fmt.Sprintf(`
		WITH candidate_files AS (
		  SELECT repo_id, relative_path, coalesce(language, '') AS language, content
		  FROM content_files
		  WHERE content ~* $%d
		  %s
		),
		candidate_lines AS (
		  SELECT
		    f.repo_id,
		    f.relative_path,
		    f.language,
		    lines.line_number::int AS line_number,
		    lines.line_text,
		    CASE
		      WHEN lines.line_text ~* 'AKIA[0-9A-Z]{16}' THEN 'aws_access_key'
		      WHEN lines.line_text ~* '-----BEGIN [A-Z ]*PRIVATE KEY-----' THEN 'private_key'
		      WHEN lines.line_text ~* 'xox[baprs]-[A-Za-z0-9-]{10,}' THEN 'slack_token'
		      WHEN lines.line_text ~* '(api[_-]?key|apikey|token)[[:space:]]*[:=]' THEN 'api_token'
		      WHEN lines.line_text ~* '(password|passwd|pwd)[[:space:]]*[:=]' THEN 'password_literal'
		      WHEN lines.line_text ~* '(secret|client[_-]?secret|private[_-]?key|authorization)[[:space:]]*[:=]' THEN 'secret_literal'
		      ELSE ''
		    END AS finding_kind
		  FROM candidate_files f
		  CROSS JOIN LATERAL regexp_split_to_table(f.content, E'\n')
		    WITH ORDINALITY AS lines(line_text, line_number)
		  WHERE lines.line_text ~* $%d
		)
		SELECT repo_id, relative_path, language, line_number, line_text, finding_kind
		FROM candidate_lines
		WHERE finding_kind <> ''
		  AND ($%d = '' OR finding_kind = ANY(string_to_array($%d, E'\x1f')))
		ORDER BY repo_id, relative_path, line_number, finding_kind
		LIMIT $%d OFFSET $%d
	`, nextArg, where, nextArg, kindFilterArg, kindFilterArg, limitArg, offsetArg)
	args = append(args, hardcodedSecretSQLPattern, strings.Join(req.FindingKinds, "\x1f"), req.Limit, req.Offset)

	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("investigate hardcoded secrets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := make([]hardcodedSecretFindingRow, 0)
	for rows.Next() {
		var row hardcodedSecretFindingRow
		if err := rows.Scan(
			&row.RepoID,
			&row.RelativePath,
			&row.Language,
			&row.LineNumber,
			&row.LineText,
			&row.FindingKind,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan hardcoded secret result: %w", err)
		}
		row.Confidence, row.Severity = hardcodedSecretRisk(row.FindingKind)
		row.Suppressions = hardcodedSecretSuppressions(row.RelativePath, row.LineText)
		row.Suppressed = len(row.Suppressions) > 0
		if row.Suppressed && !req.IncludeSuppressed {
			continue
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

func hardcodedSecretFilters(req hardcodedSecretInvestigationRequest) ([]string, []any, int) {
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

func hardcodedSecretRisk(kind string) (string, string) {
	switch kind {
	case "aws_access_key", "private_key", "slack_token":
		return "high", "critical"
	case "api_token", "password_literal", "secret_literal":
		return "medium", "high"
	default:
		return "low", "medium"
	}
}

func hardcodedSecretSuppressions(relativePath, lineText string) []string {
	path := strings.ToLower(relativePath)
	line := strings.ToLower(lineText)
	suppressions := make([]string, 0, 2)
	if strings.Contains(path, "_test.") || strings.Contains(path, "/testdata/") ||
		strings.Contains(path, "/fixtures/") || strings.Contains(path, "/examples/") {
		suppressions = append(suppressions, "test_or_fixture_path")
	}
	if strings.Contains(line, "example") || strings.Contains(line, "dummy") ||
		strings.Contains(line, "placeholder") || strings.Contains(line, "changeme") {
		suppressions = append(suppressions, "placeholder_literal")
	}
	return suppressions
}
