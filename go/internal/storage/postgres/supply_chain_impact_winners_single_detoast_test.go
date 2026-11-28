// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core/cvss"
)

// Single-detoast extraction (#11120 criterion 2). The keyed tick re-reads the
// same finding payload once per extracted field: every `->>` detoasts the
// value again. The winner body (source_candidates, operator_candidates and the
// final projection over them) and the scope-identifier expansion now extract
// every text field once through one LATERAL jsonb_to_record per row, and the
// final projection reads the carried columns instead of re-extracting.
//
// The differential proof is composed, not single-sited:
//   - Each (legacy, detoasted) field pair differs only by extraction
//     mechanism. TestSupplyChainImpactWinnersDetoastFieldEquivalenceLive (live, PG18) proves
//     every pair equivalent on edge-case payloads: missing keys, JSON nulls,
//     empty strings, numbers (including 5.0), booleans, padded and unicode
//     strings, and non-object payloads via the CASE guard.
//   - These hermetic tests prove the shipped statements differ from the legacy
//     text ONLY by those pairs: every legacy fragment is absent, every
//     detoasted fragment is present, and reverseWinnersDetoast (built from the
//     shipped constants, never hand-copied) restores the legacy text.
//     Together the two halves show byte-identical winners.
//
// Deliberate non-conversions, pinned here so a drive-by "finish the job"
// cannot regress them:
//   - winnersKeyedNarrowProbeSelect keeps its five `->>` equalities: they are
//     the 291 keyed index expressions, and a record Var cannot match them
//     (EXPLAIN in #11120 (retired evidence record)).
//   - cvss.BucketSQL keeps reading source.payload: it is shared with the
//     read-time dedup (byte-parity rule in
//     supply_chain_impact_canonical_winners_store.go), and splitting it would
//     fork the severity contract.
//   - The three jsonb array projections (service_ids, workload_ids,
//     environments) keep `->`: jsonb_to_record maps a JSON null to SQL NULL
//     while `->` keeps jsonb null, so converting them changes stored winners
//     (proven live).

// reverseWinnersDetoast restores the legacy `->>` text from a detoasted
// statement. Every replacement is derived from the shipped constants (the
// lateral block, the carried block, the field lists), never hand-copied, so a
// drift between the statement and its reversal fails loudly instead of going
// false-green. TestWinnersDetoastReversesToLegacy pins the round trip;
// TestWinnersContractRepinnedForSupportsInputs chains it with the supports
// reversal back to the pre-supports pin.
func reverseWinnersDetoast(stmt string) string {
	out := stmt
	out = strings.ReplaceAll(out, winnersDetoastEligibilityFromActiveFacts, supplyChainImpactSourceCandidateEligibilitySQL)
	out = strings.ReplaceAll(out, winnersDetoastFactLateral, "")
	out = strings.ReplaceAll(out, winnersDetoastCarriedSelect(), "")
	out = strings.ReplaceAll(out, winnersDetoastOperatorPredicate, supplyChainImpactOperatorSuppressionPredicate)
	for _, f := range winnersDetoastFields {
		out = strings.ReplaceAll(out, "d."+f, "fact.payload->>'"+f+"'")
	}
	for _, f := range winnersDetoastFinalFields {
		out = strings.ReplaceAll(out, "source."+f+" AS "+f, "COALESCE(source.payload->>'"+f+"', '') AS "+f)
	}
	return out
}

