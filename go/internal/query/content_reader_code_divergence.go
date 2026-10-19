// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Code divergence reads (epic #6833, child #6836): fingerprint-equality
// grouping over the #6835 side tables. Same Postgres content store and
// required-repo grant discipline as the topic reads in
// content_reader_code_topic.go. Split out of that file for the 500-line cap.

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
	storeDB := &postgres.InstrumentedQueryer{
		Inner:     cr.db,
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
