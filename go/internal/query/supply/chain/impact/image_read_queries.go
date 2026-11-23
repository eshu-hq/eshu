// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "strings"

// The unscoped image_ref reads of #10880 PR-B2a (arbiter ruling V, index amendment).
// An image reference is a per-support column, so the winners row cannot answer
// it: copies of one key can name different images. The read serves the supports
// of the image through the partial index supply_chain_impact_finding_supports_image_ref_idx
// (migration 344), picks the best support per key among them (filter then pick,
// ruling Q2) and applies the rank filters to that pick.
//
// The candidate fetch is AS MATERIALIZED and that is part of the contract: without
// it the planner drives the read from the scope tables and filters on the image,
// measured at about 10 s for a typical image against 9 to 19 ms with it
// (docs/internal/evidence/10880-pr-b2a/RESULTS.md). The fetch is not capped or
// truncated: a cap would change the answer.

// imageFetchCTE is the materialized candidate fetch of an image read: every
// support of the image, found through the partial index. imageParam is the bound
// image_ref parameter.
func imageFetchCTE(imageParam string) string {
	return "\nimage_supports AS MATERIALIZED (\n  SELECT * FROM supply_chain_impact_finding_supports\n  WHERE image_ref = " + imageParam + " AND image_ref <> ''\n),"
}

// imageSourceCTE is the source CTE of an image read: the materialized fetch, then
// the shared supports source reading from it.
func imageSourceCTE(b supportsSourceBinds) string {
	b.from = "image_supports AS fact"
	return imageFetchCTE(b.image) + supportsSourceCTE(b)
}

// ListFindingsFromSupportsImageQuery is the unscoped image_ref list read. It takes
// the same 30 parameters as ListFindingsQuery.
var ListFindingsFromSupportsImageQuery = mustDeriveImageList(ListFindingsQuery)

func mustDeriveImageList(legacy string) string {
	query, err := deriveSupportsListQueryFrom(legacy, imageSourceCTE(supportsListBinds))
	if err != nil {
		panic(err)
	}
	return query
}

// AggregateSupportsImageCanonicalFactsCTE is the aggregates' canonical-facts CTE of
// an unscoped image_ref call.
var AggregateSupportsImageCanonicalFactsCTE = mustDeriveImageAggregate()

func mustDeriveImageAggregate() string {
	text, err := deriveSupportsCanonicalFactsCTE(AggregateCanonicalFactsCTE, imageSourceCTE(aggregateSupportsBinds))
	if err != nil {
		panic(err)
	}
	return text
}

// AggregateSupportsImageCombinedCountQuery is the unscoped image_ref count read.
var AggregateSupportsImageCombinedCountQuery = AggregateSupportsImageCanonicalFactsCTE +
	strings.TrimPrefix(AggregateCombinedCountQuery, AggregateCanonicalFactsCTE)

// AggregateSupportsImageInventoryQueryTemplate is the unscoped image_ref inventory read.
var AggregateSupportsImageInventoryQueryTemplate = AggregateSupportsImageCanonicalFactsCTE +
	strings.TrimPrefix(InventoryQueryTemplate, AggregateCanonicalFactsCTE)

// groupedImageCanonicalFactsCTE is the grouped inventory's canonical-facts CTE of an
// unscoped image_ref call.
var groupedImageCanonicalFactsCTE = mustDeriveImageGrouped()

func mustDeriveImageGrouped() string {
	text, err := deriveSupportsCanonicalFactsCTE(AggregateCanonicalFactsCTE, imageSourceCTE(groupedSupportsBinds))
	if err != nil {
		panic(err)
	}
	return text
}

// groupedInventoryImageQuery builds the unscoped image_ref grouped inventory query
// for one dimension. It takes the same 22 arguments as groupedInventoryQuery and
// the unscoped tail: with no grant there is no hidden bucket to mark partial.
func groupedInventoryImageQuery(dimension GroupedInventoryDimension) (string, error) {
	mapRows, err := groupedInventoryMapRowsSQL(dimension)
	if err != nil {
		return "", err
	}
	query := groupedImageCanonicalFactsCTE + `,
grouped_findings AS MATERIALIZED (
  SELECT repository_id, impact_status, severity_bucket, priority_score
  FROM canonical_facts
),
grouped_map_rows AS MATERIALIZED (` + mapRows + `
)` + groupedInventoryTailSQL
	return supplyChainImpactReadModeSQL(query, false), nil
}

// inventoryImageQuery inserts one inventory dimension expression into the
// unscoped image_ref inventory read.
func inventoryImageQuery(groupExpr string) string {
	return strings.Replace(
		AggregateSupportsImageInventoryQueryTemplate,
		supplyChainImpactInventoryGroupExpressionPlaceholder,
		groupExpr,
		1,
	)
}
