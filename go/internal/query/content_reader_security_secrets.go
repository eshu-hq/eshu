// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	secretlines "github.com/eshu-hq/eshu/go/internal/storage/postgres/secret/lines"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const hardcodedSecretSQLPattern = `(password|passwd|pwd|api[_-]?key|apikey|token|secret|client[_-]?secret|private[_-]?key|authorization)[[:space:]]*[:=][[:space:]]*['"]?[A-Za-z0-9_./+=:@!#$%^-]{6,}|AKIA[0-9A-Z]{16}|sk_live_[A-Za-z0-9]{8,}|xox[baprs]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----`

// The compile-time half of the #6060 interface-export tripwire for the
// source-reporting form of the secrets investigation read; the plain form is
// asserted in content_reader.go.
var _ codequery.HardcodedSecretSourceInvestigator = (*ContentReader)(nil)

// InvestigateHardcodedSecrets reads classified hardcoded-secret findings
// (AWS keys, private-key blocks, Slack tokens, and password/token/secret
// literal assignments). It is the source-blind form of
// InvestigateHardcodedSecretsWithSource, kept for callers that only need rows.
func (cr *ContentReader) InvestigateHardcodedSecrets(
	ctx context.Context,
	req codequery.HardcodedSecretInvestigationRequest,
) ([]codequery.HardcodedSecretFindingRow, error) {
	rows, _, err := cr.InvestigateHardcodedSecretsWithSource(ctx, req)
	return rows, err
}

// InvestigateHardcodedSecretsWithSource reads classified hardcoded-secret
// findings and reports which storage path served them. While
// content_file_secret_lines is published ready (migration 131, #7125) the read
// is an ordered primary-key scan of that side table that stops at LIMIT. While
// it is not (a bootstrap-index bulk load skipped the write-time derivation and
// its finalizer has not completed) the read runs the legacy content_files scan,
// returns identical rows more slowly, and says so through the returned source,
// the read-source counter, and a span attribute. It never returns a partial
// side-table answer. The detection pattern (hardcodedSecretSQLPattern) and
// suppression rules (hardcodedSecretSQLSuppressionPredicate) stay the Go
// sources of truth that both paths and the migration are bound to. Suppressed
// rows are filtered unless req.IncludeSuppressed is set; Suppressions and
// Suppressed on each row are computed in Go from the row. It is the
// Postgres-backed fast path codequery.HardcodedSecretSourceInvestigator exposes
// to CodeHandler.hardcodedSecretRowsWithSource; without a satisfying store that
// caller returns errHardcodedSecretBackendUnavailable rather than a degraded
// scan.
func (cr *ContentReader) InvestigateHardcodedSecretsWithSource(
	ctx context.Context,
	req codequery.HardcodedSecretInvestigationRequest,
) ([]codequery.HardcodedSecretFindingRow, codequery.HardcodedSecretReadSource, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "investigate_hardcoded_secrets"),
		),
	)
	defer span.End()

	source := codequery.HardcodedSecretReadSideTable
	ready, err := secretlines.Ready(ctx, cr.db)
	if err != nil {
		span.RecordError(err)
		return nil, source, fmt.Errorf("investigate hardcoded secrets: %w", err)
	}
	var (
		query string
		args  []any
		table = "content_file_secret_lines"
	)
	if ready {
		query, args = hardcodedSecretInvestigationQuery(req)
	} else {
		source = codequery.HardcodedSecretReadLegacyScan
		table = "content_files"
		query, args = hardcodedSecretLegacyScanQuery(req)
	}
	span.SetAttributes(
		attribute.String("db.sql.table", table),
		attribute.String("eshu.hardcoded_secret.read_source", string(source)),
	)
	cr.recordHardcodedSecretRead(ctx, source)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, source, fmt.Errorf("investigate hardcoded secrets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := make([]codequery.HardcodedSecretFindingRow, 0)
	for rows.Next() {
		var row codequery.HardcodedSecretFindingRow
		if err := rows.Scan(
			&row.RepoID,
			&row.RelativePath,
			&row.Language,
			&row.LineNumber,
			&row.LineText,
			&row.FindingKind,
		); err != nil {
			span.RecordError(err)
			return nil, source, fmt.Errorf("scan hardcoded secret result: %w", err)
		}
		row.Confidence, row.Severity = hardcodedSecretRisk(row.FindingKind)
		row.Suppressions = hardcodedSecretSuppressions(row.RelativePath, row.LineText)
		row.Suppressed = len(row.Suppressions) > 0
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, source, err
	}
	span.SetAttributes(attribute.Int("db.rows.hardcoded_secret_findings", len(results)))
	return results, source, nil
}

// WithInstruments returns cr with operator metrics wired, so reads that report
// a signal (the hardcoded-secret read source counter) record it. Nil is
// tolerated and records nothing.
func (cr *ContentReader) WithInstruments(instruments *telemetry.Instruments) *ContentReader {
	cr.instruments = instruments
	return cr
}

// recordHardcodedSecretRead counts the read by source so an operator can alert
// on legacy_scan reads (a bulk load that never published the side table).
func (cr *ContentReader) recordHardcodedSecretRead(ctx context.Context, source codequery.HardcodedSecretReadSource) {
	if cr.instruments == nil {
		return
	}
	cr.instruments.HardcodedSecretReads.Add(ctx, 1, metric.WithAttributes(telemetry.AttrSource(string(source))))
}

