// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"

	"github.com/lib/pq"
)

type serviceStoryTargetSupportSourceOnlySummary struct {
	TotalCount           int
	WorkItemCount        int
	IncidentRoutingCount int
}

func (s serviceStoryTargetSupportSourceOnlySummary) hasEvidence() bool {
	return s.TotalCount > 0 || s.WorkItemCount > 0 || s.IncidentRoutingCount > 0
}

func (cr *ContentReader) serviceStoryTargetSupportSourceOnlySummary(
	ctx context.Context,
	factKinds []string,
) (serviceStoryTargetSupportSourceOnlySummary, error) {
	query, args := buildServiceStoryTargetSupportSourceOnlySQL(factKinds)
	if query == "" {
		return serviceStoryTargetSupportSourceOnlySummary{}, nil
	}
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		return serviceStoryTargetSupportSourceOnlySummary{}, fmt.Errorf("query source-only service story target support: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return serviceStoryTargetSupportSourceOnlySummary{}, fmt.Errorf("query source-only service story target support: %w", err)
		}
		return serviceStoryTargetSupportSourceOnlySummary{}, nil
	}
	var summary serviceStoryTargetSupportSourceOnlySummary
	if err := rows.Scan(&summary.TotalCount, &summary.WorkItemCount, &summary.IncidentRoutingCount); err != nil {
		return serviceStoryTargetSupportSourceOnlySummary{}, fmt.Errorf("query source-only service story target support: %w", err)
	}
	if err := rows.Err(); err != nil {
		return serviceStoryTargetSupportSourceOnlySummary{}, fmt.Errorf("query source-only service story target support: %w", err)
	}
	return summary, nil
}

func buildServiceStoryTargetSupportSourceOnlySQL(factKinds []string) (string, []any) {
	if len(factKinds) == 0 {
		return "", nil
	}
	return `
SELECT
    COUNT(*) AS support_source_only_count,
    COUNT(*) FILTER (WHERE fact.fact_kind LIKE 'work_item.%') AS work_item_source_only_count,
    COUNT(*) FILTER (WHERE fact.fact_kind LIKE 'incident_routing.%') AS incident_routing_source_only_count
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = ANY($1::text[])
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND NOT (
      (jsonb_typeof(fact.payload->'candidate_refs') = 'array' AND jsonb_array_length(fact.payload->'candidate_refs') > 0)
   OR (jsonb_typeof(fact.payload->'evidence_refs') = 'array' AND jsonb_array_length(fact.payload->'evidence_refs') > 0)
   OR (jsonb_typeof(fact.payload->'linked_entities') = 'array' AND jsonb_array_length(fact.payload->'linked_entities') > 0)
  )
`, []any{pq.Array(factKinds)}
}

func buildStoryTargetSupportWithSourceOnlySummary(
	filter serviceStoryTargetSupportFilter,
	facts []map[string]any,
	truncated bool,
	sourceOnlySummary serviceStoryTargetSupportSourceOnlySummary,
) map[string]any {
	out := buildStoryTargetSupport(filter, facts, truncated)
	if !sourceOnlySummary.hasEvidence() {
		return out
	}
	coverage := mapValue(out, "coverage")
	coverage["source_only_count"] = sourceOnlySummary.TotalCount
	coverage["work_item_source_only_count"] = sourceOnlySummary.WorkItemCount
	coverage["incident_routing_source_only_count"] = sourceOnlySummary.IncidentRoutingCount
	out["coverage"] = coverage
	if IntVal(out, "evidence_count") == 0 && IntVal(out, "ambiguous_count") == 0 {
		out["missing_evidence"] = serviceStorySupportSourceOnlyMissingEvidence()
	}
	return out
}

func serviceStorySupportSourceOnlyMissingEvidence() []map[string]any {
	return []map[string]any{{
		"reason": "support_source_only_not_target_linked",
		"detail": "Jira or PagerDuty support facts exist, but none carry structured refs for the selected target scope",
	}}
}
