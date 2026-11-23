// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "strings"

// aggregateSupplementBinds is the aggregates' parameter numbering ($1-$7 key
// filters, $8-$10 runtime filters, $11 severity, $13 priority bucket, $14 minimum
// score, $15 suppression state, $16 include suppressed, $20 read clock).
var aggregateSupplementBinds = supplementBinds{
	cve: "$1", pkg: "$2", repo: "$3", digest: "$4", status: "$5", advisory: "$6", ecosystem: "$7",
	service: "$8", workload: "$9", environment: "$10",
	severity: "$11", bucket: "$13", minScore: "$14", suppState: "$15", includeSuppressed: "$16", readAt: "$20",
}

// aggregateSupplementFindingsCTE is the winners aggregate finding set of route V
// (unscoped profile=precise, #10880 PR-B2a): the shipped aggregate_winners CTE with
// the precise predicate on the winner row (branch A), unioned with the supplement's
// picks (branch B, supplement_sql.go), under the same aggregate_winners name so
// the shipped count and inventory text that follows it is unchanged.
var aggregateSupplementFindingsCTE = mustDeriveAggregateSupplement(aggregateWinnersFindingsCTE)

func mustDeriveAggregateSupplement(winners string) string {
	text, err := deriveAggregateSupplement(winners)
	if err != nil {
		panic(err)
	}
	return text
}

func deriveAggregateSupplement(winners string) (string, error) {
	text, err := supplementProfileBlock(winners, "    AND (\n          $12 = ''\n", "    AND ($13 = '' OR w.priority_bucket = $13)", "w", "$12")
	if err != nil {
		return "", err
	}
	if text, err = replaceOnce(text, "aggregate_winners AS NOT MATERIALIZED (\n  SELECT w.impact_status,", "winners_branch AS NOT MATERIALIZED (\n  SELECT w.impact_status,", "aggregate_winners CTE"); err != nil {
		return "", err
	}
	return text + supplementCTEs(aggregateSupplementBinds) + `,
aggregate_winners AS NOT MATERIALIZED (
  SELECT impact_status, priority_bucket, severity_bucket, repository_id, ecosystem FROM winners_branch
  UNION ALL
  SELECT impact_status, priority_bucket, severity_bucket, repository_id,
         LOWER((ecosystem) COLLATE "C") COLLATE "default" AS ecosystem
  FROM branch_b
)`, nil
}

// aggregateSupplementFromClause opens every route V aggregate query, as
// aggregateWinnersFromClause opens the winners ones.
func aggregateSupplementFromClause() string {
	return supplyChainImpactReadModeSQL(
		`WITH `+supplyChainImpactRuntimeFilterCTE("$8", "$9", "$10", "$18", "$19")+`,`+aggregateSupplementFindingsCTE,
		false,
	)
}

// AggregateSupplementCombinedCountQuery is AggregateWinnersCombinedCountQuery over
// the route V finding set. The select that follows the finding set is the shipped
// text, taken from the winners query by prefix.
var AggregateSupplementCombinedCountQuery = aggregateSupplementFromClause() +
	strings.TrimPrefix(AggregateWinnersCombinedCountQuery, aggregateWinnersFromClause())

// AggregateSupplementInventoryQueryTemplate is AggregateWinnersInventoryQueryTemplate
// over the route V finding set.
var AggregateSupplementInventoryQueryTemplate = aggregateSupplementFromClause() +
	strings.TrimPrefix(AggregateWinnersInventoryQueryTemplate, aggregateWinnersFromClause())

// AggregateSupplementInventoryQuery inserts one inventory dimension expression.
func AggregateSupplementInventoryQuery(groupExpr string) string {
	return strings.Replace(
		AggregateSupplementInventoryQueryTemplate,
		supplyChainImpactInventoryGroupExpressionPlaceholder,
		groupExpr,
		1,
	)
}
