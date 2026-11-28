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
	// supplyChainImpactWinnerActiveFactsInnerFromSupportsSQL is the inner
	// candidate set of the full select over the supports table: the same
	// swaps narrow the inner select the chunked-full enumeration pages, so
	// the full rebuild's live arm reads the supports row source the keyed
	// tick and the audit read. The table stays aliased AS fact, so the
	// detoast lateral keeps resolving fact.payload verbatim.
	supplyChainImpactWinnerActiveFactsInnerFromSupportsSQL = applyWinnersSupportsInputSwaps(
		"full active facts inner", supplyChainImpactWinnerActiveFactsInnerSQL, winnersActiveFactsSwaps)
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
	// buildWinnersKeyedActiveFactsNarrowFromSupportsSQL is the stage-1 narrow
	// temp build the keyed tick runs (#9805 C5 / #9975 C4): the copy identity
	// plus the once-extracted detoast fields, without the payload. Narrowing
	// the supports twin, never the legacy text, keeps the legacy statement
	// the untouched derivation source the swaps and the hermetic pins read.
	buildWinnersKeyedActiveFactsNarrowFromSupportsSQL = narrowWinnersKeyedTempBuild(
		"keyed active facts", buildWinnersKeyedActiveFactsFromSupportsSQL, "")
	// addWinnersKeyedOperatorFactsNarrowFromSupportsSQL is the narrow twin of
	// the operator insert: same rows, same filters, identity plus extracted
	// fields, no payload. The lateral splices before the outer WHERE (the
	// build above appends it at the end, where its FROM/JOINs already end).
	addWinnersKeyedOperatorFactsNarrowFromSupportsSQL = narrowWinnersKeyedTempBuild(
		"keyed operator facts", addWinnersKeyedOperatorFactsFromSupportsSQL,
		"\nWHERE fact.scope_id = '"+supplyChainImpactOperatorScopeID+"'")
	// winnersKeyedStreamBodySQL is the CTE-streaming keyed winner body: the
	// shipped body with extraction moved to the stage-1 temp and the payload
	// fetched by fact_id in the final select, only for the emitted winner
	// and override rows. It is derived from the shipped body, never copied,
	// and reverseWinnersKeyedStreamBody (in the stream test) inverts the
	// derivation byte-for-byte, so the two differ only by the declared
	// transform.
	winnersKeyedStreamBodySQL = streamWinnersKeyedBody(supplyChainImpactWinnerBodySQL)
)

// winnersKeyedSelectSQL is the shipped winner select over the keyed
// active_facts temp table: the narrow stage-1 rows streamed through the
// keyed body, which fetches the payload by fact_id for the emitted rows.
// The full path keeps the shared body over its wide active_facts; the audit
// recomputes through this same select, so the tick and its verifier cannot
// split.
var winnersKeyedSelectSQL = `WITH active_facts AS (SELECT * FROM ` + winnersKeyedActiveFactsTable + `)` +
	winnersKeyedStreamBodySQL

// winnersKeyedNarrowTempSelectList renders the stage-1 temp select list: the
// copy identity plus source_confidence plus one record ref per detoasted
// field, in winnersDetoastFields order. The temp column names are the field
// names, so the streamed body reads fact.<field> where the shipped body read
// the lateral record.
func winnersKeyedNarrowTempSelectList() string {
	cols := []string{"fact.fact_id", "fact.scope_id", "fact.generation_id", "fact.source_confidence"}
	for _, f := range winnersDetoastFields {
		cols = append(cols, "d."+f)
	}
	return "SELECT " + strings.Join(cols, ",\n    ")
}

// narrowWinnersKeyedTempBuild derives a stage-1 narrow temp build from a
// wide supports build: the select list becomes the copy identity plus the
// once-extracted record refs, and the guarded lateral moves into the FROM
// clause so extraction runs once per kept row. spliceBefore splices the
// lateral before the named outer clause; empty appends it at the end, where
// the active-facts build's FROM/JOINs already end. Every anchor must match
// exactly once, so a change to the wide statement fails loudly instead of
// running half-narrowed.
func narrowWinnersKeyedTempBuild(name, wideSupports, spliceBefore string) string {
	const oldSelect = "SELECT fact.*"
	if n := strings.Count(wideSupports, oldSelect); n != 1 {
		panic("winners narrow temp " + name + ": select list occurs " + fmt.Sprintf("%d", n) + " times, want 1")
	}
	out := strings.Replace(wideSupports, oldSelect, winnersKeyedNarrowTempSelectList(), 1)
	if spliceBefore == "" {
		return out + winnersDetoastFactLateral
	}
	if n := strings.Count(out, spliceBefore); n != 1 {
		panic("winners narrow temp " + name + ": splice anchor occurs " + fmt.Sprintf("%d", n) + " times, want 1")
	}
	return strings.Replace(out, spliceBefore, winnersDetoastFactLateral+spliceBefore, 1)
}

