// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/lib/pq"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const serviceStoryTargetSupportLimit = 10

type serviceStoryTargetSupportStore interface {
	serviceStoryTargetSupportEvidence(
		context.Context,
		serviceStoryTargetSupportFilter,
	) (serviceStoryTargetSupportReadModel, error)
}

type serviceStoryTargetSupportFilter struct {
	Repository string
	TargetKind string
	TargetID   string
	ServiceID  string
	Limit      int
}

type serviceStoryTargetSupportReadModel struct {
	Support map[string]any
}

func loadServiceStoryTargetSupportForOperation(
	ctx context.Context,
	content ContentStore,
	workloadContext map[string]any,
	operation string,
) (map[string]any, error) {
	if strings.TrimSpace(operation) != "service_story" {
		return nil, nil
	}
	return loadServiceStoryTargetSupport(ctx, content, workloadContext)
}

func loadServiceStoryTargetSupport(
	ctx context.Context,
	content ContentStore,
	workloadContext map[string]any,
) (map[string]any, error) {
	store, ok := content.(serviceStoryTargetSupportStore)
	if !ok || store == nil {
		return nil, nil
	}
	repoID := safeStr(workloadContext, "repo_id")
	serviceID := safeStr(workloadContext, "id")
	if repoID == "" && serviceID == "" {
		return nil, nil
	}
	filter := serviceStoryTargetSupportFilter{
		Repository: repoID,
		Limit:      serviceStoryTargetSupportLimit,
	}
	if serviceID != "" {
		filter.TargetKind = "service"
		filter.TargetID = serviceID
		filter.ServiceID = serviceID
	} else {
		filter.TargetKind = "repository"
		filter.TargetID = repoID
	}
	readModel, err := store.serviceStoryTargetSupportEvidence(ctx, filter)
	if err != nil {
		return nil, err
	}
	return readModel.Support, nil
}

func loadRepositoryStoryTargetSupport(
	ctx context.Context,
	content ContentStore,
	repoID string,
) (map[string]any, error) {
	store, ok := content.(serviceStoryTargetSupportStore)
	if !ok || store == nil {
		return nil, nil
	}
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return nil, nil
	}
	readModel, err := store.serviceStoryTargetSupportEvidence(ctx, serviceStoryTargetSupportFilter{
		Repository: repoID,
		TargetKind: "repository",
		TargetID:   repoID,
		Limit:      serviceStoryTargetSupportLimit,
	})
	if err != nil {
		return nil, err
	}
	return readModel.Support, nil
}

func (cr *ContentReader) serviceStoryTargetSupportEvidence(
	ctx context.Context,
	filter serviceStoryTargetSupportFilter,
) (serviceStoryTargetSupportReadModel, error) {
	if cr == nil || cr.db == nil {
		return serviceStoryTargetSupportReadModel{}, nil
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_service_story_target_support"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	factKinds := serviceStoryTargetSupportFactKinds()
	query, args := buildServiceStoryTargetSupportSQL(filter)
	if query == "" {
		return serviceStoryTargetSupportReadModel{}, nil
	}
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return serviceStoryTargetSupportReadModel{}, fmt.Errorf("query service story target support: %w", err)
	}
	defer func() { _ = rows.Close() }()

	limit := serviceStoryTargetSupportRowLimit(filter.Limit)
	facts := make([]map[string]any, 0, limit)
	for rows.Next() {
		payload, err := scanJSONPayload(rows)
		if err != nil {
			span.RecordError(err)
			return serviceStoryTargetSupportReadModel{}, fmt.Errorf("query service story target support: %w", err)
		}
		facts = append(facts, payload)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return serviceStoryTargetSupportReadModel{}, fmt.Errorf("query service story target support: %w", err)
	}
	truncated := len(facts) > limit
	if truncated {
		facts = facts[:limit]
	}
	var sourceOnlySummary serviceStoryTargetSupportSourceOnlySummary
	if len(facts) == 0 && documentationTargetScopeFromValues(
		filter.Repository,
		filter.TargetKind,
		filter.TargetID,
		filter.ServiceID,
	).hasSelector() {
		sourceOnlySummary, err = cr.serviceStoryTargetSupportSourceOnlySummary(ctx, factKinds)
		if err != nil {
			span.RecordError(err)
			return serviceStoryTargetSupportReadModel{}, err
		}
	}
	return serviceStoryTargetSupportReadModel{
		Support: buildStoryTargetSupportWithSourceOnlySummary(filter, facts, truncated, sourceOnlySummary),
	}, nil
}

