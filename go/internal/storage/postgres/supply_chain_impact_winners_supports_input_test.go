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
// f1f45ddad3, before the maintainer's inputs moved to supports.
const winnersLegacyContractPin = "8e761819b7b0f43d77ea3d46d8fd7395f5308ad945e542bc3b5c7ce3677d292e"

// TestWinnersMaintainerStatementsReadNoFactRecords proves every statement the
// winners contract names, the full rebuild and every keyed build and apply
// statement, reads the supports table and not fact_records: after the PR-C write
// stop the finding facts are no longer written, so a maintainer statement that
// still read them would read stale rows.
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
		"rebuild":        rebuildSupplyChainImpactWinnersSQL,
		"scope idents":   buildWinnersKeyedScopeIdentsFromSupportsSQL,
		"keyed active":   buildWinnersKeyedActiveFactsFromSupportsSQL,
		"keyed operator": addWinnersKeyedOperatorFactsFromSupportsSQL,
		"keyed narrow":   buildWinnersKeyedNarrowFromSupportsSQL,
	} {
		if !strings.Contains(stmt, "supply_chain_impact_finding_supports AS ") {
			t.Errorf("%s statement does not read the supports table", name)
		}
	}
}

// TestWinnersAuditStaysOnFacts proves the audit and the staged audit scan keep
// reading the finding facts in C1: they are the oracle the supports-derived
// winners are compared with, so moving them would compare supports with supports.
func TestWinnersAuditStaysOnFacts(t *testing.T) {
	t.Parallel()

	for name, stmt := range map[string]string{
		"audit candidates":           selectWinnersAuditCandidatesSQL,
		"legacy keyed active":        buildWinnersKeyedActiveFactsSQL,
		"legacy keyed operator":      addWinnersKeyedOperatorFactsSQL,
		"legacy winner select":       supplyChainImpactWinnerSelectSQL,
		"scope expansion":            winnersScopeExpansionSelect(winnersStagingTable),
		"staged audit scan source":   stagedWinnersSourceBatchSQL,
		"staged audit scan active":   stagedWinnersBatchActiveFactsSQL,
		"staged audit scan operator": stagedWinnersOperatorBatchSQL,
		"staged audit ident fetch":   stagedWinnerFactIdentSQL,
	} {
		if !strings.Contains(stmt, "fact_records") {
			t.Errorf("%s statement no longer reads fact_records: the audit must stay on facts in C1", name)
		}
		if strings.Contains(stmt, "supply_chain_impact_finding_supports") {
			t.Errorf("%s statement reads the supports table: the audit must stay on facts in C1", name)
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
		"full active facts": {supplyChainImpactWinnerActiveFactsFromSupportsSQL, supplyChainImpactWinnerActiveFactsSQL},
		"full select":       {supplyChainImpactWinnerSelectFromSupportsSQL, supplyChainImpactWinnerSelectSQL},
		"scope idents":      {buildWinnersKeyedScopeIdentsFromSupportsSQL, buildWinnersKeyedScopeIdentsSQL},
		"keyed active":      {buildWinnersKeyedActiveFactsFromSupportsSQL, buildWinnersKeyedActiveFactsSQL},
		"keyed operator":    {addWinnersKeyedOperatorFactsFromSupportsSQL, addWinnersKeyedOperatorFactsSQL},
		"keyed narrow":      {buildWinnersKeyedNarrowFromSupportsSQL, buildWinnersKeyedNarrowSQL},
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

// TestWinnersContractRepinnedForSupportsInputs is the differential that lets the
// version-2 pin be re-pinned instead of bumped: reversing the supports swaps in
// every contract statement must give the statements the previous pin hashed.
// The winner output for the same rows is unchanged by construction here (the
// supports payload is the fact payload verbatim); the live byte-identity proof on
// a seed is owed with the live proofs.
func TestWinnersContractRepinnedForSupportsInputs(t *testing.T) {
	t.Parallel()

	shipped := supplyChainImpactWinnersContractStatements()
	reversed := make([]string, len(shipped))
	for i, stmt := range shipped {
		reversed[i] = reverseWinnersSupportsInput(stmt)
	}
	if got := supplyChainImpactWinnersContractHashOf(reversed); got != winnersLegacyContractPin {
		t.Fatalf("the contract statements with the supports swaps reversed hash to %s, want the previous version-2 pin %s", got, winnersLegacyContractPin)
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
		"(f.payload->>'package_id') = ident.package_id",
		"(f.payload->>'repository_id') = ident.repository_id",
		"(f.payload->>'subject_digest') = ident.subject_digest",
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
