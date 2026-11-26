// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"
)

// The winners audit reads the supports table (#10880 PR-C2 l, arbiter-c1-review section 5).
// Until the write stop the audit read the finding facts as the independent oracle the
// supports-driven winners were compared with. The stop freezes those facts, so an audit that
// still read them would compare today's winners with a stale oracle and repair them toward
// it. Each audit statement is now its legacy statement with the row source swapped, by the
// same declared-swap derivation the maintainer's inputs use (supply_chain_impact_winners_
// supports_input.go): the table, and the kind and tombstone predicates the table makes
// implicit (a support exists only for a live finding). The payload expressions stay byte
// for byte, so the keyed probe index on the supports payload (migration 354) serves the
// audit's range, lateral and probe reads as the fact index did.
//
// After this change the audit compares the winners with the supports only: the D3a sweep
// that runs before the stop is the last independent oracle. The legacy constants stay as the
// base the swaps derive from until PR-C2 m removes the legacy read.

// winnersAuditSupportsSwap is one legacy fragment, the supports fragment that replaces it,
// and how many times it must occur in the statement it is applied to.
type winnersAuditSupportsSwap struct {
	legacy, supports string
	count            int
}

// applyWinnersAuditSupportsSwaps returns legacy with every swap applied. A fragment that
// does not occur exactly its declared number of times means the legacy statement changed
// shape and the swap would silently stop applying, so it panics at package initialization.
func applyWinnersAuditSupportsSwaps(name, legacy string, swaps []winnersAuditSupportsSwap) string {
	out := legacy
	for _, swap := range swaps {
		if n := strings.Count(out, swap.legacy); n != swap.count {
			panic(fmt.Sprintf("winners audit supports input %s: legacy fragment %q occurs %d times, want %d", name, swap.legacy, n, swap.count))
		}
		out = strings.ReplaceAll(out, swap.legacy, swap.supports)
	}
	return out
}

const (
	auditRangeKindFilter = "'reducer_supply_chain_impact_finding'"
	// auditSupportsTable aliases the shared winnersSupportsTable for the audit swaps.
	auditSupportsTable = winnersSupportsTable
)

var (
	// winnersAuditRangeSwaps derive the keyed range statement: the raw range read, the
	// oversize arm and the operator arm of the active facts.
	winnersAuditRangeSwaps = []winnersAuditSupportsSwap{
		{
			"    FROM fact_records AS f\n    WHERE f.fact_kind = " + auditRangeKindFilter + "\n      AND f.is_tombstone = FALSE",
			"    FROM " + auditSupportsTable + " AS f\n    WHERE TRUE", 1,
		},
		{
			"    FROM fact_records AS f\n    WHERE (SELECT oversize FROM range_bounds)\n      AND f.fact_kind = " + auditRangeKindFilter + "\n      AND f.is_tombstone = FALSE\n      AND ",
			"    FROM " + auditSupportsTable + " AS f\n    WHERE (SELECT oversize FROM range_bounds)\n      AND ", 1,
		},
		{
			"    FROM fact_records AS fact\n    JOIN ingestion_scopes AS scope",
			"    FROM " + auditSupportsTable + " AS fact\n    JOIN ingestion_scopes AS scope", 1,
		},
		{
			"      AND fact.fact_kind = " + auditRangeKindFilter + "\n      AND fact.is_tombstone = FALSE\n      AND generation.status = 'active'",
			"      AND generation.status = 'active'", 1,
		},
	}
	// winnersAuditNullProbeSwaps derive the NULL-key probe: its three EXISTS reads.
	winnersAuditNullProbeSwaps = []winnersAuditSupportsSwap{
		{
			"FROM fact_records AS f\n        WHERE f.fact_kind = " + auditRangeKindFilter + " AND f.is_tombstone = FALSE\n          AND ",
			"FROM " + auditSupportsTable + " AS f\n        WHERE ", 3,
		},
	}
	// winnersAuditPendingLateralSwaps derive the pending select's fact arm.
	winnersAuditPendingLateralSwaps = []winnersAuditSupportsSwap{
		{
			"    FROM fact_records AS f\n    WHERE (f.payload->>'package_id') = i.package_id",
			"    FROM " + auditSupportsTable + " AS f\n    WHERE (f.payload->>'package_id') = i.package_id", 1,
		},
		{
			"= i.advisory_id\n      AND f.fact_kind = " + auditRangeKindFilter + "\n      AND f.is_tombstone = FALSE\n    OFFSET 0",
			"= i.advisory_id\n    OFFSET 0", 1,
		},
	}
	// stagedWinnersBatchSwaps derive the staged batch's pinned (scope, generation) read.
	stagedWinnersBatchSwaps = []winnersAuditSupportsSwap{
		{
			"    FROM fact_records AS fact\n    WHERE fact.scope_id = $1\n      AND fact.generation_id = $2\n      AND fact.fact_kind = " + auditRangeKindFilter + "\n      AND fact.is_tombstone = FALSE\n)",
			"    FROM " + auditSupportsTable + " AS fact\n    WHERE fact.scope_id = $1\n      AND fact.generation_id = $2\n)", 1,
		},
	}
	// stagedWinnerFactIdentSwaps derive the staged point fetch of one winner source row.
	stagedWinnerFactIdentSwaps = []winnersAuditSupportsSwap{
		{"FROM fact_records AS fact\nWHERE fact.fact_id = $1", "FROM " + auditSupportsTable + " AS fact\nWHERE fact.fact_id = $1", 1},
	}
)

