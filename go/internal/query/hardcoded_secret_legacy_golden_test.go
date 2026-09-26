// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
)

// legacyHardcodedSecretInvestigationQuery is the pre-#7125 corpus-scan query
// builder, kept as a test-only golden. It was lifted mechanically from
// InvestigateHardcodedSecrets at b7a0d0e367 (git show of
// content_reader_security_secrets.go) with only the function shell and the
// filter helper renamed; it still interpolates the production
// hardcodedSecretSQLPattern and hardcodedSecretSQLSuppressionPredicate, so it
// tracks the Go sources of truth instead of a hand-copied frozen pattern.
// The differential proof in content_reader_security_secrets_live_test.go runs
// this query and the side-table reader over the same content_files data and
// requires identical rows in identical order.
func legacyHardcodedSecretInvestigationQuery(
	req codequery.HardcodedSecretInvestigationRequest,
) (string, []any) {
	filters, args, nextArg := legacyHardcodedSecretFilters(req)
	where := ""
	if len(filters) > 0 {
		where = "AND " + strings.Join(filters, " AND ")
	}
	kindFilterArg := nextArg + 1
	includeSuppressedArg := nextArg + 2
	limitArg := nextArg + 3
	offsetArg := nextArg + 4
	// #nosec G201 -- interpolates integer arg indices and hardcodedSecretSQLSuppressionPredicate (built from compile-time suppression rules with SQL-escaped literals); no user data concatenated into SQL
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
		    END AS finding_kind,
		    (%s) AS suppressed
		  FROM candidate_files f
		  CROSS JOIN LATERAL regexp_split_to_table(f.content, E'\n')
		    WITH ORDINALITY AS lines(line_text, line_number)
		  WHERE lines.line_text ~* $%d
		)
		SELECT repo_id, relative_path, language, line_number, line_text, finding_kind
		FROM candidate_lines
		WHERE finding_kind <> ''
		  AND ($%d = '' OR finding_kind = ANY(string_to_array($%d, E'\x1f')))
		  AND ($%d OR NOT suppressed)
		ORDER BY repo_id, relative_path, line_number, finding_kind
		LIMIT $%d OFFSET $%d
	`, nextArg, where, hardcodedSecretSQLSuppressionPredicate(), nextArg, kindFilterArg, kindFilterArg, includeSuppressedArg, limitArg, offsetArg)
	args = append(args, hardcodedSecretSQLPattern, strings.Join(req.FindingKinds, "\x1f"), req.IncludeSuppressed, req.Limit, req.Offset)
	return query, args
}

func legacyHardcodedSecretFilters(req codequery.HardcodedSecretInvestigationRequest) ([]string, []any, int) {
	filters := make([]string, 0, 2)
	args := make([]any, 0, 2)
	nextArg := 1
	if strings.TrimSpace(req.RepoID) != "" {
		filters = append(filters, fmt.Sprintf("repo_id = $%d", nextArg))
		args = append(args, strings.TrimSpace(req.RepoID))
		nextArg++
	} else {
		filters, args, nextArg = appendRepositoryGrantFilter(filters, args, nextArg, req.AllowedRepositoryIDs)
	}
	if strings.TrimSpace(req.Language) != "" {
		filters = append(filters, fmt.Sprintf("coalesce(language, '') = $%d", nextArg))
		args = append(args, strings.TrimSpace(req.Language))
		nextArg++
	}
	return filters, args, nextArg
}
