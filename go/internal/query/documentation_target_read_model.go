// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const documentationTargetFactPreviewLimit = 10

// The target readback types are aliases onto querycontract, so this package's
// call sites keep their unexported spelling while a ContentStore double
// outside package query can still name them (#6060).
type (
	documentationTargetScope     = querycontract.DocumentationTargetScope
	documentationTargetCoverage  = querycontract.DocumentationTargetCoverage
	documentationMissingEvidence = querycontract.DocumentationMissingEvidence
)

func documentationFindingsResponse(readModel documentationFindingListReadModel) map[string]any {
	findings := readModel.Findings
	if findings == nil {
		findings = []map[string]any{}
	}
	body := map[string]any{
		"findings":    findings,
		"next_cursor": readModel.NextCursor,
	}
	if !documentationFindingListHasTargetReadback(readModel) {
		return body
	}
	relatedFacts := readModel.RelatedFacts
	if relatedFacts == nil {
		relatedFacts = []map[string]any{}
	}
	missingEvidence := readModel.MissingEvidence
	if missingEvidence == nil {
		missingEvidence = []documentationMissingEvidence{}
	}
	body["coverage"] = readModel.Coverage
	body["related_facts"] = relatedFacts
	body["missing_evidence"] = missingEvidence
	return body
}

// documentationFindingListHasTargetReadback reports whether the read model
// carries a target readback worth emitting. It is a function rather than a
// method because documentationFindingListReadModel is now an alias onto
// querycontract, and Go only allows methods in the package that declares the
// type.
func documentationFindingListHasTargetReadback(m documentationFindingListReadModel) bool {
	return querycontract.DocumentationTargetScopeHasSelector(m.Coverage.Target) ||
		m.Coverage.TargetFactCount > 0 ||
		m.Coverage.SourceOnlyCount > 0 ||
		len(m.RelatedFacts) > 0 ||
		len(m.MissingEvidence) > 0
}

// querycontract.DocumentationTargetScopeHasSelector reports whether the scope names anything
// to anchor a readback to. TargetKind alone does not count: a kind with no id
// selects every target of that kind, which is not an anchor. It is a function
// for the same aliasing reason as the readback check above.

func documentationTargetPredicate(args []any, payloadExpr string, refs []querycontract.DocumentationTargetRef) (string, []any) {
	predicates := []string{}
	for _, ref := range refs {
		for _, contains := range documentationTargetContainsPayloads(ref) {
			args = append(args, contains)
			predicates = append(predicates, fmt.Sprintf("%s @> $%d::jsonb", payloadExpr, len(args)))
		}
	}
	if len(predicates) == 0 {
		return "", args
	}
	return "(" + strings.Join(predicates, " OR ") + ")", args
}

func appendDocumentationTargetClause(
	clauses []string,
	args []any,
	payloadExpr string,
	refs []querycontract.DocumentationTargetRef,
) ([]string, []any) {
	predicate, args := documentationTargetPredicate(args, payloadExpr, refs)
	if predicate == "" {
		return clauses, args
	}
	return append(clauses, predicate), args
}

func documentationTargetContainsPayloads(ref querycontract.DocumentationTargetRef) []string {
	contains := []map[string]any{
		{"candidate_refs": []map[string]string{documentationTargetRefObject(ref, "kind", "id")}},
		{"evidence_refs": []map[string]string{documentationTargetRefObject(ref, "kind", "id")}},
		{"linked_entities": []map[string]string{documentationTargetRefObject(ref, "entity_type", "entity_id")}},
	}
	out := make([]string, 0, len(contains))
	for _, value := range contains {
		raw, err := json.Marshal(value)
		if err != nil {
			continue
		}
		out = append(out, string(raw))
	}
	return out
}

func documentationTargetRefObject(ref querycontract.DocumentationTargetRef, kindKey, idKey string) map[string]string {
	out := map[string]string{idKey: ref.ID}
	if ref.Kind != "" {
		out[kindKey] = ref.Kind
	}
	return out
}

func documentationTargetCoverageFromFacts(
	filter documentationFindingFilter,
	findings []map[string]any,
	relatedFacts []map[string]any,
	truncated bool,
) documentationTargetCoverage {
	kinds := map[string]int{}
	for _, fact := range relatedFacts {
		kind := stringFromMap(fact, "fact_kind")
		if kind == "" {
			continue
		}
		kinds[kind]++
	}
	return documentationTargetCoverage{
		Target:           querycontract.DocumentationTargetScopeFromFindingFilter(filter),
		FindingsReturned: documentationTargetFindingsReturned(filter, findings),
		TargetFactCount:  len(relatedFacts),
		TargetFactKinds:  kinds,
		Truncated:        truncated,
	}
}