var (
	// selectWinnersAuditFirstRangeFromSupportsSQL and selectWinnersAuditNextRangeFromSupportsSQL
	// are the sweep's range statements over the supports table.
	selectWinnersAuditFirstRangeFromSupportsSQL = applyWinnersAuditSupportsSwaps(
		"audit first range", selectWinnersAuditFirstRangeSQL, winnersAuditRangeSwaps)
	selectWinnersAuditNextRangeFromSupportsSQL = applyWinnersAuditSupportsSwaps(
		"audit next range", selectWinnersAuditNextRangeSQL, winnersAuditRangeSwaps)
	// probeWinnersAuditNullKeysFromSupportsSQL is the NULL-key probe over the supports table.
	probeWinnersAuditNullKeysFromSupportsSQL = applyWinnersAuditSupportsSwaps(
		"audit null probe", probeWinnersAuditNullKeysSQL, winnersAuditNullProbeSwaps)
	// stagedWinnersBatchActiveFactsFromSupportsSQL is the staged batch's active_facts CTE over
	// the supports table; the source and operator batch statements are built on it.
	stagedWinnersBatchActiveFactsFromSupportsSQL = applyWinnersAuditSupportsSwaps(
		"staged batch active facts", stagedWinnersBatchActiveFactsSQL, stagedWinnersBatchSwaps)
	// stagedWinnerFactIdentFromSupportsSQL point-fetches one winner source row's identifiers.
	stagedWinnerFactIdentFromSupportsSQL = applyWinnersAuditSupportsSwaps(
		"staged winner fact ident", stagedWinnerFactIdentSQL, stagedWinnerFactIdentSwaps)
)

// winnersAuditRestrictedPendingSelectFromSupports is winnersAuditRestrictedPendingSelect with
// the fact arm reading the supports table.
func winnersAuditRestrictedPendingSelectFromSupports(table string) string {
	return applyWinnersAuditSupportsSwaps("audit pending select", winnersAuditRestrictedPendingSelect(table), winnersAuditPendingLateralSwaps)
}

// winnersScopeExpansionSelectFromSupports is winnersScopeExpansionSelect over the supports
// table, by the scope-identifier swaps the maintainer's expansion uses (the two statements
// carry the same row-source fragments).
func winnersScopeExpansionSelectFromSupports(table string) string {
	return applyWinnersSupportsInputSwaps("audit scope expansion", winnersScopeExpansionSelect(table), winnersScopeIdentsSwaps)
}
