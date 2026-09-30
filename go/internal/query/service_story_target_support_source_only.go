// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B4 EntityHandler/ContentReader seam stayer for #6060: methods on those types must stay in package query, so this file cannot move to service/

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
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

// serviceStoryTargetSupportUnlinkedPredicate is "this active support fact has no
// durable link to any target". Only a work_item.external_link that carries a
// linked_repository_id is linked (to some repository); every other support kind
// has no target key yet, so it is source-only. The predicate is two-valued: the
// kind comparison is never NULL and NULLIF(...) IS NOT NULL is never NULL, so
// NOT of it cannot drop a row to UNKNOWN. A row linked to a different
// repository is neither evidence for this target nor source-only.
const serviceStoryTargetSupportUnlinkedPredicate = "NOT (fact.fact_kind = '" + storySupportLinkFactKind +
	"' AND NULLIF(fact.payload->>'" + storySupportLinkPayloadKey + "', '') IS NOT NULL)"

func buildServiceStoryTargetSupportSourceOnlySQL(factKinds []string) (string, []any) {
	if len(factKinds) == 0 {
		return "", nil
	}
	return `
SELECT
    COUNT(*) AS support_source_only_count,
    COUNT(*) FILTER (WHERE fact.fact_kind LIKE 'work_item.%') AS work_item_source_only_count,
    COUNT(*) FILTER (WHERE fact.fact_kind LIKE 'incident_routing.%') AS incident_routing_source_only_count
` + serviceStoryTargetSupportActiveFactsFrom("fact.fact_kind", []string{
		"fact.is_tombstone = FALSE",
		serviceStoryTargetSupportUnlinkedPredicate,
	}) + `
`, []any{array.Of(factKinds)}
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
		"detail": "Jira or PagerDuty support facts exist, but no fact carries a durable link to the selected target",
	}}
}