// streamWinnersKeyedBody derives the CTE-streaming keyed body from the
// shipped winner body:
//   - the two guarded laterals drop out (stage 1 already extracted);
//   - every d.<field> becomes fact.<field> (the narrow temp columns);
//   - the two fact.payload passthroughs drop out (the temp has no payload);
//   - every source.payload becomes source_payload.payload and every
//     override.payload becomes override_payload.payload, fetched by fact_id
//     in the final select, only for the emitted rows.
//
// The deliberate detoast exceptions ride along unchanged, still on a payload
// expression: cvss.BucketSQL keeps reading a payload (now the fetched row's,
// the same row), and the three `->` array projections keep `->`, preserving
// the jsonb-null semantics the live differential pins. live_gens, the
// scanned-copies probe, the chunk bound, the statement guard, and the index
// set are untouched: this moves when payload bytes are read, never which
// rows flow. Every anchor must match its exact count, so a change to the
// shipped body fails loudly instead of running half-streamed.
func streamWinnersKeyedBody(body string) string {
	return streamWinnersKeyedBodyFrom(body, winnersSupportsTable)
}

// streamWinnersKeyedBodyFrom derives the CTE-streaming keyed body from the
// shipped winner body with the payload fetched from payloadTable. The keyed
// tick and the ruled #9805 chunked full both stream over the supports table,
// so the winner and override payloads arrive by fact_id point lookup from
// the supports while the shipped final projection runs verbatim. Every
// anchor count is independent of the table name, so two derivations over
// different payload tables differ only by the declared row source.
func streamWinnersKeyedBodyFrom(body, payloadTable string) string {
	if n := strings.Count(body, winnersDetoastFactLateral); n != 2 {
		panic("winners stream body: lateral occurs " + fmt.Sprintf("%d", n) + " times, want 2")
	}
	out := strings.ReplaceAll(body, winnersDetoastFactLateral, "")
	for _, f := range winnersDetoastFields {
		if n := strings.Count(out, "d."+f); n < 1 {
			panic("winners stream body: record ref d." + f + " is absent")
		}
		out = strings.ReplaceAll(out, "d."+f, "fact."+f)
	}
	if strings.Contains(out, "d.") {
		panic("winners stream body: a d. ref survived the record-to-temp rewrite")
	}
	const payloadPassthrough = "        fact.payload,\n"
	if n := strings.Count(out, payloadPassthrough); n != 2 {
		panic("winners stream body: payload passthrough occurs " + fmt.Sprintf("%d", n) + " times, want 2")
	}
	out = strings.ReplaceAll(out, payloadPassthrough, "")
	// Rendered refs: cvss.BucketSQL fans the alias out to 5 (four score
	// thresholds plus the label arm), the three array projections add 3,
	// and the final payload merge adds 2.
	const sourcePayloadRefs = 10
	if n := strings.Count(out, "source.payload"); n != sourcePayloadRefs {
		panic("winners stream body: source.payload occurs " + fmt.Sprintf("%d", n) + " times, want " + fmt.Sprintf("%d", sourcePayloadRefs))
	}
	out = strings.ReplaceAll(out, "source.payload", "source_payload.payload")
	// Rendered refs: three expiry arms plus the two suppression-fragment
	// reads on the final payload line.
	const overridePayloadRefs = 5
	if n := strings.Count(out, "override.payload"); n != overridePayloadRefs {
		panic("winners stream body: override.payload occurs " + fmt.Sprintf("%d", n) + " times, want " + fmt.Sprintf("%d", overridePayloadRefs))
	}
	out = strings.ReplaceAll(out, "override.payload", "override_payload.payload")
	const streamFromAnchor = `FROM source_winners AS source
LEFT JOIN operator_overrides AS override
  ON override.canonical_key = source.canonical_key`
	if n := strings.Count(out, streamFromAnchor); n != 1 {
		panic("winners stream body: final FROM occurs " + fmt.Sprintf("%d", n) + " times, want 1")
	}
	return strings.Replace(out, streamFromAnchor,
		`FROM source_winners AS source
JOIN `+payloadTable+` AS source_payload ON source_payload.fact_id = source.fact_id
LEFT JOIN operator_overrides AS override
  ON override.canonical_key = source.canonical_key
LEFT JOIN `+payloadTable+` AS override_payload ON override_payload.fact_id = override.fact_id`, 1)
}

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