func buildServiceStoryTargetSupportSQL(filter serviceStoryTargetSupportFilter) (string, []any) {
	refs := serviceStorySupportTargetRefs(filter)
	if len(refs) == 0 {
		return "", nil
	}
	args := []any{}
	factKinds := serviceStoryTargetSupportFactKinds()
	args = append(args, pq.Array(factKinds))
	clauses := []string{
		"fact.fact_kind = ANY($1::text[])",
		"fact.is_tombstone = FALSE",
	}
	clauses, args = appendDocumentationTargetClause(
		clauses,
		args,
		"fact.payload",
		refs,
	)
	limit := serviceStoryTargetSupportRowLimit(filter.Limit)
	args = append(args, limit+1)
	return fmt.Sprintf(`
SELECT jsonb_build_object(
    'fact_id', fact.fact_id,
    'fact_kind', fact.fact_kind,
    'scope_id', fact.scope_id,
    'generation_id', fact.generation_id,
    'source_system', fact.source_system,
    'source_record_id', fact.source_record_id,
    'observed_at', fact.observed_at,
    'payload', fact.payload
) AS payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE %s
  AND generation.status = 'active'
ORDER BY fact.observed_at DESC, fact.fact_id DESC
LIMIT $%d
`, strings.Join(clauses, " AND "), len(args)), args
}

func serviceStoryTargetSupportFactKinds() []string {
	kinds := append([]string{}, workItemEvidenceFactKinds...)
	kinds = append(
		kinds,
		"incident_routing.applied_pagerduty_resource",
		"incident_routing.observed_pagerduty_service",
		"incident_routing.coverage_warning",
	)
	return kinds
}

func buildStoryTargetSupport(
	filter serviceStoryTargetSupportFilter,
	facts []map[string]any,
	truncated bool,
) map[string]any {
	refs := serviceStorySupportTargetRefs(filter)
	evidence := make([]map[string]any, 0, len(facts))
	ambiguous := make([]map[string]any, 0)
	for _, fact := range facts {
		if serviceStorySupportFactAmbiguousForTarget(fact, refs) {
			ambiguous = append(ambiguous, serviceStorySupportEvidenceRow(fact))
			continue
		}
		if !serviceStorySupportPayloadMatchesTargetRefs(fact, refs) {
			continue
		}
		evidence = append(evidence, serviceStorySupportEvidenceRow(fact))
	}
	out := map[string]any{
		"evidence":               evidence,
		"evidence_count":         len(evidence),
		"work_item_count":        serviceStorySupportFamilyCount(evidence, "work_item."),
		"incident_routing_count": serviceStorySupportFamilyCount(evidence, "incident_routing."),
		"ambiguous_evidence":     ambiguous,
		"ambiguous_count":        len(ambiguous),
		"coverage": map[string]any{
			"target":            serviceStoryTargetSupportScopeMap(filter),
			"target_fact_count": len(evidence) + len(ambiguous),
			"truncated":         truncated,
		},
		"missing_evidence": serviceStorySupportMissingEvidence(filter, evidence, ambiguous),
		"limit":            serviceStoryTargetSupportRowLimit(filter.Limit),
		"source":           "support_read_model",
	}
	return out
}

func serviceStorySupportEvidenceRow(fact map[string]any) map[string]any {
	row := map[string]any{
		"fact_id":       StringVal(fact, "fact_id"),
		"fact_kind":     StringVal(fact, "fact_kind"),
		"scope_id":      StringVal(fact, "scope_id"),
		"generation_id": StringVal(fact, "generation_id"),
		"source_system": StringVal(fact, "source_system"),
		"observed_at":   StringVal(fact, "observed_at"),
	}
	if sourceRecordID := StringVal(fact, "source_record_id"); sourceRecordID != "" {
		row["source_record_id"] = sourceRecordID
	}
	if payload := serviceStorySupportEvidencePayload(mapValue(fact, "payload")); len(payload) > 0 {
		row["payload"] = payload
	}
	return row
}

func serviceStorySupportEvidencePayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	allowed := []string{
		"provider",
		"evidence_state",
		"work_item_key",
		"provider_work_item_id",
		"project_key",
		"issue_type_name",
		"status_name",
		"provider_changelog_id",
		"field",
		"value_redacted",
		"provider_remote_link_id",
		"application_name",
		"application_type",
		"relationship",
		"url_fingerprint",
		"url_present",
		"url_redacted",
		"title_present",
		"summary_present",
		"correlation_anchor_class",
		"provider_support_state",
		"redaction_policy_version",
		"source_class",
		"source_kind",
		"outcome",
		"resource_class",
		"provider_object_id",
		"service_id",
		"status",
		"declared_match_state",
		"drift_candidate_reason",
		"redaction_state",
		"reason",
	}
	out := map[string]any{}
	for _, key := range allowed {
		if value, ok := payload[key]; ok {
			out[key] = value
		}
	}
	return out
}

func serviceStorySupportFactAmbiguousForTarget(fact map[string]any, refs []documentationTargetRef) bool {
	payloads := []map[string]any{fact}
	if payload := mapValue(fact, "payload"); len(payload) > 0 {
		payloads = append(payloads, payload)
	}
	for _, payload := range payloads {
		if supportRefListAmbiguousForTarget(payload["candidate_refs"], refs, "kind", "id") ||
			supportRefListAmbiguousForTarget(payload["evidence_refs"], refs, "kind", "id") ||
			supportRefListAmbiguousForTarget(payload["linked_entities"], refs, "entity_type", "entity_id") {
			return true
		}
	}
	return false
}

func supportRefListAmbiguousForTarget(raw any, refs []documentationTargetRef, kindKey, idKey string) bool {
	matched := false
	other := false
	switch values := raw.(type) {
	case []any:
		for _, value := range values {
			matched, other = supportRefObjectMatchState(value, refs, kindKey, idKey, matched, other)
		}
	case []map[string]any:
		for _, value := range values {
			matched, other = supportRefObjectMatchState(value, refs, kindKey, idKey, matched, other)
		}
	}
	return matched && other
}

func supportRefObjectMatchState(
	raw any,
	refs []documentationTargetRef,
	kindKey string,
	idKey string,
	matched bool,
	other bool,
) (bool, bool) {
	value, _ := raw.(map[string]any)
	if len(value) == 0 {
		return matched, other
	}
	id := strings.TrimSpace(documentationStringAny(value[idKey]))
	if id == "" {
		return matched, other
	}
	kind := strings.TrimSpace(documentationStringAny(value[kindKey]))
	for _, ref := range refs {
		if id == ref.id && (ref.kind == "" || strings.EqualFold(kind, ref.kind)) {
			return true, other
		}
	}
	return matched, true
}

func serviceStorySupportFamilyCount(rows []map[string]any, prefix string) int {
	count := 0
	for _, row := range rows {
		if strings.HasPrefix(StringVal(row, "fact_kind"), prefix) {
			count++
		}
	}
	return count
}

func serviceStoryTargetSupportScopeMap(filter serviceStoryTargetSupportFilter) map[string]any {
	return map[string]any{
		"repository":  strings.TrimSpace(filter.Repository),
		"target_kind": strings.TrimSpace(filter.TargetKind),
		"target_id":   strings.TrimSpace(filter.TargetID),
		"service_id":  strings.TrimSpace(filter.ServiceID),
	}
}

func serviceStorySupportMissingEvidence(
	filter serviceStoryTargetSupportFilter,
	evidence []map[string]any,
	ambiguous []map[string]any,
) []map[string]any {
	if len(evidence) > 0 {
		return []map[string]any{}
	}
	if len(ambiguous) > 0 {
		return []map[string]any{{
			"reason": "support_correlation_ambiguous",
			"detail": "support collector facts reference the selected target and another target, so ownership is ambiguous",
		}}
	}
	if !documentationTargetScopeFromValues(filter.Repository, filter.TargetKind, filter.TargetID, filter.ServiceID).hasSelector() {
		return []map[string]any{}
	}
	return []map[string]any{{
		"reason": "support_target_facts_absent",
		"detail": "no collected Jira or PagerDuty support facts explicitly referenced the selected target scope",
	}}
}

func serviceStoryTargetSupportRowLimit(limit int) int {
	if limit <= 0 || limit > serviceStoryTargetSupportLimit {
		return serviceStoryTargetSupportLimit
	}
	return limit
}
