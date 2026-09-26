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

// codeTopicCandidatePoolBudget/Floor bound the per-term candidate pool
// InvestigateCodeTopic scans before scoring (#7008). candidateCap divides a
// fixed row budget across the term count so scanned rows stay roughly
// constant regardless of term count, with a floor for few-term searches.
const (
	codeTopicCandidatePoolBudget = 4000
	codeTopicCandidatePoolFloor  = 150
)

// codeTopicCandidateCap returns the per-term limit InvestigateCodeTopic applies
// to each static content_entities and content_files branch.
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

// InvestigateCodeTopic scores entities and files in content_entities and
// content_files against req.Terms (name/source-cache substring match for
// entities, path/content substring match for files), ranked by distinct
// term hits and scoped by req.RepoID or, for a corpus-wide search, by
// req.AllowedRepositoryIDs. It is the batched fast path
// codequery.CodeTopicContentInvestigator exposes to CodeHandler.codeTopicRows and
// changeSurfaceTopicRows; the fallback those callers take without a
// satisfying store returns an error rather than a slower equivalent result.
//
// #7008/#7033: entity_probe and file_probe each UNION one independently
// bounded branch per term. The former LATERAL entity form performed a broad
// content_entities scan for every unscoped term before reaching the indexed
// ILIKE predicates; static branches let PostgreSQL select the indexed OR
// predicate before the per-term cap. Keep both OR arms in one predicate: a
// source-cache-only match is eligible under the same contract as a name match.
// entity_pool_capped/file_pool_capped detect, from the already-materialized
// probe rows, whether any term's pool hit its cap; PoolTruncated carries
// that so a capped search reports an explicit marker, not a silent gap.
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

	filters, args, nextArg := codeTopicFilters(req)
	where := ""
	if len(filters) > 0 {
		where = "AND " + strings.Join(filters, " AND ")
	}

	entityBranches := make([]string, len(req.Terms))
	fileBranches := make([]string, len(req.Terms))
	for i, term := range req.Terms {
		// #nosec G201 -- nextArg/where/candidateCap are integer/placeholder-only; no user data concatenated
		entityBranches[i] = fmt.Sprintf(`(SELECT $%[1]d AS matched_term, e.repo_id, e.relative_path, e.entity_id,
		    e.entity_name, e.entity_type, coalesce(e.language, '') AS language, e.start_line, e.end_line
		  FROM content_entities e
		  WHERE (e.entity_name ILIKE '%%' || $%[1]d || '%%'
		         OR e.source_cache ILIKE '%%' || $%[1]d || '%%')
		  %[2]s
		  ORDER BY e.entity_id
		  LIMIT %[3]d)`, nextArg, where, candidateCap)
		// #nosec G201 -- nextArg/where/candidateCap are integer/placeholder-only; no user data concatenated
		fileBranches[i] = fmt.Sprintf(`(SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		    least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $%[1]d AS matched_term
		  FROM content_files f
		  WHERE (f.relative_path ILIKE '%%' || $%[1]d || '%%' OR f.content ILIKE '%%' || $%[1]d || '%%')
		  %[2]s
		  ORDER BY f.repo_id, f.relative_path
		  LIMIT %[3]d)`, nextArg, where, candidateCap)
		args = append(args, term)
		nextArg++
	}
	limitArg, offsetArg := nextArg, nextArg+1
	args = append(args, req.Limit, req.Offset)

	// #nosec G201 -- interpolates integer arg indices and the generated
	// per-term UNION branches above, which contain only $N placeholders
	// and static SQL; no user data concatenated
	query := fmt.Sprintf(`
		WITH entity_probe AS (
		  %[1]s
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
		ORDER BY score DESC, repo_id, relative_path, entity_name, source_kind, entity_id
		LIMIT $%[5]d OFFSET $%[6]d
	`, strings.Join(entityBranches, "\n\t\t  UNION ALL\n"), where, candidateCap, strings.Join(fileBranches, "\n\t\t  UNION ALL\n"), limitArg, offsetArg)

	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		err = contentSubstringIndexReadError(err)
		span.RecordError(err)
		return nil, fmt.Errorf("investigate code topic: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
			span.RecordError(err)
			return nil, fmt.Errorf("scan code topic result: %w", err)
		}
		row.MatchedTerms = splitCodeTopicTerms(matchedTerms)
		if row.PoolTruncated {
			poolTruncated = true
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	span.SetAttributes(attribute.Bool("code_topic.pool_truncated", poolTruncated))
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
