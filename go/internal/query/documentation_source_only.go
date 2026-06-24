// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/lib/pq"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (cr *ContentReader) documentationSourceOnlySummary(
	ctx context.Context,
	filter documentationFindingFilter,
) (documentationTargetCoverage, error) {
	if cr == nil || cr.db == nil || !documentationTargetScopeFromFindingFilter(filter).hasSelector() {
		return documentationTargetCoverage{}, nil
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "count_documentation_source_only_facts"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	query, args := buildDocumentationSourceOnlySQL(filter)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return documentationTargetCoverage{}, fmt.Errorf("query source-only documentation facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			span.RecordError(err)
			return documentationTargetCoverage{}, fmt.Errorf("query source-only documentation facts: %w", err)
		}
		return documentationTargetCoverage{}, nil
	}
	var coverage documentationTargetCoverage
	var sourceCount, documentCount, sectionCount, linkCount int
	if err := rows.Scan(
		&coverage.SourceOnlyCount,
		&sourceCount,
		&documentCount,
		&sectionCount,
		&linkCount,
	); err != nil {
		span.RecordError(err)
		return documentationTargetCoverage{}, fmt.Errorf("query source-only documentation facts: %w", err)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return documentationTargetCoverage{}, fmt.Errorf("query source-only documentation facts: %w", err)
	}
	coverage.SourceOnlyFactKinds = documentationSourceOnlyFactKinds(sourceCount, documentCount, sectionCount, linkCount)
	return coverage, nil
}

func buildDocumentationSourceOnlySQL(filter documentationFindingFilter) (string, []any) {
	args := []any{pq.Array(documentationSourceOnlyFactKindsList())}
	clauses := []string{
		"fact.fact_kind = ANY($1::text[])",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		`NOT (
      (jsonb_typeof(fact.payload->'candidate_refs') = 'array' AND jsonb_array_length(fact.payload->'candidate_refs') > 0)
   OR (jsonb_typeof(fact.payload->'evidence_refs') = 'array' AND jsonb_array_length(fact.payload->'evidence_refs') > 0)
   OR (jsonb_typeof(fact.payload->'linked_entities') = 'array' AND jsonb_array_length(fact.payload->'linked_entities') > 0)
  )`,
	}
	addColumnFilter := func(column, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	addPayloadFilter := func(field, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("fact.payload->>'%s' = $%d", field, len(args)))
	}
	addColumnFilter("fact.scope_id", filter.ScopeID)
	addColumnFilter("fact.generation_id", filter.GenerationID)
	addPayloadFilter("source_id", filter.SourceID)
	addPayloadFilter("document_id", filter.DocumentID)
	clauses, args = appendDocumentationAuthorizationClause(
		clauses,
		args,
		"fact",
		"scope",
		filter.AllowedRepositoryIDs,
		filter.AllowedScopeIDs,
	)
	return fmt.Sprintf(
		`
SELECT
    COUNT(*) AS documentation_source_only_count,
    COUNT(*) FILTER (WHERE fact.fact_kind = '%s') AS documentation_source_fact_count,
    COUNT(*) FILTER (WHERE fact.fact_kind = '%s') AS documentation_document_fact_count,
    COUNT(*) FILTER (WHERE fact.fact_kind = '%s') AS documentation_section_fact_count,
    COUNT(*) FILTER (WHERE fact.fact_kind = '%s') AS documentation_link_fact_count
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE %s
`,
		facts.DocumentationSourceFactKind,
		facts.DocumentationDocumentFactKind,
		facts.DocumentationSectionFactKind,
		facts.DocumentationLinkFactKind,
		strings.Join(clauses, " AND "),
	), args
}

func documentationSourceOnlyFactKindsList() []string {
	return []string{
		facts.DocumentationSourceFactKind,
		facts.DocumentationDocumentFactKind,
		facts.DocumentationSectionFactKind,
		facts.DocumentationLinkFactKind,
	}
}

func documentationSourceOnlyFactKinds(sourceCount, documentCount, sectionCount, linkCount int) map[string]int {
	kinds := map[string]int{}
	if sourceCount > 0 {
		kinds[facts.DocumentationSourceFactKind] = sourceCount
	}
	if documentCount > 0 {
		kinds[facts.DocumentationDocumentFactKind] = documentCount
	}
	if sectionCount > 0 {
		kinds[facts.DocumentationSectionFactKind] = sectionCount
	}
	if linkCount > 0 {
		kinds[facts.DocumentationLinkFactKind] = linkCount
	}
	return kinds
}
