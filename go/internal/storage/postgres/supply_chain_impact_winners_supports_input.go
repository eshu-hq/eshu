// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"
)

// The winners maintainer's inputs read the supports table (#10880 PR-C1 d). The
// per-support table mirrors every live finding fact column for column that the
// winner select reads (fact_id, scope_id, generation_id, source_confidence and the
// payload verbatim), so each maintainer statement is its legacy statement with the
// row source swapped: the table, and the kind and tombstone predicates the table
// makes implicit (a support exists only for a live finding fact). Each supports
// statement below is derived from the shipped legacy constant by exactly those
// swaps, never copied, so the select, the keyed probe and the operator insert
// cannot drift from what the audit runs.
//
// The audit and the staged audit scan keep the legacy statements: they are the
// oracle the supports-derived winners are compared with. The probe's index on the
// supports table is migration 354 (supply_chain_impact_finding_supports_keyed_probe_idx),
// which mirrors fact_records_supply_chain_impact_winners_keyed_idx; this file
// changes the row source only.

// winnersSupportsInputSwap is one legacy fragment and the supports fragment that
// replaces it.
type winnersSupportsInputSwap struct{ legacy, supports string }

const winnersSupportsTable = "supply_chain_impact_finding_supports"

// winnersSupportsInputSwaps lists, per statement, the swaps that turn the legacy
// fact statement into its supports twin. Each fragment must occur exactly once in
// its statement.
var (
	winnersActiveFactsSwaps = []winnersSupportsInputSwap{
		{"FROM fact_records AS fact\n    JOIN ingestion_scopes", "FROM " + winnersSupportsTable + " AS fact\n    JOIN ingestion_scopes"},
		{"WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'\n      AND fact.is_tombstone = FALSE\n      AND generation.status = 'active'", "WHERE generation.status = 'active'"},
	}
	winnersScopeIdentsSwaps = []winnersSupportsInputSwap{
		{"JOIN fact_records AS fact\n  ON fact.scope_id = pending.scope_id", "JOIN " + winnersSupportsTable + " AS fact\n  ON fact.scope_id = pending.scope_id"},
		{" AND fact.generation_id = pending.generation_id\nWHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'\n  AND fact.is_tombstone = FALSE", " AND fact.generation_id = pending.generation_id"},
	}
	winnersKeyedActiveSwaps = []winnersSupportsInputSwap{
		{"FROM fact_records AS f\n        WHERE (f.payload", "FROM " + winnersSupportsTable + " AS f\n        WHERE (f.payload"},
		{"= ident.advisory_id\n          AND f.fact_kind = 'reducer_supply_chain_impact_finding'\n          AND f.is_tombstone = FALSE", "= ident.advisory_id"},
	}
	// winnersKeyedFetchSwaps moves the keyed final fetch from the facts to the
	// persisted narrow probe output's supports: the narrow table already holds
	// the probe's (fact_id, scope_id, generation_id) rows, so only the wide
	// row source changes.
	winnersKeyedFetchSwaps = []winnersSupportsInputSwap{
		{"FROM fact_records AS fact\nJOIN " + winnersKeyedNarrowTable, "FROM " + winnersSupportsTable + " AS fact\nJOIN " + winnersKeyedNarrowTable},
	}
	winnersKeyedOperatorSwaps = []winnersSupportsInputSwap{
		{"FROM fact_records AS fact\nJOIN ingestion_scopes", "FROM " + winnersSupportsTable + " AS fact\nJOIN ingestion_scopes"},
		{"WHERE fact.scope_id = 'operator:vulnerability_suppressions'\n  AND fact.fact_kind = 'reducer_supply_chain_impact_finding'\n  AND fact.is_tombstone = FALSE", "WHERE fact.scope_id = 'operator:vulnerability_suppressions'"},
	}
)

