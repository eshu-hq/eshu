// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The winners maintainer's full and keyed inputs read the supports table
// (#10880 PR-C1 d); the audit, which is the oracle, stays on fact_records. These
// tests pin that split hermetically. The equality of the two inputs on a seed is a
// live proof (T1) and is not run here.

// winnersLegacyContractPin is the version-2 winners contract hash at origin/main
// aa2ddc3aa6 (after the watermark-stall narrow-temp-table re-pin), before the
// maintainer's inputs moved to supports.
const winnersLegacyContractPin = "25e4d8910be1951ac76980f97de64e5d5f9b483e9151a459236dc0761175747a"

// winnersKeyedLegacyReversedPin pins the hash of the keyed contract
// statements with every reversal applied (supports swaps, stream layer,
// detoast, EPSS strip), after the ruled #9805 v3-full re-pin moved the full
// rebuild to chunked statements. It proves the keyed inputs still reduce to
// the legacy statements byte-for-byte: these members are untouched by the
// v3-full edit, and the completed-reversal assertions above prove no layer
// stopped halfway.
// Re-pinned for the ruled #11691 shape-(b) fix: the legacy base itself is
// lenient now (missing key and JSON-null read as ” in the triple legs of
// the payload probe, plain lenient equalities in the plain-column twin), so
// the reversed statements differ from the previous pin exactly by those legs.
// The reversal still proves the shipped text differs from the legacy base
// only by the declared transforms; behavior proof rides the live agreement
// and outrank legs (Refs #11691). The lane-A TOP-1 statements are skipped
// before the reversal (new statements, proven live instead): the batch
// probe, the narrow ANALYZE, the live-gens build, the active, counts, and
// operator builds, and the TOP-1 apply.
const winnersKeyedLegacyReversedPin = "45de5ca59a2f59150b60ff444656f145cd0fdfe5239a509841e87feab42734de"

// TestWinnersMaintainerStatementsReadNoFactRecords proves every statement
// the winners contract names reads the supports table and not fact_records:
// after the PR-C write stop the finding facts are no longer written, so a
// maintainer statement that still read them would read stale rows. The ruled
// #9805 chunked full is covered, not exempt: round 5 re-sourced every chunk
// statement to supports (TestWinnersFullChunkedReadsPayloadDerivedRanking
// pins the row source per statement), so the exemption the v3-full tree
// carried is gone.
func TestWinnersMaintainerStatementsReadNoFactRecords(t *testing.T) {
	t.Parallel()

	for i, stmt := range supplyChainImpactWinnersContractStatements() {
		if strings.Contains(stmt, "fact_records") {
			t.Errorf("winners contract statement %d still names fact_records:\n%s", i, stmt)
		}
		if strings.Contains(stmt, "reducer_supply_chain_impact_finding") {
			t.Errorf("winners contract statement %d still names the finding fact kind", i)
		}
	}
	for name, stmt := range map[string]string{
		"scope idents":              buildWinnersKeyedScopeIdentsFromSupportsSQL,
		"keyed active":              buildWinnersKeyedActiveFactsNarrowFromSupportsSQL,
		"keyed operator":            addWinnersKeyedOperatorFactsNarrowFromSupportsSQL,
		"keyed narrow":              buildWinnersKeyedNarrowFromSupportsSQL,
		"keyed narrow key-presence": buildWinnersKeyedNarrowKeyPresenceSQL,
	} {
		if !strings.Contains(stmt, "supply_chain_impact_finding_supports AS ") {
			t.Errorf("%s statement does not read the supports table", name)
		}
	}
}

