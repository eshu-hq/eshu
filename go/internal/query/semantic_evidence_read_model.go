// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func buildSemanticEvidenceSQL(filter semanticEvidenceFilter) (string, []any) {
	args := []any{}
	clauses := []string{
		"fact_records.fact_kind = '" + filter.FactKind + "'",
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
	addPayloadFilter := func(expr, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("%s = $%d", expr, len(args)))
	}

	addColumnFilter("fact_records.fact_id", filter.FactID)
	addColumnFilter("fact_records.scope_id", filter.ScopeID)
	addColumnFilter("fact_records.generation_id", filter.GenerationID)
	addPayloadFilter("fact_records.payload->'source'->>'source_class'", filter.SourceClass)
	addPayloadFilter("fact_records.payload->'source'->>'source_id'", filter.SourceID)
	addPayloadFilter("fact_records.payload->'source'->>'document_id'", filter.DocumentID)
	addPayloadFilter("fact_records.payload->'source'->>'section_id'", filter.SectionID)
	addPayloadFilter("fact_records.payload->'source'->>'relative_path'", filter.RelativePath)
	addPayloadFilter("fact_records.payload->'subject'->>'entity_id'", filter.EntityID)
	addPayloadFilter("fact_records.payload->'provider'->>'provider_profile_id'", filter.ProviderProfileID)
	addPayloadFilter("fact_records.payload->'provider'->>'provider_kind'", filter.ProviderKind)
	addPayloadFilter("fact_records.payload->'chunk'->>'prompt_version'", filter.PromptVersion)
	addPayloadFilter("fact_records.payload->'chunk'->>'redaction_version'", filter.RedactionVersion)
	addPayloadFilter("fact_records.payload->'chunk'->>'extraction_mode'", filter.ExtractionMode)
	addPayloadFilter("fact_records.payload->>'policy_state'", filter.PolicyState)
	addPayloadFilter("fact_records.payload->>'redaction_state'", filter.RedactionState)
	addPayloadFilter("fact_records.payload->>'freshness_state'", filter.FreshnessState)
	addPayloadFilter("fact_records.payload->>'admission_state'", filter.AdmissionState)
	addPayloadFilter("fact_records.payload->>'corroboration_state'", filter.CorroborationState)
	addPayloadFilter("fact_records.payload->>'observation_type'", filter.ObservationType)
	addPayloadFilter("fact_records.payload->>'hint_type'", filter.HintType)
	addPayloadFilter("fact_records.payload->>'relationship_kind'", filter.RelationshipKind)
	clauses, args = appendSemanticEvidenceRepositoryClause(clauses, args, filter)
	clauses, args = appendSemanticEvidenceAuthorizationClause(clauses, args, filter)
	if strings.TrimSpace(filter.Query) != "" {
		args = append(args, "%"+strings.ToLower(strings.TrimSpace(filter.Query))+"%")
		clauses = append(clauses, fmt.Sprintf(`LOWER(
			COALESCE(fact_records.payload->>'observation_type', '') || ' ' ||
			COALESCE(fact_records.payload->>'observation_text', '') || ' ' ||
			COALESCE(fact_records.payload->>'hint_type', '') || ' ' ||
			COALESCE(fact_records.payload->>'relationship_kind', '') || ' ' ||
			COALESCE(fact_records.payload->>'hint_text', '')
		) LIKE $%d`, len(args)))
	}
	if filter.UpdatedSince != nil {
		args = append(args, *filter.UpdatedSince)
		clauses = append(clauses, fmt.Sprintf("fact_records.observed_at >= $%d", len(args)))
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit+1, filter.Offset)
	return fmt.Sprintf(`
SELECT jsonb_build_object(
    'fact_id', fact_records.fact_id,
    'fact_kind', fact_records.fact_kind,
    'scope_id', fact_records.scope_id,
    'generation_id', fact_records.generation_id,
    'source_system', fact_records.source_system,
    'observed_at', fact_records.observed_at,
    'payload', fact_records.payload
) AS payload
FROM fact_records
WHERE %s
ORDER BY fact_records.observed_at DESC, fact_records.fact_id DESC
LIMIT $%d OFFSET $%d
`, strings.Join(clauses, " AND "), len(args)-1, len(args)), args
}

