// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// groupedSupplementFindingsCTE is the grouped inventory's winners finding set for
// route V (unscoped profile=precise, #10880 PR-B2a): the shipped grouped_findings
// CTE with the precise predicate on the winner row (branch A), unioned with the
// supplement's picks (branch B, supplement_sql.go) under the same name, so the
// fan-out and tail that follow are the shipped text. The grouped statement binds
// the aggregate numbering ($1-$20) plus limit and offset ($21, $22).
var groupedSupplementFindingsCTE = mustDeriveGroupedSupplement(groupedInventoryFindingsCTE)

func mustDeriveGroupedSupplement(winners string) string {
	text, err := deriveGroupedSupplement(winners)
	if err != nil {
		panic(err)
	}
	return text
}

func deriveGroupedSupplement(winners string) (string, error) {
	text, err := supplementProfileBlock(winners, "    AND (\n          $12 = ''\n", "    AND ($13 = '' OR w.priority_bucket = $13)", "w", "$12")
	if err != nil {
		return "", err
	}
	if text, err = replaceOnce(text, "grouped_findings AS MATERIALIZED (\n  SELECT w.repository_id,", "winners_branch AS MATERIALIZED (\n  SELECT w.repository_id,", "grouped_findings CTE"); err != nil {
		return "", err
	}
	return text + supplementCTEs(aggregateSupplementBinds) + `,
grouped_findings AS MATERIALIZED (
  SELECT repository_id, impact_status, severity_bucket, priority_score FROM winners_branch
  UNION ALL
  SELECT repository_id, impact_status, severity_bucket, priority_score FROM branch_b
)`, nil
}

// groupedInventorySupplementQuery builds the route V grouped inventory query for
// one dimension. It takes the same 22 arguments as groupedInventoryQuery.
func groupedInventorySupplementQuery(dimension GroupedInventoryDimension) (string, error) {
	return groupedInventoryBuildQuery(dimension, groupedSupplementFindingsCTE, groupedInventoryTailSQL)
}
