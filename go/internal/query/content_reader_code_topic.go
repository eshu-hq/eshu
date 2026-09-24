// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// InvestigateCodeTopic scores entities and files in content_entities and
// content_files against req.Terms (name/source-cache substring match for
// entities, path/content substring match for files), ranked by distinct
// term hits and scoped by req.RepoID or, for a corpus-wide search, by
// req.AllowedRepositoryIDs. It is the batched fast path
// codequery.CodeTopicContentInvestigator exposes to CodeHandler.codeTopicRows and
// changeSurfaceTopicRows; the fallback those callers take without a
// satisfying store returns an error rather than a slower equivalent result.
func (cr *ContentReader) InvestigateCodeTopic(
	ctx context.Context,
	req codequery.CodeTopicInvestigationRequest,
) ([]codequery.CodeTopicEvidenceRow, error) {
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

	var results []codequery.CodeTopicEvidenceRow
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

func codeTopicFilters(req codequery.CodeTopicInvestigationRequest) ([]string, []any, int) {
	filters := make([]string, 0, 3)
	args := make([]any, 0, 3)
	nextArg := 1
	if strings.TrimSpace(req.RepoID) != "" {
		filters = append(filters, fmt.Sprintf("repo_id = $%d", nextArg))
		args = append(args, strings.TrimSpace(req.RepoID))
		nextArg++
	} else {
		filters = append(filters, "eshu_require_content_substring_indexes_ready()")
		// #5167 W3 P1: bind a corpus-wide search to the caller's grant so the
		// LIMIT/OFFSET page is taken from the granted set, not cross-tenant.
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
// granted repository ids at the SQL WHERE (#5167 W3 P1 filter-before-limit).
// Shared by codeTopicFilters, symbolSearchFilters, hardcodedSecretFilters and
// structuralInventoryWhere. Empty is a no-op (unscoped shared/admin callers);
// a grantless SCOPED caller never reaches here (codeContentGrantScope closes
// it first). nextArg must equal len(args)+1; returns the next free index.
func appendRepositoryGrantFilter(filters []string, args []any, nextArg int, allowedRepositoryIDs []string) ([]string, []any, int) {
	if len(allowedRepositoryIDs) == 0 {
		return filters, args, nextArg
	}
	filters = append(filters, fmt.Sprintf("repo_id = ANY($%d)", nextArg))
	args = append(args, array.Of(allowedRepositoryIDs))
	return filters, args, nextArg + 1
}

// ---- Code divergence reads (epic #6833, child #6836) ----
// Fingerprint-equality grouping over the #6835 side tables: same Postgres
// content store and required-repo grant discipline as the topic reads above.

// divergenceGroupStatsQuery is the phase-one grouping statement for one
// fingerprint column (fp_exact or fp_renamed): every multi-member group in
// the repo, as narrow rows the handler sorts/windows before any member
// fetch. Never selects source_cache (#6835 contract gate). $1 repo_id,
// $2 token floor.
func divergenceGroupStatsQuery(fingerprintColumn string) string {
	if fingerprintColumn != "fp_exact" && fingerprintColumn != "fp_renamed" {
		return ""
	}
	nullFilter := ""
	if fingerprintColumn == "fp_renamed" {
		nullFilter = "AND f.fp_renamed IS NOT NULL "
	}
	// #nosec G201 -- fingerprintColumn is one of two literals validated above; the rest is static SQL with $N args
	return fmt.Sprintf(`
		SELECT f.%[1]s AS fp, count(*) AS members, max(f.token_count) AS tokens
		FROM code_function_fingerprint f
		WHERE f.repo_id = $1 AND f.token_count >= $2 %[2]s
		GROUP BY f.%[1]s HAVING count(*) > 1
	`, fingerprintColumn, nullFilter)
}

// divergenceMembersQuery is the phase-two member lookup: one row per member
// of the page's fingerprints, ordered for contiguous grouping in Go. %s is
// the qualified fingerprint column (one of two literals, never caller text).
const divergenceMembersQuery = `
	SELECT %s AS fp, e.entity_id, e.entity_name, e.entity_type,
	       e.relative_path, coalesce(e.language, ''), f.token_count,
	       e.start_line, e.end_line
	FROM code_function_fingerprint f
	JOIN content_entities e ON e.entity_id = f.entity_id AND e.repo_id = f.repo_id
	WHERE f.repo_id = $1 AND %s = ANY($2)
	ORDER BY %s, e.entity_id
`

func divergenceColumn(kind codedivergence.Kind) string {
	if kind == codedivergence.KindRenamed {
		return "fp_renamed"
	}
	return "fp_exact"
}

// DivergenceGroupStats returns every multi-member fingerprint group for one
// repository and kind. repoID must already be grant-resolved by the caller
// (required-repo pattern, like call-graph metrics).
func (cr *ContentReader) DivergenceGroupStats(ctx context.Context, repoID string, kind codedivergence.Kind, floor int) ([]codedivergence.GroupStat, error) {
	if cr == nil || cr.db == nil {
		return nil, nil
	}
	statsQuery := divergenceGroupStatsQuery(divergenceColumn(kind))
	if statsQuery == "" {
		return nil, fmt.Errorf("divergence stats: unknown kind %q", kind)
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "divergence_group_stats"),
			attribute.String("db.sql.table", "code_function_fingerprint"),
		),
	)
	defer span.End()

	rows, err := cr.db.QueryContext(ctx, statsQuery, repoID, floor)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("divergence group stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	stats := make([]codedivergence.GroupStat, 0, 64)
	for rows.Next() {
		var stat codedivergence.GroupStat
		if err := rows.Scan(&stat.Fingerprint, &stat.Members, &stat.Tokens); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan divergence group stat: %w", err)
		}
		stat.Kind = kind
		stats = append(stats, stat)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return stats, nil
}

// driftedFindingStore adapts the drifted findings store over this reader's
// handle: the query package owns the read contract, postgres owns the SQL.
func (cr *ContentReader) driftedFindingStore() postgres.PostgresCodeDriftedFindingStore {
	storeDB := &postgres.InstrumentedDB{
		Inner:     postgres.SQLDB{DB: cr.db},
		Tracer:    cr.tracer,
		StoreName: "code_drifted_findings",
	}
	return postgres.PostgresCodeDriftedFindingStore{DB: storeDB}
}

// DivergenceMembersByEntityID resolves wrapper-bypass finding members from
// the fingerprint table by entity id (graph-discovered caller entities, not
// a fingerprint group). Ids missing here simply resolve to nothing.
// Repo-scoped like every other divergence read.
func (cr *ContentReader) DivergenceMembersByEntityID(ctx context.Context, repoID string, entityIDs []string) (map[string]codedivergence.Member, error) {
	if cr == nil || cr.db == nil {
		return map[string]codedivergence.Member{}, nil
	}
	if len(entityIDs) == 0 {
		return map[string]codedivergence.Member{}, nil
	}
	const membersByEntityQuery = `
	SELECT e.entity_id, e.entity_name, e.entity_type,
	       e.relative_path, coalesce(e.language, ''), f.token_count,
	       e.start_line, e.end_line
	FROM code_function_fingerprint f
	JOIN content_entities e ON e.entity_id = f.entity_id AND e.repo_id = f.repo_id
	WHERE f.repo_id = $1 AND f.entity_id = ANY($2)
	ORDER BY e.entity_id
`
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "divergence_members_by_entity"),
			attribute.String("db.sql.table", "code_function_fingerprint,content_entities"),
		),
	)
	defer span.End()

	rows, err := cr.db.QueryContext(ctx, membersByEntityQuery, repoID, array.Of(entityIDs))
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("divergence members by entity: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byID := make(map[string]codedivergence.Member, len(entityIDs))
	for rows.Next() {
		var member codedivergence.Member
		if err := rows.Scan(
			&member.EntityID,
			&member.EntityName,
			&member.EntityType,
			&member.RelativePath,
			&member.Language,
			&member.TokenCount,
			&member.StartLine,
			&member.EndLine,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan divergence member by entity: %w", err)
		}
		byID[member.EntityID] = member
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return byID, nil
}

// DriftedFindingStats returns one stat per active drifted finding in the
// repo, keyed like the equality kinds (finding id as fingerprint, pair
// token max) for the merged cross-kind page.
func (cr *ContentReader) DriftedFindingStats(ctx context.Context, repoID string) ([]codedivergence.GroupStat, error) {
	if cr == nil || cr.db == nil {
		return nil, nil
	}
	return cr.driftedFindingStore().DriftedFindingStats(ctx, repoID)
}

// DriftedFindingRows hydrates exactly the given finding ids into drifted
// rows keyed by finding id, so a large active set never turns hydration
// into a full-repo fetch.
func (cr *ContentReader) DriftedFindingRows(ctx context.Context, repoID string, findingIDs []string) (map[string]codedivergence.DriftedRow, error) {
	if cr == nil || cr.db == nil {
		return map[string]codedivergence.DriftedRow{}, nil
	}
	return cr.driftedFindingStore().DriftedFindingRows(ctx, repoID, findingIDs)
}

// DivergenceMembers hydrates the members of exactly the given fingerprints
// in one repository, keyed by fingerprint, so a large group list never
// turns hydration into a full-repo fetch.
func (cr *ContentReader) DivergenceMembers(ctx context.Context, repoID string, kind codedivergence.Kind, fingerprints []string) (map[string][]codedivergence.Member, error) {
	if cr == nil || cr.db == nil {
		return nil, nil
	}
	if len(fingerprints) == 0 {
		return map[string][]codedivergence.Member{}, nil
	}
	column := divergenceColumn(kind)

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "divergence_members"),
			attribute.String("db.sql.table", "code_function_fingerprint,content_entities"),
		),
	)
	defer span.End()

	// #nosec G201 -- column is one of two literals; the rest is static SQL with $N args
	membersQuery := fmt.Sprintf(divergenceMembersQuery, "f."+column, "f."+column, "f."+column)
	rows, err := cr.db.QueryContext(ctx, membersQuery, repoID, array.Of(fingerprints))
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("divergence members: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byFingerprint := make(map[string][]codedivergence.Member, len(fingerprints))
	for rows.Next() {
		var fingerprint string
		var member codedivergence.Member
		if err := rows.Scan(
			&fingerprint,
			&member.EntityID,
			&member.EntityName,
			&member.EntityType,
			&member.RelativePath,
			&member.Language,
			&member.TokenCount,
			&member.StartLine,
			&member.EndLine,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan divergence member: %w", err)
		}
		byFingerprint[fingerprint] = append(byFingerprint[fingerprint], member)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return byFingerprint, nil
}