func documentationTargetFindingsReturned(filter documentationFindingFilter, findings []map[string]any) int {
	if !querycontract.DocumentationFindingFilterHasExplicitTarget(filter) {
		return len(findings)
	}
	refs := querycontract.DocumentationTargetRefsFromFindingFilter(filter)
	if len(refs) == 0 {
		return len(findings)
	}
	count := 0
	for _, finding := range findings {
		if querycontract.DocumentationPayloadMatchesTargetRefs(finding, refs) {
			count++
		}
	}
	return count
}

func (cr *ContentReader) documentationTargetFacts(
	ctx context.Context,
	filter documentationFindingFilter,
) ([]map[string]any, bool, error) {
	if cr == nil || cr.db == nil ||
		!querycontract.DocumentationTargetScopeHasSelector(querycontract.DocumentationTargetScopeFromFindingFilter(filter)) {
		return nil, false, nil
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_documentation_target_facts"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	query, args := buildDocumentationTargetFactsSQL(filter)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, false, fmt.Errorf("query documentation target facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	limit := documentationTargetFactLimit(filter.Limit)
	factRows := make([]map[string]any, 0, limit)
	for rows.Next() {
		payload, err := scanJSONPayload(rows)
		if err != nil {
			span.RecordError(err)
			return nil, false, fmt.Errorf("query documentation target facts: %w", err)
		}
		factRows = append(factRows, payload)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, false, fmt.Errorf("query documentation target facts: %w", err)
	}
	truncated := len(factRows) > limit
	if truncated {
		factRows = factRows[:limit]
	}
	return factRows, truncated, nil
}

func buildDocumentationTargetFactsSQL(filter documentationFindingFilter) (string, []any) {
	args := []any{}
	clauses := []string{
		"fact_records.fact_kind IN ('" + facts.DocumentationEntityMentionFactKind + "', '" + facts.DocumentationClaimCandidateFactKind + "', '" + facts.SemanticDocumentationObservationFactKind + "')",
		"fact_records.is_tombstone = FALSE",
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
		clauses = append(clauses, fmt.Sprintf("fact_records.payload->>'%s' = $%d", field, len(args)))
	}
	addColumnFilter("fact_records.scope_id", filter.ScopeID)
	addColumnFilter("fact_records.generation_id", filter.GenerationID)
	addPayloadFilter("source_id", filter.SourceID)
	addPayloadFilter("document_id", filter.DocumentID)
	clauses, args = appendDocumentationTargetClause(
		clauses,
		args,
		"fact_records.payload",
		querycontract.DocumentationTargetRefsFromFindingFilter(filter),
	)
	clauses, args = appendDocumentationAuthorizationClause(
		clauses,
		args,
		"fact_records",
		"ingestion_scopes",
		filter.AllowedRepositoryIDs,
		filter.AllowedScopeIDs,
	)
	limit := documentationTargetFactLimit(filter.Limit)
	scopeJoin := ""
	if documentationAuthorizationApplies(filter.AllowedRepositoryIDs, filter.AllowedScopeIDs) {
		scopeJoin = "\nLEFT JOIN ingestion_scopes ON ingestion_scopes.scope_id = fact_records.scope_id"
	}
	args = append(args, limit+1)
	return fmt.Sprintf(`
SELECT jsonb_build_object(
    'fact_id', fact_records.fact_id,
    'fact_kind', fact_records.fact_kind,
    'scope_id', fact_records.scope_id,
    'generation_id', fact_records.generation_id,
    'source_system', fact_records.source_system,
    'source_uri', fact_records.source_uri,
    'source_record_id', fact_records.source_record_id,
    'observed_at', fact_records.observed_at,
    'payload', fact_records.payload
) AS payload
FROM fact_records
%s
WHERE %s
ORDER BY fact_records.observed_at DESC, fact_records.fact_id DESC
LIMIT $%d
`, scopeJoin, strings.Join(clauses, " AND "), len(args)), args
}

func documentationTargetFactLimit(limit int) int {
	if limit <= 0 || limit > documentationTargetFactPreviewLimit {
		return documentationTargetFactPreviewLimit
	}
	return limit
}