func appendSemanticEvidenceRepositoryClause(
	clauses []string,
	args []any,
	filter semanticEvidenceFilter,
) ([]string, []any) {
	if strings.TrimSpace(filter.Repository) != "" {
		args = append(args, strings.TrimSpace(filter.Repository))
		repositoryPredicate := fmt.Sprintf("fact_records.payload->'source'->>'repository_id' = $%d", len(args))
		targetPredicate, nextArgs := documentationTargetPredicate(
			args,
			"fact_records.payload",
			documentationTargetRefsFromSemanticEvidenceFilter(filter),
		)
		args = nextArgs
		if targetPredicate != "" {
			repositoryPredicate = "(" + repositoryPredicate + " OR " + targetPredicate + ")"
		}
		clauses = append(clauses, repositoryPredicate)
		return clauses, args
	}
	return appendDocumentationTargetClause(
		clauses,
		args,
		"fact_records.payload",
		documentationTargetRefsFromSemanticEvidenceFilter(filter),
	)
}

func appendSemanticEvidenceAuthorizationClause(
	clauses []string,
	args []any,
	filter semanticEvidenceFilter,
) ([]string, []any) {
	ids := uniqueSemanticEvidenceAccessIDs(filter.AllowedRepositoryIDs, filter.AllowedScopeIDs)
	if len(ids) == 0 {
		return clauses, args
	}
	placeholders := make([]string, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	inList := strings.Join(placeholders, ", ")
	clauses = append(clauses, fmt.Sprintf(`(
	fact_records.scope_id IN (%[1]s)
	OR fact_records.payload->'source'->>'repository_id' IN (%[1]s)
	OR fact_records.payload->'subject'->>'repository_id' IN (%[1]s)
	OR EXISTS (
		SELECT 1
		FROM jsonb_array_elements(COALESCE(fact_records.payload->'object_refs', '[]'::jsonb)) AS object_ref
		WHERE object_ref->>'repository_id' IN (%[1]s)
	)
)`, inList))
	return clauses, args
}

func uniqueSemanticEvidenceAccessIDs(groups ...[]string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, group := range groups {
		for _, value := range group {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func documentationTargetRefsFromSemanticEvidenceFilter(filter semanticEvidenceFilter) []documentationTargetRef {
	return documentationTargetRefs(documentationTargetScopeFromValues(
		filter.Repository,
		filter.TargetKind,
		filter.TargetID,
		filter.ServiceID,
	))
}

func semanticEvidencePublicRow(raw map[string]any) map[string]any {
	payload := mapValue(raw, "payload")
	factKind := stringFromMap(raw, "fact_kind")
	out := map[string]any{
		"fact_id":     stringFromMap(raw, "fact_id"),
		"fact_kind":   factKind,
		"truth_basis": semanticEvidenceTruthBasis(factKind),
	}
	copyString(out, raw, "scope_id")
	copyString(out, raw, "generation_id")
	copyString(out, raw, "source_system")
	copyString(out, raw, "observed_at")

	for _, key := range []string{
		"observation_id",
		"observation_type",
		"observation_text",
		"observation_hash",
		"hint_id",
		"hint_type",
		"relationship_kind",
		"hint_text",
		"hint_hash",
		"confidence",
		"unsupported_reason",
		"freshness_state",
		"policy_state",
		"redaction_state",
		"redaction_summary",
		"admission_state",
		"corroboration_state",
		"promotion_policy",
		"observed_at",
	} {
		copyString(out, payload, key)
	}
	copyAny(out, payload, "missing_evidence")
	copyFilteredMapList(out, payload, "evidence_refs", []string{"kind", "id", "uri", "confidence"})
	copyFilteredMapList(out, payload, "object_refs", []string{
		"repository_id", "relative_path", "entity_kind", "entity_id", "line_start", "line_end",
	})
	copyFilteredNestedMap(out, payload, "source", []string{
		"source_id", "source_class", "source_handle", "repository_id", "document_id",
		"relative_path", "external_anchor", "section_id", "line_start", "line_end",
		"page_start", "page_end",
	})
	copyFilteredNestedMap(out, payload, "chunk", []string{
		"chunk_id", "chunk_hash", "source_hash", "prompt_version",
		"redaction_version", "extractor_version", "extraction_mode",
	})
	copyFilteredNestedMap(out, payload, "provider", []string{
		"provider_profile_id", "provider_kind", "model_id", "endpoint_profile_id",
	})
	copyFilteredNestedMap(out, payload, "subject", []string{
		"repository_id", "relative_path", "entity_kind", "entity_id", "line_start", "line_end",
	})
	copyNestedString(out, payload, "provider", "provider_profile_id")
	copyNestedString(out, payload, "provider", "provider_kind")
	copyNestedString(out, payload, "provider", "model_id")
	copyNestedString(out, payload, "provider", "endpoint_profile_id")
	copyNestedString(out, payload, "chunk", "prompt_version")
	copyNestedString(out, payload, "chunk", "redaction_version")
	copyNestedString(out, payload, "chunk", "extractor_version")
	copyNestedString(out, payload, "chunk", "extraction_mode")
	// Honest source-ACL disclosure (#2164 USER-APPROVED policy). source_acl_state
	// is a distinct access-posture axis surfaced alongside (never folded into)
	// policy_state/freshness_state/admission_state/corroboration_state/
	// redaction_state. Semantic-evidence facts carry no per-caller documentation
	// permissions object, so the bounded source_acl_state on the payload's
	// acl_summary is the sole posture signal: denied withholds the observation
	// content (observation_text/hint_text/evidence_refs) behind an access-denied
	// disposition, partial withholds content behind a partial marker, stale is
	// surfaced as stale with content intact, missing is disclosed as missing, and
	// allowed / no-claim stays visible. The #2138 truth labels (freshness_state,
	// admission_state, corroboration_state, missing_evidence, unsupported_reason)
	// are preserved on a withheld row and never collapsed into the access marker.
	// Surface the bounded state from the raw payload onto the projected row first,
	// then enforce disclosure against the projected row.
	surfaceSourceACLState(out, payload)
	applySourceACLDisclosure(out, true)
	return out
}

func semanticEvidenceTruthBasis(factKind string) string {
	switch factKind {
	case facts.SemanticDocumentationObservationFactKind:
		return "semantic_observation"
	case facts.SemanticCodeHintFactKind:
		return "code_hint"
	default:
		return "semantic_evidence"
	}
}

func copyString(out map[string]any, values map[string]any, key string) {
	if value := stringFromMap(values, key); value != "" {
		out[key] = value
	}
}

func copyAny(out map[string]any, values map[string]any, key string) {
	if value, ok := values[key]; ok {
		out[key] = value
	}
}

func copyFilteredMapList(out map[string]any, values map[string]any, key string, allowed []string) {
	raw, ok := values[key].([]any)
	if !ok {
		return
	}
	filteredRows := make([]any, 0, len(raw))
	for _, item := range raw {
		values, ok := item.(map[string]any)
		if !ok {
			continue
		}
		filtered := make(map[string]any, len(allowed))
		for _, allowedKey := range allowed {
			if value, ok := values[allowedKey]; ok {
				filtered[allowedKey] = value
			}
		}
		if len(filtered) > 0 {
			filteredRows = append(filteredRows, filtered)
		}
	}
	if len(filteredRows) > 0 {
		out[key] = filteredRows
	}
}

func copyFilteredNestedMap(out map[string]any, values map[string]any, key string, allowed []string) {
	source := mapValue(values, key)
	if len(source) == 0 {
		return
	}
	filtered := make(map[string]any, len(allowed))
	for _, allowedKey := range allowed {
		if value, ok := source[allowedKey]; ok {
			filtered[allowedKey] = value
		}
	}
	if len(filtered) > 0 {
		out[key] = filtered
	}
}

func copyNestedString(out map[string]any, values map[string]any, objectKey, valueKey string) {
	if value := nestedString(values, objectKey, valueKey); value != "" {
		out[valueKey] = value
	}
}