// TestWinnersAuditReadsSupports proves the audit and the staged audit scan read the
// supports table (#10880 PR-C2 l): the write stop freezes the finding facts, so an audit
// still on them would compare today's winners with a stale oracle. After this change the
// audit compares the winners with the supports only; the D3a sweep before the stop is the
// last independent oracle. The legacy constants stay as the base the swaps derive from.
func TestWinnersAuditReadsSupports(t *testing.T) {
	t.Parallel()

	for name, stmt := range map[string]string{
		"audit candidates":           selectWinnersAuditCandidatesSQL,
		"audit pending":              createWinnersAuditPendingSQL,
		"audit range pending":        createWinnersAuditRangePendingSQL,
		"audit range stale pending":  createWinnersAuditRangeStalePendingSQL,
		"keyed active":               buildWinnersKeyedActiveFactsNarrowFromSupportsSQL,
		"keyed operator":             addWinnersKeyedOperatorFactsNarrowFromSupportsSQL,
		"scope expansion":            winnersScopeExpansionSelectFromSupports(winnersStagingTable),
		"audit first range":          selectWinnersAuditFirstRangeFromSupportsSQL,
		"audit next range":           selectWinnersAuditNextRangeFromSupportsSQL,
		"audit null probe":           probeWinnersAuditNullKeysFromSupportsSQL,
		"staged audit scan source":   stagedWinnersSourceBatchSQL,
		"staged audit scan active":   stagedWinnersBatchActiveFactsFromSupportsSQL,
		"staged audit scan operator": stagedWinnersOperatorBatchSQL,
		"staged audit ident fetch":   stagedWinnerFactIdentFromSupportsSQL,
	} {
		if strings.Contains(stmt, "fact_records") {
			t.Errorf("%s statement still names fact_records: the audit must read the supports table", name)
		}
		if strings.Contains(stmt, "'reducer_supply_chain_impact_finding'") {
			t.Errorf("%s statement still filters on the finding fact kind: a support is implicitly a live finding", name)
		}
		if !strings.Contains(stmt, "supply_chain_impact_finding_supports") {
			t.Errorf("%s statement does not read the supports table", name)
		}
	}
	// The legacy constants are the base the swaps derive from and stay on facts.
	for name, stmt := range map[string]string{
		"legacy keyed active":   buildWinnersKeyedActiveFactsSQL,
		"legacy winner select":  supplyChainImpactWinnerSelectSQL,
		"legacy staged active":  stagedWinnersBatchActiveFactsSQL,
		"legacy audit range":    selectWinnersAuditFirstRangeSQL,
		"legacy audit nullprob": probeWinnersAuditNullKeysSQL,
	} {
		if !strings.Contains(stmt, "fact_records") {
			t.Errorf("%s no longer reads fact_records: the derivation base moved", name)
		}
	}
}

// TestWinnersSupportsInputsDeriveFromTheLegacyStatements proves each supports
// statement is its legacy statement with exactly the swaps declared in
// winnersSupportsInputSwaps: reversing them gives back the legacy text byte for
// byte, so nothing else in the select, the keyed probe or the operator insert
// changed.
func TestWinnersSupportsInputsDeriveFromTheLegacyStatements(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct{ derived, legacy string }{
		"full active facts":       {supplyChainImpactWinnerActiveFactsFromSupportsSQL, supplyChainImpactWinnerActiveFactsSQL},
		"full active facts inner": {supplyChainImpactWinnerActiveFactsInnerFromSupportsSQL, supplyChainImpactWinnerActiveFactsInnerSQL},
		"full select":             {supplyChainImpactWinnerSelectFromSupportsSQL, supplyChainImpactWinnerSelectSQL},
		"scope idents":            {buildWinnersKeyedScopeIdentsFromSupportsSQL, buildWinnersKeyedScopeIdentsSQL},
		"keyed active":            {buildWinnersKeyedActiveFactsFromSupportsSQL, buildWinnersKeyedActiveFactsSQL},
		"keyed operator":          {addWinnersKeyedOperatorFactsFromSupportsSQL, addWinnersKeyedOperatorFactsSQL},
		"keyed narrow":            {buildWinnersKeyedNarrowFromSupportsSQL, buildWinnersKeyedNarrowSQL},
	} {
		if c.derived == c.legacy {
			t.Errorf("%s: the supports statement equals the legacy one", name)
		}
		if got := reverseWinnersSupportsInput(c.derived); got != c.legacy {
			t.Errorf("%s: reversing the swaps does not give the legacy statement back: something else changed", name)
		}
	}
}

// TestWinnersSupportsInputReadsOnlyColumnsTheSupportsTableHas proves the winner
// body reads from its active_facts alias only columns the supports table carries
// (migration 338), so the swap cannot leave a column reference dangling.
func TestWinnersSupportsInputReadsOnlyColumnsTheSupportsTableHas(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("migrations/338_supply_chain_impact_finding_supports.sql")
	if err != nil {
		t.Fatalf("read migration 338: %v", err)
	}
	_, table, _ := strings.Cut(string(raw), "CREATE TABLE IF NOT EXISTS supply_chain_impact_finding_supports (")
	table, _, _ = strings.Cut(table, "\n);")
	has := map[string]bool{}
	for _, line := range strings.Split(table, "\n") {
		if fields := strings.Fields(line); len(fields) > 1 && !strings.HasPrefix(fields[0], "--") {
			has[fields[0]] = true
		}
	}
	if !has["fact_id"] || !has["payload"] || len(has) < 20 {
		t.Fatalf("parsed supports columns = %v: the migration parse is broken", has)
	}
	used := regexp.MustCompile(`\bfact\.([a-z_]+)`)
	body := supplyChainImpactWinnerBodySQL
	for _, stmt := range []string{body, buildWinnersKeyedActiveFactsFromSupportsSQL, addWinnersKeyedOperatorFactsFromSupportsSQL, buildWinnersKeyedScopeIdentsFromSupportsSQL} {
		for _, m := range used.FindAllStringSubmatch(stmt, -1) {
			if m[1] != "*" && !has[m[1]] {
				t.Errorf("a maintainer statement reads fact.%s, which the supports table does not have", m[1])
			}
		}
	}
}