// TestWinnersScopeIdentsDetoastOnce proves the identifier expansion extracts
// each field once: the five legacy `->>` refs are gone, the record refs are
// present, and exactly one guarded lateral feeds them.
func TestWinnersScopeIdentsDetoastOnce(t *testing.T) {
	t.Parallel()

	for name, stmt := range map[string]string{
		"keyed scope idents": buildWinnersKeyedScopeIdentsSQL,
		"scope expansion":    winnersScopeExpansionSelect("winners_scope_probe"),
	} {
		for _, f := range winnersDetoastIdentFields {
			if strings.Contains(stmt, "fact.payload->>'"+f+"'") {
				t.Errorf("%s still extracts %s per field", name, f)
			}
			if !strings.Contains(stmt, "d."+f) {
				t.Errorf("%s does not read the once-extracted d.%s", name, f)
			}
		}
		if n := strings.Count(stmt, "jsonb_to_record"); n != 1 {
			t.Errorf("%s calls jsonb_to_record %d times, want exactly 1 (all fields once)", name, n)
		}
		if !strings.Contains(stmt, "jsonb_typeof(fact.payload) = 'object'") {
			t.Errorf("%s lost the non-object guard: a scalar payload must read NULLs, not abort the tick", name)
		}
	}
}

// TestWinnerSourceCandidatesDetoastOnce proves source_candidates and
// operator_candidates extract every text field once through one lateral each,
// with no legacy text-field read left.
func TestWinnerSourceCandidatesDetoastOnce(t *testing.T) {
	t.Parallel()

	if n := strings.Count(supplyChainImpactWinnerBodySQL, "jsonb_to_record"); n != 2 {
		t.Fatalf("winner body calls jsonb_to_record %d times, want 2 (source and operator candidates, one lateral each)", n)
	}
	for _, f := range winnersDetoastFields {
		if strings.Contains(supplyChainImpactWinnerBodySQL, "fact.payload->>'"+f+"'") {
			t.Errorf("winner body still extracts %s per field", f)
		}
		if !strings.Contains(supplyChainImpactWinnerBodySQL, "d."+f) {
			t.Errorf("winner body does not read the once-extracted d.%s", f)
		}
	}
}

// TestWinnerFinalProjectionReadsCarriedColumns proves the final SELECT over
// the winners reads the columns source_candidates carried instead of
// re-extracting them from source.payload.
func TestWinnerFinalProjectionReadsCarriedColumns(t *testing.T) {
	t.Parallel()

	for _, f := range winnersDetoastFinalFields {
		if strings.Contains(supplyChainImpactWinnerBodySQL, "source.payload->>'"+f+"'") {
			t.Errorf("final projection re-extracts %s from source.payload instead of reading the carried column", f)
		}
		if !strings.Contains(supplyChainImpactWinnerBodySQL, "source."+f) {
			t.Errorf("final projection does not read the carried source.%s column", f)
		}
	}
}

// TestWinnerFinalProjectionKeepsCarriedDefaults pins the carried-column
// COALESCE defaults in the shipped final projection: stripping a default
// would store NULL where the legacy stored ”, so every carried column must
// render COALESCE(d.<field>, ”) AS <field>. A stripped mutant renders bare
// d.<field> AS <field> and fails this test.
func TestWinnerFinalProjectionKeepsCarriedDefaults(t *testing.T) {
	t.Parallel()

	for _, f := range winnersDetoastFinalFields {
		want := "COALESCE(d." + f + ", '') AS " + f
		if !strings.Contains(supplyChainImpactWinnerBodySQL, want) {
			t.Errorf("final projection does not pin carried default %s", want)
		}
	}
}

// TestWinnersDetoastRecordCoversBodyFields proves the record definition
// carries every text field the body reads, each exactly once, so no read can
// fall back to a per-field detoast.
func TestWinnersDetoastRecordCoversBodyFields(t *testing.T) {
	t.Parallel()

	seen := map[string]int{}
	for _, col := range strings.Split(winnersDetoastTextColumns, ",") {
		parts := strings.Fields(col)
		if len(parts) != 2 || parts[1] != "text" {
			t.Fatalf("record column %q is not a '<name> text' definition", col)
		}
		seen[parts[0]]++
	}
	for _, f := range winnersDetoastFields {
		if seen[f] != 1 {
			t.Errorf("record column %s declared %d times, want exactly once", f, seen[f])
		}
	}
	if len(seen) != len(winnersDetoastFields) {
		t.Errorf("record declares %d columns, want the %d body text fields (no more, no fewer)", len(seen), len(winnersDetoastFields))
	}
}

