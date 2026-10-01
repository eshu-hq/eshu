// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B4 EntityHandler/ContentReader seam stayer for #6060: methods on those types must stay in package query, so this file cannot move to service/

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/support"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// serviceStoryTargetSupportStatement is one bounded row read of the target
// support section: the Jira link statement and the PagerDuty routing statement
// are planned and measured separately, so each keeps its own index proof.
type serviceStoryTargetSupportStatement struct {
	query string
	args  []any
}

// buildServiceStoryTargetSupportStatements returns the row reads that can find
// evidence for filter: the work_item.external_link read and, through the
// reducer's incident-repository correlation, the PagerDuty routing read. Both
// are keyed on the same repository id and bounded to limit+1 rows, so both stay
// closed for a service target whose graph gate did not pass (no statement at all).
func buildServiceStoryTargetSupportStatements(
	filter serviceStoryTargetSupportFilter,
) []serviceStoryTargetSupportStatement {
	var statements []serviceStoryTargetSupportStatement
	if query, args := buildServiceStoryTargetSupportSQL(filter); query != "" {
		statements = append(statements, serviceStoryTargetSupportStatement{query: query, args: args})
	}
	routingQuery, routingArgs := support.IncidentRoutingSQL(
		storySupportLinkRepositoryID(filter),
		serviceStoryTargetSupportRowLimit(filter.Limit),
	)
	if routingQuery != "" {
		statements = append(statements, serviceStoryTargetSupportStatement{query: routingQuery, args: routingArgs})
	}
	return statements
}

// queryServiceStoryTargetSupportFacts runs each statement and returns their rows
// merged newest first. Each statement returns at most limit+1 rows in that order,
// so the newest limit+1 overall are always among the merged rows; the merged list
// itself can hold up to one such bound per statement, and the caller truncates it
// to limit and reads a longer list as "the section was truncated".
func (cr *ContentReader) queryServiceStoryTargetSupportFacts(
	ctx context.Context,
	statements []serviceStoryTargetSupportStatement,
	limit int,
) ([]map[string]any, error) {
	facts := make([]map[string]any, 0, limit)
	for _, statement := range statements {
		rows, err := cr.db.QueryContext(ctx, statement.query, statement.args...)
		if err != nil {
			return nil, fmt.Errorf("query service story target support: %w", err)
		}
		scanned, err := scanServiceStoryTargetSupportRows(rows)
		if err != nil {
			return nil, err
		}
		facts = append(facts, scanned...)
	}
	if len(statements) > 1 {
		sortServiceStoryTargetSupportFacts(facts)
	}
	return facts, nil
}

func scanServiceStoryTargetSupportRows(rows db.Rows) ([]map[string]any, error) {
	defer func() { _ = rows.Close() }()
	var facts []map[string]any
	for rows.Next() {
		payload, err := scanJSONPayload(rows)
		if err != nil {
			return nil, fmt.Errorf("query service story target support: %w", err)
		}
		facts = append(facts, payload)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query service story target support: %w", err)
	}
	return facts, nil
}

// sortServiceStoryTargetSupportFacts orders rows newest first, then by fact id
// descending, the order each statement returns them in. observed_at is parsed
// rather than compared as text because the JSON timestamp drops trailing zeros.
func sortServiceStoryTargetSupportFacts(facts []map[string]any) {
	observed := func(fact map[string]any) time.Time {
		parsed, _ := time.Parse(time.RFC3339Nano, StringVal(fact, "observed_at"))
		return parsed
	}
	sort.SliceStable(facts, func(i, j int) bool {
		if left, right := observed(facts[i]), observed(facts[j]); !left.Equal(right) {
			return left.After(right)
		}
		return StringVal(facts[i], "fact_id") > StringVal(facts[j], "fact_id")
	})
}

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
// durable link to any target". A work_item.external_link that carries a
// linked_repository_id is linked (to some repository), and so is a PagerDuty
// applied or observed service whose provider service id the reducer correlated
// to some repository (support.LinkedIncidentRoutingPredicate); every other
// support kind has no target key yet, so it is source-only. The predicate is
// two-valued: the kind comparison is never NULL, NULLIF(...) IS NOT NULL is never
// NULL, and the routing half coalesces its key, so NOT of it cannot drop a row to
// UNKNOWN. A row linked to a different repository is neither evidence for this
// target nor source-only. It reads the admissible_correlations expression the
// statement defines with support.AdmissibleCorrelationsSQL.
var serviceStoryTargetSupportUnlinkedPredicate = "NOT (\n  (fact.fact_kind = '" + storySupportLinkFactKind +
	"' AND NULLIF(fact.payload->>'" + storySupportLinkPayloadKey + "', '') IS NOT NULL)\n  OR " +
	support.LinkedIncidentRoutingPredicate() + "\n)"

func buildServiceStoryTargetSupportSourceOnlySQL(factKinds []string) (string, []any) {
	if len(factKinds) == 0 {
		return "", nil
	}
	return support.AdmissibleCorrelationsSQL() + `
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