// reverseWinnersEPSSDateStrip inverts the #11598 top-level date strip,
// restoring the version-2 projection text. The strip is output-preserving
// on real payloads (the date lives nested in provenance, never top-level),
// so the re-pin stands without a contract version bump or a resweep; the
// nested-date behavior itself is pinned by the digest unit tests.
func reverseWinnersEPSSDateStrip(statement string) string {
	return strings.ReplaceAll(statement, " - 'selected_epss_score_date'", "")
}

// TestWinnersContractRepinnedForSupportsInputs is the differential that lets the
// version-2 pin be re-pinned instead of bumped: reversing the supports swaps in
// the keyed contract statements must give the legacy statements back, pinned
// below. The winner output for the same rows is unchanged by construction here
// (the supports payload is the fact payload verbatim); the live byte-identity
// proof on a seed is owed with the live proofs.
//
// Single-detoast (#11120) chains a second reversal: reverseWinnersDetoast
// restores the legacy `->>` reads. The field pairs the reversal inverts are
// covered live per field (TestSupplyChainImpactWinnersDetoastFieldEquivalenceLive), which is the
// byte-identity proof the re-pin rests on. The CTE-streaming body (#9805 C5 /
// #9975 C4) chains a third, innermost reversal first:
// reverseWinnersKeyedStreamLayer restores the wide supports builds and the
// shipped body. The #11598 date strip chains a fourth, outermost reversal
// first: reverseWinnersEPSSDateStrip restores the version-2 projection text.
//
// The ruled #9805 v3-full chunks are new statements, not swaps: they cannot
// reverse to a version-2 member, so the reversal skips them. Their own
// differential is the v3-full suite (stream reversal, select prefix, insert
// round-trip), chained into the version-3 re-pin in
// supplyChainImpactWinnersContractPins. The triple-guarded probe (#9975
// round 6) is likewise authored on the plain supports columns and skipped;
// its row agreement with the shipped twin is proven live by
// TestSupplyChainImpactWinnersNarrowTripleGuardProbeMatchesShippedLive instead.
func TestWinnersContractRepinnedForSupportsInputs(t *testing.T) {
	t.Parallel()

	shipped := supplyChainImpactWinnersContractStatements()
	fullChunk := map[string]bool{}
	for _, stmt := range winnersFullChunkContractStatements() {
		fullChunk[stmt] = true
	}
	var reversed []string
	var shippedKept []string
	for _, stmt := range shipped {
		if stmt == buildWinnersKeyedNarrowKeyPresenceSQL || fullChunk[stmt] {
			continue
		}
		// The lane-A TOP-1 statements are new statements, not swaps: the
		// marker-gated probe has no whole-chunk twin; the tick-invariant
		// live-gens build, the narrow ANALYZE, and the temp active, counts,
		// and operator builds exist only on the TOP-1 path. They cannot
		// reverse to a version-2 member (a reversal layer is the identity on
		// them, so hashing a reversed ANALYZE would pin a vacuous member),
		// so the reversal skips them exactly like the 363 probe and the
		// v3-full chunks; their row agreement with the shipped body is
		// proven live by the lane-A byte-parity differential instead
		// (TestSupplyChainImpactWinnersKeyedTop1MatchesRecomputeLive).
		if stmt == insertWinnersKeyedTop1NarrowBatchSQL ||
			stmt == analyzeWinnersKeyedTop1NarrowSQL ||
			stmt == buildWinnersKeyedTop1LiveGensSQL ||
			stmt == buildWinnersKeyedTop1ActiveSQL ||
			stmt == buildWinnersKeyedTop1CountsSQL ||
			stmt == buildWinnersKeyedTop1OpSQL ||
			stmt == applyWinnersKeyedTop1SQL {
			continue
		}
		shippedKept = append(shippedKept, stmt)
		reversed = append(reversed, reverseWinnersDetoast(reverseWinnersSupportsInput(reverseWinnersKeyedStreamLayer(reverseWinnersEPSSDateStrip(stmt)))))
	}
	// Every reversal ran to completion: no supports row source, no extracted
	// record, no streamed fetch and no date strip survives. A statement whose
	// shipped form read facts or supports reads its legacy row source again:
	// the facts, or the keyed temp table the streamed body ranks over.
	legacyRowSources := append([]string{"fact_records"},
		winnersKeyedActiveFactsTable, winnersKeyedNarrowTable, winnersKeyedIdentsTable)
	for i, stmt := range reversed {
		for _, banned := range []string{
			"supply_chain_impact_finding_supports",
			"jsonb_to_record",
			"source_payload",
			"override_payload",
			"selected_epss_score_date",
		} {
			if strings.Contains(stmt, banned) {
				t.Errorf("reversed keyed statement %d still carries %q: a reversal layer did not run", i, banned)
			}
		}
		if !strings.Contains(shippedKept[i], "fact_records") &&
			!strings.Contains(shippedKept[i], "supply_chain_impact_finding_supports") {
			continue
		}
		restored := false
		for _, source := range legacyRowSources {
			if strings.Contains(stmt, source) {
				restored = true
			}
		}
		if !restored {
			t.Errorf("reversed keyed statement %d lost its legacy row source", i)
		}
	}
	if got := supplyChainImpactWinnersContractHashOf(reversed); got != winnersKeyedLegacyReversedPin {
		t.Fatalf("the keyed contract statements with the supports swaps reversed hash to %s, want the pinned legacy reversal %s", got, winnersKeyedLegacyReversedPin)
	}
	if got := supplyChainImpactWinnersContractHashOf(shipped); got == winnersLegacyContractPin {
		t.Fatal("the shipped contract statements still hash to the previous pin: the maintainer did not move")
	}
}