// TestWinnersDetoastReversesToLegacy is the text-level half of the derived
// differential: reversing the detoasted statements restores every legacy
// fragment and leaves no detoasted ref behind, so the shipped text differs
// from the legacy text only by the field pairs the live proof covers.
func TestWinnersDetoastReversesToLegacy(t *testing.T) {
	t.Parallel()

	// scopeStmts extract only the five identifier fields; bodyStmts extract the
	// full nineteen plus the twelve carried final-projection columns.
	scopeStmts := map[string]string{
		"keyed scope idents": buildWinnersKeyedScopeIdentsSQL,
		"scope expansion":    winnersScopeExpansionSelect("winners_scope_probe"),
	}
	for name, stmt := range scopeStmts {
		restored := reverseWinnersDetoast(reverseWinnersSupportsInput(stmt))
		for _, f := range winnersDetoastIdentFields {
			if !strings.Contains(restored, "fact.payload->>'"+f+"'") {
				t.Errorf("%s: reversed text does not restore fact.payload->>'%s'", name, f)
			}
		}
	}
	bodyStmts := map[string]string{
		"winner body": supplyChainImpactWinnerBodySQL,
		"full select": supplyChainImpactWinnerSelectSQL,
	}
	// The keyed select streams the derived body over the narrow temp: its
	// equivalence to the shipped body is the stream derivation's own
	// round trip (TestWinnersKeyedStreamBodyReversesToShipped), chained
	// here so this test still covers the keyed select end to end.
	if got := reverseWinnersDetoast(reverseWinnersSupportsInput(
		reverseWinnersKeyedStreamBody(winnersKeyedStreamBodySQL))); got != reverseWinnersDetoast(reverseWinnersSupportsInput(supplyChainImpactWinnerBodySQL)) {
		t.Error("keyed select: stream reversal chained with the detoast reversal diverges from the shipped body chain")
	}
	for name, stmt := range bodyStmts {
		restored := reverseWinnersDetoast(reverseWinnersSupportsInput(stmt))
		for _, f := range winnersDetoastFields {
			if strings.Contains(restored, "d."+f) {
				t.Errorf("%s: reverse mapping leaves detoasted ref d.%s behind", name, f)
			}
		}
		for _, legacy := range []string{
			"fact.payload->>'package_id'", "fact.payload->>'cve_id'",
			"fact.payload->>'suppression_state'", "fact.payload->>'finding_id'",
			"fact.payload->>'purl'",
			"COALESCE(source.payload->>'match_reason', '') AS match_reason",
			"COALESCE(source.payload->>'ecosystem', '') AS ecosystem",
		} {
			if !strings.Contains(restored, legacy) {
				t.Errorf("%s: reversed text does not restore %s", name, legacy)
			}
		}
	}
	for _, f := range winnersDetoastFinalFields {
		if strings.Contains(reverseWinnersDetoast(supplyChainImpactWinnerBodySQL), "source."+f+" AS "+f) {
			t.Errorf("reverse mapping leaves carried ref source.%s behind", f)
		}
	}
}

// TestWinnersDetoastMappingInverts proves the field mapping is a bijection:
// no detoasted ref is a substring of another, so the reversal cannot corrupt
// one field while restoring another.
func TestWinnersDetoastMappingInverts(t *testing.T) {
	t.Parallel()

	refs := make([]string, 0, len(winnersDetoastFields)+len(winnersDetoastFinalFields))
	for _, f := range winnersDetoastFields {
		refs = append(refs, "d."+f)
	}
	for _, f := range winnersDetoastFinalFields {
		refs = append(refs, "source."+f+" AS "+f)
	}
	for i, a := range refs {
		for j, b := range refs {
			if i != j && strings.Contains(a, b) {
				t.Errorf("detoasted ref %q contains %q: the mapping is not invertible", a, b)
			}
		}
	}
}

