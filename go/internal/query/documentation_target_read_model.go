package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const documentationTargetFactPreviewLimit = 10

type documentationTargetScope struct {
	Repository string `json:"repository,omitempty"`
	TargetKind string `json:"target_kind,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	ServiceID  string `json:"service_id,omitempty"`
}

type documentationTargetCoverage struct {
	Target              documentationTargetScope `json:"target,omitempty"`
	FindingsReturned    int                      `json:"findings_returned"`
	TargetFactCount     int                      `json:"target_fact_count"`
	TargetFactKinds     map[string]int           `json:"target_fact_kinds,omitempty"`
	SourceOnlyCount     int                      `json:"source_only_count,omitempty"`
	SourceOnlyFactKinds map[string]int           `json:"source_only_fact_kinds,omitempty"`
	Truncated           bool                     `json:"truncated"`
}

type documentationMissingEvidence struct {
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

type documentationTargetRef struct {
	kind string
	id   string
}

func documentationFindingsResponse(readModel documentationFindingListReadModel) map[string]any {
	findings := readModel.Findings
	if findings == nil {
		findings = []map[string]any{}
	}
	body := map[string]any{
		"findings":    findings,
		"next_cursor": readModel.NextCursor,
	}
	if !readModel.hasTargetReadback() {
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

func (m documentationFindingListReadModel) hasTargetReadback() bool {
	return m.Coverage.Target.hasSelector() ||
		m.Coverage.TargetFactCount > 0 ||
		m.Coverage.SourceOnlyCount > 0 ||
		len(m.RelatedFacts) > 0 ||
		len(m.MissingEvidence) > 0
}

func (s documentationTargetScope) hasSelector() bool {
	return strings.TrimSpace(s.Repository) != "" ||
		strings.TrimSpace(s.TargetID) != "" ||
		strings.TrimSpace(s.ServiceID) != ""
}

func documentationTargetScopeFromFindingFilter(filter documentationFindingFilter) documentationTargetScope {
	return documentationTargetScopeFromValues(
		filter.Repository,
		filter.TargetKind,
		filter.TargetID,
		filter.ServiceID,
	)
}

func documentationTargetScopeFromFactFilter(filter documentationFactFilter) documentationTargetScope {
	return documentationTargetScopeFromValues(
		filter.Repository,
		filter.TargetKind,
		filter.TargetID,
		filter.ServiceID,
	)
}

func documentationTargetScopeFromValues(repository, targetKind, targetID, serviceID string) documentationTargetScope {
	scope := documentationTargetScope{
		Repository: strings.TrimSpace(repository),
		ServiceID:  strings.TrimSpace(serviceID),
	}
	targetKind = strings.TrimSpace(targetKind)
	targetID = strings.TrimSpace(targetID)
	if scope.ServiceID != "" {
		scope.TargetKind = "service"
		scope.TargetID = scope.ServiceID
	}
	if scope.TargetID == "" && targetID != "" {
		scope.TargetKind = targetKind
		scope.TargetID = targetID
	}
	if scope.TargetID == "" && scope.Repository != "" &&
		(targetKind == "" || targetKind == "repository") {
		scope.TargetKind = "repository"
		scope.TargetID = scope.Repository
	}
	return scope
}

func documentationTargetRefsFromFindingFilter(filter documentationFindingFilter) []documentationTargetRef {
	return documentationTargetRefs(documentationTargetScopeFromFindingFilter(filter))
}

func documentationTargetRefsFromFactFilter(filter documentationFactFilter) []documentationTargetRef {
	return documentationTargetRefs(documentationTargetScopeFromFactFilter(filter))
}

func documentationTargetRefs(scope documentationTargetScope) []documentationTargetRef {
	refs := []documentationTargetRef{}
	if scope.ServiceID != "" {
		refs = append(refs, documentationTargetRef{kind: "service", id: scope.ServiceID})
	}
	if scope.TargetID != "" {
		refs = append(refs, documentationTargetRef{kind: scope.TargetKind, id: scope.TargetID})
	}
	if len(refs) == 0 && scope.Repository != "" {
		refs = append(refs, documentationTargetRef{kind: "repository", id: scope.Repository})
	}
	return uniqueDocumentationTargetRefs(refs)
}

func uniqueDocumentationTargetRefs(refs []documentationTargetRef) []documentationTargetRef {
	seen := map[string]struct{}{}
	out := make([]documentationTargetRef, 0, len(refs))
	for _, ref := range refs {
		ref.kind = strings.TrimSpace(ref.kind)
		ref.id = strings.TrimSpace(ref.id)
		if ref.id == "" {
			continue
		}
		key := ref.kind + "\x00" + ref.id
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func documentationTargetPredicate(args []any, payloadExpr string, refs []documentationTargetRef) (string, []any) {
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
	refs []documentationTargetRef,
) ([]string, []any) {
	predicate, args := documentationTargetPredicate(args, payloadExpr, refs)
	if predicate == "" {
		return clauses, args
	}
	return append(clauses, predicate), args
}

func documentationTargetContainsPayloads(ref documentationTargetRef) []string {
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

func documentationTargetRefObject(ref documentationTargetRef, kindKey, idKey string) map[string]string {
	out := map[string]string{idKey: ref.id}
	if ref.kind != "" {
		out[kindKey] = ref.kind
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
		Target:           documentationTargetScopeFromFindingFilter(filter),
		FindingsReturned: documentationTargetFindingsReturned(filter, findings),
		TargetFactCount:  len(relatedFacts),
		TargetFactKinds:  kinds,
		Truncated:        truncated,
	}
}

func documentationTargetFindingsReturned(filter documentationFindingFilter, findings []map[string]any) int {
	if !documentationFindingFilterHasExplicitTarget(filter) {
		return len(findings)
	}
	refs := documentationTargetRefsFromFindingFilter(filter)
	if len(refs) == 0 {
		return len(findings)
	}
	count := 0
	for _, finding := range findings {
		if documentationPayloadMatchesTargetRefs(finding, refs) {
			count++
		}
	}
	return count
}

func documentationFindingFilterHasExplicitTarget(filter documentationFindingFilter) bool {
	return strings.TrimSpace(filter.TargetID) != "" || strings.TrimSpace(filter.ServiceID) != ""
}

func documentationPayloadMatchesTargetRefs(payload map[string]any, refs []documentationTargetRef) bool {
	for _, ref := range refs {
		if documentationPayloadMatchesTargetRef(payload, ref) {
			return true
		}
	}
	nested, _ := payload["payload"].(map[string]any)
	if len(nested) == 0 {
		return false
	}
	for _, ref := range refs {
		if documentationPayloadMatchesTargetRef(nested, ref) {
			return true
		}
	}
	return false
}

func documentationPayloadMatchesTargetRef(payload map[string]any, ref documentationTargetRef) bool {
	return documentationRefListMatchesTarget(payload["candidate_refs"], ref, "kind", "id") ||
		documentationRefListMatchesTarget(payload["evidence_refs"], ref, "kind", "id") ||
		documentationRefListMatchesTarget(payload["linked_entities"], ref, "entity_type", "entity_id")
}

func documentationRefListMatchesTarget(raw any, ref documentationTargetRef, kindKey, idKey string) bool {
	switch values := raw.(type) {
	case []any:
		for _, value := range values {
			if documentationRefObjectMatchesTarget(value, ref, kindKey, idKey) {
				return true
			}
		}
	case []map[string]any:
		for _, value := range values {
			if documentationRefObjectMatchesTarget(value, ref, kindKey, idKey) {
				return true
			}
		}
	case []map[string]string:
		for _, value := range values {
			if documentationStringRefObjectMatchesTarget(value, ref, kindKey, idKey) {
				return true
			}
		}
	}
	return false
}

func documentationRefObjectMatchesTarget(raw any, ref documentationTargetRef, kindKey, idKey string) bool {
	value, _ := raw.(map[string]any)
	if len(value) == 0 {
		return false
	}
	id := strings.TrimSpace(documentationStringAny(value[idKey]))
	if id == "" || id != ref.id {
		return false
	}
	if ref.kind == "" {
		return true
	}
	return strings.TrimSpace(documentationStringAny(value[kindKey])) == ref.kind
}

func documentationStringRefObjectMatchesTarget(
	value map[string]string,
	ref documentationTargetRef,
	kindKey string,
	idKey string,
) bool {
	id := strings.TrimSpace(value[idKey])
	if id == "" || id != ref.id {
		return false
	}
	if ref.kind == "" {
		return true
	}
	return strings.TrimSpace(value[kindKey]) == ref.kind
}

func documentationStringAny(raw any) string {
	value, _ := raw.(string)
	return value
}

func documentationMissingEvidenceForTarget(coverage documentationTargetCoverage) []documentationMissingEvidence {
	if !coverage.Target.hasSelector() {
		return nil
	}
	if coverage.FindingsReturned > 0 {
		return nil
	}
	if coverage.TargetFactCount > 0 {
		return []documentationMissingEvidence{{
			Reason: "documentation_findings_absent",
			Detail: "target documentation facts exist but no admissible documentation findings matched the target scope",
		}}
	}
	if coverage.SourceOnlyCount > 0 {
		return []documentationMissingEvidence{{
			Reason: "target_link_not_modeled",
			Detail: "external documentation facts exist, but none carry structured refs for the selected target scope",
		}}
	}
	return []documentationMissingEvidence{{
		Reason: "documentation_target_facts_absent",
		Detail: "no collected documentation facts referenced the selected target scope",
	}}
}

func (cr *ContentReader) documentationTargetFacts(
	ctx context.Context,
	filter documentationFindingFilter,
) ([]map[string]any, bool, error) {
	if cr == nil || cr.db == nil || !documentationTargetScopeFromFindingFilter(filter).hasSelector() {
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
		documentationTargetRefsFromFindingFilter(filter),
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
