// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"strings"
)

// aggregateSupportsSourceCTE is the aggregates' source CTE over the supports
// table: the same builder and the same predicates as the granted list read, in
// the aggregates' bind order ($1-$7 key filters, $8-$10 runtime filters, $12
// profile, $17 image ref, $18/$19 allowed repository and scope ids, $21-$26 the
// caller authorization arguments).
var aggregateSupportsBinds = supportsSourceBinds{
	columns: `fact.fact_id, fact.scope_id, fact.suppression_state, fact.priority_score,
         fact.has_payload_finding_id, fact.canonical_key, fact.impact_status,
         fact.priority_bucket, fact.severity_bucket, fact.repository_id, fact.ecosystem`,
	cve: "$1", pkg: "$2", repo: "$3", digest: "$4", status: "$5", advisory: "$6", ecosystem: "$7",
	service: "$8", workload: "$9", environment: "$10", profile: "$12", image: "$17",
	repoGrant: "$18", scopeGrant: "$19",
	protectedFlag: "$21", protected2: "$22", protected3: "$23", protected4: "$24", protected5: "$25", protectedList: "$26",
}
var aggregateSupportsSourceCTE = supportsSourceCTE(aggregateSupportsBinds)

// aggregateSupportsRankFilter is the canonical_facts predicate of the supports
// aggregates. The rank filters (severity $11, priority bucket $13, minimum
// priority score $14) apply to the picked row, ahead of the suppression filters,
// exactly as the list read does (arbiter ruling for #10880 PR-B1, Q2), so a
// count equals the number of rows the list returns for the same filter.
const aggregateSupportsRankFilter = `  WHERE ($11 = '' OR fact.severity_bucket = $11)
    AND ($13 = '' OR fact.priority_bucket = $13)
    AND ($14 = 0 OR fact.priority_score >= $14)
    AND ($15 = '' OR fact.suppression_state = $15)`

// AggregateSupportsCanonicalFactsCTE is the aggregates' canonical-facts CTE for a
// granted call, derived from the legacy AggregateCanonicalFactsCTE: the text
// before the source candidates, the winner pick, the suppression overlay and the
// canonical_facts tail are the legacy text unchanged. Each replaced block is
// located by an explicit marker; a marker that no longer matches panics at package
// initialization, and TestAggregateSupportsDerivationFailsOnALostMarker pins each.
var AggregateSupportsCanonicalFactsCTE = mustDeriveAggregateSupportsCTE(AggregateCanonicalFactsCTE)

// AggregateSupportsCombinedCountQuery is AggregateCombinedCountQuery over the
// supports CTE. The select that follows the CTE is the legacy text, taken from
// the legacy constant by prefix.
var AggregateSupportsCombinedCountQuery = AggregateSupportsCanonicalFactsCTE +
	strings.TrimPrefix(AggregateCombinedCountQuery, AggregateCanonicalFactsCTE)

// InventorySupportsQueryTemplate is InventoryQueryTemplate over the supports CTE.
var InventorySupportsQueryTemplate = AggregateSupportsCanonicalFactsCTE +
	strings.TrimPrefix(InventoryQueryTemplate, AggregateCanonicalFactsCTE)

func mustDeriveAggregateSupportsCTE(legacy string) string {
	query, err := deriveAggregateSupportsCTE(legacy)
	if err != nil {
		panic(err)
	}
	return query
}

// deriveAggregateSupportsCTE swaps each fact_records block of the legacy
// canonical-facts CTE for its supports equivalent, returning an error that names
// a marker the legacy text no longer carries.
func deriveAggregateSupportsCTE(legacy string) (string, error) {
	return deriveSupportsCanonicalFactsCTE(legacy, aggregateSupportsSourceCTE)
}

// deriveSupportsCanonicalFactsCTE is deriveAggregateSupportsCTE with the source
// CTE chosen by the caller, so the grouped inventory, which binds no caller
// authorization arguments, shares the derivation without those guards.
func deriveSupportsCanonicalFactsCTE(legacy, sourceCTE string) (string, error) {
	cut := func(text, from, to string) (before, after string, err error) {
		start := strings.Index(text, from)
		if start < 0 {
			return "", "", fmt.Errorf("legacy aggregate CTE lost the marker %q: update the supports derivation", from)
		}
		end := strings.Index(text[start:], to)
		if end < 0 {
			return "", "", fmt.Errorf("legacy aggregate CTE lost the marker %q after %q: update the supports derivation", to, from)
		}
		return text[:start], text[start+end:], nil
	}
	// source_candidates and operator_candidates become the supports source CTE.
	head, rest, err := cut(legacy, "\nsource_candidates AS (", "\nsource_winners AS (")
	if err != nil {
		return "", err
	}
	query := head + sourceCTE + "," + rest
	// The legacy operator_overrides CTE and its join become the lateral probe.
	head, rest, err = cut(query, "operator_overrides AS (", "joined_winners AS (")
	if err != nil {
		return "", err
	}
	query = head + rest
	const overrideJoin = "LEFT JOIN operator_overrides AS override\n    ON override.canonical_key = source.canonical_key"
	if !strings.Contains(query, overrideJoin) {
		return "", fmt.Errorf("legacy aggregate CTE lost the operator_overrides join: update the supports derivation")
	}
	query = strings.Replace(query, overrideJoin, supportsListOverrideLateral, 1)
	// joined_winners carries the priority score so canonical_facts can rank-filter.
	const joinedSelect = "SELECT source.impact_status,"
	if !strings.Contains(query, joinedSelect) {
		return "", fmt.Errorf("legacy aggregate CTE lost the joined_winners select: update the supports derivation")
	}
	query = strings.Replace(query, joinedSelect, "SELECT source.priority_score,\n         source.impact_status,", 1)
	const canonicalHead = "canonical_facts AS (\n  SELECT impact_status,"
	if !strings.Contains(query, canonicalHead) {
		return "", fmt.Errorf("legacy aggregate CTE lost the canonical_facts select: update the supports derivation")
	}
	query = strings.Replace(query, canonicalHead, "canonical_facts AS (\n  SELECT priority_score,\n         impact_status,", 1)
	const suppressionFilter = "  WHERE ($15 = '' OR fact.suppression_state = $15)"
	if !strings.Contains(query, suppressionFilter) {
		return "", fmt.Errorf("legacy aggregate CTE lost the canonical_facts filter: update the supports derivation")
	}
	return strings.Replace(query, suppressionFilter, aggregateSupportsRankFilter, 1), nil
}