// winnersSupportsInputGroups is every swap group. A group is recognised in a
// statement by its first swap's supports fragment, which names the supports table
// in that group's own context, so reversing one statement never touches a
// fragment that merely looks like another group's.
func winnersSupportsInputGroups() [][]winnersSupportsInputSwap {
	return [][]winnersSupportsInputSwap{
		winnersActiveFactsSwaps, winnersScopeIdentsSwaps, winnersKeyedActiveSwaps, winnersKeyedFetchSwaps, winnersKeyedOperatorSwaps,
	}
}

// applyWinnersSupportsInputSwaps returns legacy with each swap applied once. A
// swap that does not match exactly once means the legacy statement changed
// shape, and the swap would silently stop applying: it panics at package
// initialization instead, so the build cannot run with a half-moved input.
func applyWinnersSupportsInputSwaps(name, legacy string, swaps []winnersSupportsInputSwap) string {
	out := legacy
	for _, swap := range swaps {
		if n := strings.Count(out, swap.legacy); n != 1 {
			panic(fmt.Sprintf("winners supports input %s: legacy fragment %q occurs %d times, want 1", name, swap.legacy, n))
		}
		out = strings.Replace(out, swap.legacy, swap.supports, 1)
	}
	return out
}

// reverseWinnersSupportsInput turns every swap group found in statement back into
// its legacy fragments. A statement that carries no group comes back unchanged.
func reverseWinnersSupportsInput(statement string) string {
	out := statement
	for _, group := range winnersSupportsInputGroups() {
		if !strings.Contains(out, group[0].supports) {
			continue
		}
		for _, swap := range group {
			out = strings.Replace(out, swap.supports, swap.legacy, 1)
		}
	}
	return out
}

var (
	// supplyChainImpactWinnerActiveFactsFromSupportsSQL opens the full select with
	// the active supports of every active scope generation.
	supplyChainImpactWinnerActiveFactsFromSupportsSQL = applyWinnersSupportsInputSwaps(
		"full active facts", supplyChainImpactWinnerActiveFactsSQL, winnersActiveFactsSwaps)
	// supplyChainImpactWinnerSelectFromSupportsSQL is the winner select the
	// maintainer's rebuild runs; the audit keeps supplyChainImpactWinnerSelectSQL.
	supplyChainImpactWinnerSelectFromSupportsSQL = supplyChainImpactWinnerActiveFactsFromSupportsSQL + supplyChainImpactWinnerBodySQL
	// buildWinnersKeyedScopeIdentsFromSupportsSQL expands scope rows to identifiers
	// through the supports table.
	buildWinnersKeyedScopeIdentsFromSupportsSQL = applyWinnersSupportsInputSwaps(
		"keyed scope idents", buildWinnersKeyedScopeIdentsSQL, winnersScopeIdentsSwaps)
	// buildWinnersKeyedNarrowFromSupportsSQL persists the identifier probe from
	// the supports of exactly the identifiers in the identifier table: the
	// watermark-stall fix moved the probe text verbatim into the narrow
	// statement, so the same keyed-active swaps derive it.
	buildWinnersKeyedNarrowFromSupportsSQL = applyWinnersSupportsInputSwaps(
		"keyed narrow probe", buildWinnersKeyedNarrowSQL, winnersKeyedActiveSwaps)
	// buildWinnersKeyedActiveFactsFromSupportsSQL builds the keyed active_facts
	// from the supports backing the persisted narrow probe rows.
	buildWinnersKeyedActiveFactsFromSupportsSQL = applyWinnersSupportsInputSwaps(
		"keyed active facts", buildWinnersKeyedActiveFactsSQL, winnersKeyedFetchSwaps)
	// addWinnersKeyedOperatorFactsFromSupportsSQL adds every active operator
	// suppression support to the keyed active_facts.
	addWinnersKeyedOperatorFactsFromSupportsSQL = applyWinnersSupportsInputSwaps(
		"keyed operator facts", addWinnersKeyedOperatorFactsSQL, winnersKeyedOperatorSwaps)
)
