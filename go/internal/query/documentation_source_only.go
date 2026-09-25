// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (cr *ContentReader) documentationSourceOnlySummary(
	ctx context.Context,
	filter documentationFindingFilter,
) (documentationTargetCoverage, error) {
	if cr == nil || cr.db == nil ||
		!querycontract.DocumentationTargetScopeHasSelector(querycontract.DocumentationTargetScopeFromFindingFilter(filter)) {
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
	var args []any
	clauses := []string{
		"fact.fact_kind IN (" + documentationSourceOnlyKindLiterals() + ")",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		documentationNoStructuredRefsPredicate("fact.payload"),
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

// documentationSourceOnlyKindLiterals renders the counted documentation fact
// kinds as a SQL literal list. The list is inlined, never bound as a
// parameter: migration 123's partial index predicate carries the same literal
// kinds, and Postgres can prove `fact_kind IN (literals)` implies that
// predicate in a generic plan, whereas `fact_kind = ANY($1)` cannot be proven
// until the parameter is known. The kinds are compile-time constants from the
// facts package, never caller input.
func documentationSourceOnlyKindLiterals() string {
	kinds := documentationSourceOnlyFactKindsList()
	quoted := make([]string, len(kinds))
	for i, kind := range kinds {
		quoted[i] = "'" + kind + "'"
	}
	return strings.Join(quoted, ", ")
}

// documentationNoStructuredRefsPredicate renders "this fact carries no
// structured target refs": none of candidate_refs, evidence_refs, or
// linked_entities is a non-empty array. A missing or JSON-null key means no
// refs. The COALESCE keeps the predicate two-valued: with a bare
// `jsonb_typeof(NULL) = 'array'` the disjunct is SQL NULL for an absent key
// and NOT(NULL) excludes the row, so the count could only be nonzero for facts
// that carry all three keys (#7126). `<> '[]'` is "non-empty" for an array and
// raises no error on a scalar, unlike jsonb_array_length. Migration 123's
// partial index carries this exact text over the bare `payload` column so the
// planner can prove the statement implies the index predicate.
func documentationNoStructuredRefsPredicate(payload string) string {
	refKey := func(key string) string {
		return fmt.Sprintf(
			"COALESCE(jsonb_typeof(%[1]s->'%[2]s') = 'array' AND %[1]s->'%[2]s' <> '[]'::jsonb, FALSE)",
			payload, key,
		)
	}
	return "NOT (\n      " + refKey("candidate_refs") +
		"\n   OR " + refKey("evidence_refs") +
		"\n   OR " + refKey("linked_entities") + "\n  )"
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