// TestWinnersKeyedProbeKeepsIndexExpressions pins the deliberate probe
// non-conversion: the five WHERE equalities must stay byte-identical `->>`
// over f.payload, because they are the 291 keyed index expressions. A record
// Var cannot match an expression index, so converting the probe trades five
// detoasts for a sequential scan (EXPLAIN in
// #11120 (retired evidence record)).
func TestWinnersKeyedProbeKeepsIndexExpressions(t *testing.T) {
	t.Parallel()

	probe := winnersKeyedNarrowProbeSelect(winnersKeyedIdentsTable)
	for _, want := range []string{
		"(f.payload->>'package_id') = ident.package_id",
		"(f.payload->>'repository_id') = ident.repository_id",
		"(f.payload->>'subject_digest') = ident.subject_digest",
		"COALESCE(NULLIF(f.payload->>'cve_id', ''), NULLIF(f.payload->>'advisory_id', ''), '') = ident.c0",
		"COALESCE(f.payload->>'advisory_id', '') = ident.advisory_id",
	} {
		if !strings.Contains(probe, want) {
			t.Errorf("keyed probe lost the index expression %q: the 291 index no longer serves it", want)
		}
	}
	if strings.Contains(probe, "jsonb_to_record") {
		t.Error("keyed probe uses jsonb_to_record: the record Var cannot match the 291 expression index")
	}
}

// TestWinnersDetoastKeepsSharedBucketOnPayload pins the BucketSQL exception:
// the severity expression must keep reading source.payload through the shared
// function, which the read-time dedup also calls.
func TestWinnersDetoastKeepsSharedBucketOnPayload(t *testing.T) {
	t.Parallel()

	if bucket := cvss.BucketSQL("source.payload"); !strings.Contains(supplyChainImpactWinnerBodySQL, bucket) {
		t.Error("winner body no longer renders the shared severity expression over source.payload")
	}
}

// TestWinnersDetoastKeepsJsonbArraysOnPayload pins the array exception: the
// three jsonb projections must keep `->`, because jsonb_to_record maps a JSON
// null to SQL NULL while `->` keeps jsonb null, and COALESCE would then store
// '[]' instead of null (proven live).
func TestWinnersDetoastKeepsJsonbArraysOnPayload(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"COALESCE(source.payload->'service_ids', '[]'::jsonb) AS service_ids",
		"COALESCE(source.payload->'workload_ids', '[]'::jsonb) AS workload_ids",
		"COALESCE(source.payload->'environments', '[]'::jsonb) AS environments",
	} {
		if !strings.Contains(supplyChainImpactWinnerBodySQL, want) {
			t.Errorf("winner body lost the array projection %q", want)
		}
	}
}

// TestWinnersDetoastOperatorPredicateDerivesFromShared pins that the body's
// record guard is the shared operator predicate with only the extraction
// swapped, so the two cannot drift.
func TestWinnersDetoastOperatorPredicateDerivesFromShared(t *testing.T) {
	t.Parallel()

	if !strings.Contains(supplyChainImpactWinnerBodySQL, winnersDetoastOperatorPredicate) {
		t.Error("winner body does not use the derived operator predicate")
	}
	if winnersDetoastOperatorPredicate == supplyChainImpactOperatorSuppressionPredicate {
		t.Error("derived operator predicate equals the shared one: the extraction was not swapped")
	}
	want := strings.Replace(supplyChainImpactOperatorSuppressionPredicate,
		"fact.payload->>'suppression_state'", "d.suppression_state", 1)
	if winnersDetoastOperatorPredicate != want {
		t.Errorf("derived operator predicate = %q, want the shared predicate with only the extraction swapped", winnersDetoastOperatorPredicate)
	}
}