// TestWinnersKeyedTickReadsNoFactRecords runs one keyed tick against the phase
// fake and proves no statement it executes names fact_records or the finding
// fact kind. The contract statements are pinned above; this pins the reads the
// contract deliberately leaves out, such as the #11308 scanned-copies count,
// which must probe the same table the build probes or the cost-per-copy it
// attributes is measured on a table the tick never built from.
func TestWinnersKeyedTickReadsNoFactRecords(t *testing.T) {
	t.Parallel()

	tx := &phaseFakeTx{claimed: true, consumed: 3, identifier: 3, stored: 100, scanned: 187}
	store, _ := newPhaseStore(t, tx)
	res, err := store.ResweepWinnersKeyed(context.Background())
	if err != nil {
		t.Fatalf("ResweepWinnersKeyed() error = %v", err)
	}
	if res.ChunkScannedCopies != 187 {
		t.Errorf("ChunkScannedCopies = %d, want 187: the count must still reach the tick result", res.ChunkScannedCopies)
	}
	for i, query := range tx.queries {
		if strings.Contains(query, "fact_records") {
			t.Errorf("keyed tick statement %d names fact_records:\n%s", i, query)
		}
		if strings.Contains(query, "reducer_supply_chain_impact_finding") {
			t.Errorf("keyed tick statement %d names the finding fact kind:\n%s", i, query)
		}
	}
}

// TestWinnersSupportsNarrowProbeMatchesTheSupportsBuild pins that the supports
// narrow probe probes what the supports keyed build probed before the
// watermark-stall split: the five index equalities read byte-identical in the
// narrow twin, and neither keeps the kind or tombstone filters the supports
// table makes implicit. The scanned-copies count needs no twin: it reads the
// persisted narrow table (TestWinnersScannedCopiesProbeMatchesActiveFactsProbe
// pins that shape on the legacy statements).
func TestWinnersSupportsNarrowProbeMatchesTheSupportsBuild(t *testing.T) {
	t.Parallel()

	for _, frag := range []string{
		"FROM supply_chain_impact_finding_supports AS f",
		"(f.payload->>'package_id' = ident.package_id OR (ident.package_id = '' AND (f.payload->>'package_id') IS NULL))",
		"(f.payload->>'repository_id' = ident.repository_id OR (ident.repository_id = '' AND (f.payload->>'repository_id') IS NULL))",
		"(f.payload->>'subject_digest' = ident.subject_digest OR (ident.subject_digest = '' AND (f.payload->>'subject_digest') IS NULL))",
		"COALESCE(NULLIF(f.payload->>'cve_id', ''), NULLIF(f.payload->>'advisory_id', ''), '') = ident.c0",
		"COALESCE(f.payload->>'advisory_id', '') = ident.advisory_id",
	} {
		if !strings.Contains(buildWinnersKeyedNarrowFromSupportsSQL, frag) {
			t.Errorf("supports narrow probe lost the probe fragment %q", frag)
		}
	}
	for _, gone := range []string{"f.fact_kind", "f.is_tombstone"} {
		if strings.Contains(buildWinnersKeyedNarrowFromSupportsSQL, gone) {
			t.Errorf("supports narrow probe still filters %s", gone)
		}
	}
}