// hardcodedSecretInvestigationQuery builds the side-table read. Go assembles
// the WHERE per branch, so no "$n OR" clause couples the branches in a generic
// plan (pgx statement caching can reach generic plans). The ORDER BY is the
// side table's primary-key order plus an inert finding_kind tiebreak (one CASE
// value per line), so any page equals the legacy scan's page for the same
// request. Repository scope and the grant filter apply before LIMIT/OFFSET
// (#5167 filter-before-limit); language is matched against the column that
// already stores content_files.language with NULL folded to an empty string.
func hardcodedSecretInvestigationQuery(req codequery.HardcodedSecretInvestigationRequest) (string, []any) {
	filters, args, nextArg := hardcodedSecretFilters(req)
	if len(req.FindingKinds) > 0 {
		filters = append(filters, fmt.Sprintf("s.finding_kind = ANY(string_to_array($%d, E'\\x1f'))", nextArg))
		args = append(args, strings.Join(req.FindingKinds, "\x1f"))
		nextArg++
	}
	if !req.IncludeSuppressed {
		filters = append(filters, "NOT s.suppressed")
	}

	var query strings.Builder
	query.WriteString("\n\t\tSELECT s.repo_id, s.relative_path, s.language, s.line_number, s.line_text, s.finding_kind\n")
	query.WriteString("\t\tFROM content_file_secret_lines s\n")
	query.WriteString("\t\tWHERE TRUE\n")
	for _, filter := range filters {
		query.WriteString("\t\t  AND " + filter + "\n")
	}
	fmt.Fprintf(&query, "\t\tORDER BY s.repo_id, s.relative_path, s.line_number, s.finding_kind\n\t\tLIMIT $%d OFFSET $%d\n\t", nextArg, nextArg+1)
	args = append(args, req.Limit, req.Offset)
	return query.String(), args
}

// hardcodedSecretFilters returns the repository (or grant) and language
// predicates of the side-table read, their arguments, and the next free
// argument index. A named repository wins over the grant list, so a caller who
// anchored the request never gains a wider ANY() scan.
func hardcodedSecretFilters(req codequery.HardcodedSecretInvestigationRequest) ([]string, []any, int) {
	filters := make([]string, 0, 4)
	args := make([]any, 0, 6)
	nextArg := 1
	if repoID := strings.TrimSpace(req.RepoID); repoID != "" {
		filters = append(filters, fmt.Sprintf("s.repo_id = $%d", nextArg))
		args = append(args, repoID)
		nextArg++
	} else if len(req.AllowedRepositoryIDs) > 0 {
		filters = append(filters, fmt.Sprintf("s.repo_id = ANY($%d)", nextArg))
		args = append(args, array.Of(req.AllowedRepositoryIDs))
		nextArg++
	}
	if language := strings.TrimSpace(req.Language); language != "" {
		filters = append(filters, fmt.Sprintf("s.language = $%d", nextArg))
		args = append(args, language)
		nextArg++
	}
	return filters, args, nextArg
}

// hardcodedSecretLegacyScanQuery builds the corpus-scan read that served the
// investigation before migration 131 (#7125) and now serves it while
// content_file_secret_lines is not published ready. It is the pre-change
// builder, unchanged: hardcoded_secret_legacy_golden_test.go keeps a frozen
// copy and a guard requires this query to equal it byte for byte for every
// request shape, and the live differential requires identical rows in identical
// order to the side-table read. It interpolates hardcodedSecretSQLPattern and
// hardcodedSecretSQLSuppressionPredicate, so it tracks the same Go sources of
// truth as the migration.
func hardcodedSecretLegacyScanQuery(
	req codequery.HardcodedSecretInvestigationRequest,
) (string, []any) {
	filters, args, nextArg := hardcodedSecretLegacyScanFilters(req)
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

func hardcodedSecretLegacyScanFilters(req codequery.HardcodedSecretInvestigationRequest) ([]string, []any, int) {
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

type hardcodedSecretSuppressionRule struct {
	reason        string
	pathFragments []string
	lineFragments []string
}

var hardcodedSecretSuppressionRules = []hardcodedSecretSuppressionRule{
	{
		reason:        "test_or_fixture_path",
		pathFragments: []string{"_test.", "/testdata/", "/fixtures/", "/examples/"},
	},
	{
		reason:        "placeholder_literal",
		lineFragments: []string{"example", "dummy", "placeholder", "changeme"},
	},
}

func hardcodedSecretSuppressions(relativePath, lineText string) []string {
	path := strings.ToLower(relativePath)
	line := strings.ToLower(lineText)
	suppressions := make([]string, 0, len(hardcodedSecretSuppressionRules))
	for _, rule := range hardcodedSecretSuppressionRules {
		if containsAnyFragment(path, rule.pathFragments) || containsAnyFragment(line, rule.lineFragments) {
			suppressions = append(suppressions, rule.reason)
		}
	}
	return suppressions
}

func hardcodedSecretSQLSuppressionPredicate() string {
	clauses := make([]string, 0, 8)
	for _, rule := range hardcodedSecretSuppressionRules {
		for _, fragment := range rule.pathFragments {
			clauses = append(clauses, hardcodedSecretSQLContains("f.relative_path", fragment))
		}
		for _, fragment := range rule.lineFragments {
			clauses = append(clauses, hardcodedSecretSQLContains("lines.line_text", fragment))
		}
	}
	if len(clauses) == 0 {
		return "false"
	}
	return strings.Join(clauses, " OR ")
}

func hardcodedSecretSQLContains(column, fragment string) string {
	return fmt.Sprintf("strpos(lower(%s), '%s') > 0", column, strings.ReplaceAll(fragment, "'", "''"))
}

func containsAnyFragment(value string, fragments []string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
