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
		// The lateral rides the ON clause in both row sources (the supports
		// table is aliased AS fact, so fact.payload resolves there too); only
		// the kind and tombstone filters drop out (#11120 keeps the lateral
		// verbatim on both sides).
		{" AND fact.generation_id = pending.generation_id" + winnersDetoastFactLateral + "\nWHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'\n  AND fact.is_tombstone = FALSE", " AND fact.generation_id = pending.generation_id" + winnersDetoastFactLateral},
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

// Single-detoast extraction (#11120 criterion 2). Every `payload->>'field'`
// detoasts the whole JSONB value again, so the winner body's ~25 text reads
// per candidate re-read the same payload ~25 times. These declarations extract
// every text field the body needs exactly once through one LATERAL
// jsonb_to_record per row; every downstream expression reads the record.
//
// The detoasted fragments are DERIVED from the legacy helpers, never copied:
// winnersDetoastFieldReplacer renders the payload helpers over fact.payload
// and swaps each field read for its record ref, so the canonical key, the
// finding-id fallback, and the ranking inputs cannot drift from the shipped
// definitions. The hermetic reverse (reverseWinnersDetoast, in the detoast
// test) inverts exactly this mapping, and the live field-equivalence proof
// covers every pair.
//
// Two deliberate exceptions stay on the payload, pinned by hermetic tests:
//   - cvss.BucketSQL keeps source.payload through the shared function, which
//     the read-time dedup also calls (splitting it forks the severity
//     contract).
//   - The three jsonb array projections keep `->`: jsonb_to_record maps a
//     JSON null to SQL NULL while `->` keeps jsonb null, so converting them
//     would store '[]' where the legacy stored null (proven live).
// The guarded lateral treats a non-object payload as all-NULLs, exactly like
// the legacy `->>` reads: jsonb_to_record errors on scalars, so the CASE keeps
// one bad row from aborting the tick.

// winnersDetoastFields are the nineteen text fields the winner body extracts
// per candidate: the identifier fields, the canonical-key arguments, the
// finding-id fallback, the ranking inputs, and the final-projection columns.
// Every declaration below derives from this list, so a field cannot be
// extracted without being declared, or declared without being extracted (the
// coverage test pins both directions).
var winnersDetoastFields = []string{
	"cve_id", "advisory_id", "package_id", "purl", "product_criteria",
	"match_criteria_id", "observed_version", "requested_range",
	"impact_status", "repository_id", "subject_digest", "finding_id",
	"priority_score", "suppression_state", "ecosystem", "image_ref",
	"priority_bucket", "detection_profile", "match_reason",
}

// winnersDetoastIdentFields are the five identifier fields the scope expansion
// extracts per finding: the subset of the record the identifier build reads.
var winnersDetoastIdentFields = []string{
	"package_id", "repository_id", "subject_digest", "cve_id", "advisory_id",
}

// winnersDetoastFinalFields are the twelve carried columns the final
// projection reads instead of re-extracting from source.payload.
var winnersDetoastFinalFields = []string{
	"impact_status", "ecosystem", "repository_id", "cve_id", "advisory_id",
	"package_id", "subject_digest", "image_ref", "priority_bucket",
	"detection_profile", "observed_version", "match_reason",
}

// winnersDetoastTextColumns is the record definition shared by every guarded
// lateral. Text columns keep the legacy `->>` coercion on every JSON shape
// (numbers, booleans, nulls), proven live field by field.
var winnersDetoastTextColumns = func() string {
	cols := make([]string, 0, len(winnersDetoastFields))
	for _, f := range winnersDetoastFields {
		cols = append(cols, f+" text")
	}
	return strings.Join(cols, ", ")
}()

// winnersDetoastFactLateral extracts the record once per active_facts row.
// The CASE guard maps a non-object payload to a NULL record, which is what
// the legacy `->>` reads returned for the same row.
var winnersDetoastFactLateral = `
    CROSS JOIN LATERAL jsonb_to_record(CASE WHEN jsonb_typeof(fact.payload) = 'object' THEN fact.payload END) AS d(` + winnersDetoastTextColumns + `)`

// winnersDetoastFieldReplacer swaps each fact.payload text-field read for its
// once-extracted record ref. The trailing quote on every old fragment keeps
// field names from prefix-colliding (purl cannot match a longer key).
var winnersDetoastFieldReplacer = func() *strings.Replacer {
	pairs := make([]string, 0, 2*len(winnersDetoastFields))
	for _, f := range winnersDetoastFields {
		pairs = append(pairs, "fact.payload->>'"+f+"'", "d."+f)
	}
	return strings.NewReplacer(pairs...)
}()

// winnersDetoastOperatorPredicate is the operator-suppression guard over the
// record, derived from the shared predicate so the two cannot drift. The
// staged scan keeps the shared const (it has no record); the body uses this.
var winnersDetoastOperatorPredicate = strings.Replace(
	supplyChainImpactOperatorSuppressionPredicate,
	"fact.payload->>'suppression_state'", "d.suppression_state", 1)

// winnersDetoastEligibilityFromActiveFacts is the source-candidate row source:
// the shared eligibility tail with the guarded lateral spliced between the
// FROM and the LEFT JOINs. It panics at initialization unless the splice point
// matches exactly once, so a change to the shared tail cannot silently drop
// the lateral (the staged scan reuses the same tail without it).
var winnersDetoastEligibilityFromActiveFacts = func() string {
	const anchor = "FROM active_facts AS fact\n"
	if n := strings.Count(supplyChainImpactSourceCandidateEligibilitySQL, anchor); n != 1 {
		panic(fmt.Sprintf("winners detoast eligibility splice anchor occurs %d times, want exactly once", n))
	}
	return strings.Replace(supplyChainImpactSourceCandidateEligibilitySQL, anchor,
		"FROM active_facts AS fact"+winnersDetoastFactLateral+"\n", 1)
}()

// winnersDetoastCarriedSelect renders the final-projection columns the
// candidates carry: each pre-wrapped exactly as the legacy final SELECT
// wrapped it, so the final SELECT reads bare source columns.
func winnersDetoastCarriedSelect() string {
	items := make([]string, 0, len(winnersDetoastFinalFields))
	for _, f := range winnersDetoastFinalFields {
		items = append(items, "COALESCE(d."+f+", '') AS "+f)
	}
	return ",\n        " + strings.Join(items, ",\n        ")
}
