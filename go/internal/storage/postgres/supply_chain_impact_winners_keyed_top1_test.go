// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"strings"
	"testing"
)

// Hermetic pins for the #11120 lane-A TOP-1-per-key probe rewrite (TDD RED:
// the TOP-1 statements do not exist yet). The marker-gated TOP-1 probe keeps
// the triple-guard legs verbatim, ranks per canonical key over a new plain
// covering winner index, and shares the streamed final select byte for byte
// so the projection cannot drift from what the full path and audit read.

// TestWinnersTop1ContractIsVersionSix pins the mandatory contract bump
// (ruling decision 6): the budget redefinition and candidate-set prune change
// which identifiers a tick visits, so no same-version re-pin is allowed.
func TestWinnersTop1ContractIsVersionSix(t *testing.T) {
	t.Parallel()

	if SupplyChainImpactWinnersContractVersion != 6 {
		t.Fatalf("winners contract version = %d, want 6 (lane-A TOP-1 bump)", SupplyChainImpactWinnersContractVersion)
	}
}

// TestWinnersTop1ProbeKeepsTripleGuardLegs pins composition with the shipped
// triple-guard twin: every triple equality and key-presence guard of the
// marker-gated probe renders byte-identically in the TOP-1 probe, so the
// prune cannot widen or narrow the probed set.
func TestWinnersTop1ProbeKeepsTripleGuardLegs(t *testing.T) {
	t.Parallel()

	probe := winnersTop1NarrowProbeSelect(winnersKeyedIdentsTable)
	for _, leg := range []string{
		"s.package_id = ident.package_id",
		"(ident.package_id <> '' OR (s.key_presence & 1) <> 0)",
		"s.repository_id = ident.repository_id",
		"(ident.repository_id <> '' OR (s.key_presence & 2) <> 0)",
		"s.subject_digest = ident.subject_digest",
		"(ident.subject_digest <> '' OR (s.key_presence & 4) <> 0)",
		"COALESCE(NULLIF(s.cve_id, ''), NULLIF(s.advisory_id, ''), '') = ident.c0",
		"s.advisory_id = ident.advisory_id",
	} {
		if !strings.Contains(probe, leg) {
			t.Errorf("TOP-1 probe lacks the triple-guard leg %q", leg)
		}
	}
	if strings.Contains(probe, "fact_records") {
		t.Error("TOP-1 probe reads fact_records: the marker-gated path is plain-column only")
	}
}

// TestWinnersTop1ProbeCarriesRankingColumns pins that the probe emits every
// plain ranking input the winner step orders by: the probe must return all
// copies per identifier, carrying the columns the temp winner index ranks.
func TestWinnersTop1ProbeCarriesRankingColumns(t *testing.T) {
	t.Parallel()

	probe := winnersTop1NarrowProbeSelect(winnersKeyedIdentsTable)
	for _, frag := range []string{
		"s.canonical_key, s.priority_score,",
		"s.has_payload_finding_id",
	} {
		if !strings.Contains(probe, frag) {
			t.Errorf("TOP-1 probe lacks the ranking fragment %q", frag)
		}
	}
}

// TestWinnersTop1SelectRanksPerKey pins the complete ranking order
// (priority_score DESC, has_payload_finding_id DESC, fact_id ASC) in the
// per-key winner lookup over the temp winner index: the covering index must
// serve the probe equalities and this ordering without a Sort node, and an
// INCLUDE column could not serve the has_payload_finding_id ordering.
func TestWinnersTop1SelectRanksPerKey(t *testing.T) {
	t.Parallel()

	if !strings.Contains(winnersKeyedTop1SelectSQL, "WHERE a.canonical_key = k.canonical_key") {
		t.Error("TOP-1 winner select is not a per-key point lookup over the temp winner index")
	}
	for _, frag := range []string{
		"ORDER BY a.priority_score DESC, a.has_payload_finding_id DESC, a.fact_id ASC",
		"LIMIT 1",
	} {
		if !strings.Contains(winnersKeyedTop1SelectSQL, frag) {
			t.Errorf("TOP-1 winner select lacks the ranking fragment %q", frag)
		}
	}
}

// TestWinnersTop1SharesStreamedFinalSelect pins no-drift composition: the
// TOP-1 winner body ends in the streamed final select verbatim, so the
// projection, payload merge, and operator overlay cannot drift from what the
// full path and the audit read.
func TestWinnersTop1SharesStreamedFinalSelect(t *testing.T) {
	t.Parallel()

	if !strings.Contains(winnersKeyedTop1SelectSQL, winnersKeyedStreamedFinalSelectSQL) {
		t.Error("TOP-1 winner select does not embed the streamed final select verbatim")
	}
}

// TestWinnersKeyedTop1TickReadsNoFactRecords pins that no TOP-1 statement
// reads fact_records or the finding fact kind: after the PR-C write stop the
// finding facts are no longer written, so a maintainer statement that still
// read them would read stale rows. The TOP-1 row source is the supports
// table plus the tick's temp tables, exactly like the legacy keyed path.
func TestWinnersKeyedTop1TickReadsNoFactRecords(t *testing.T) {
	t.Parallel()

	for _, stmt := range winnersKeyedTop1Statements() {
		if strings.Contains(stmt, "fact_records") {
			t.Errorf("TOP-1 statement names fact_records:\n%s", stmt)
		}
		if strings.Contains(stmt, "reducer_supply_chain_impact_finding") {
			t.Errorf("TOP-1 statement names the finding fact kind:\n%s", stmt)
		}
	}
}

// TestWinnersTop1CoveringIndexIsInvalidCleanupCovered pins the migration
// shape the schema apply path relies on: 366 holds exactly one CONCURRENTLY
// index statement naming the covering winner index, and the extractor that
// drops INVALID concurrent indexes before re-running finds it.
func TestWinnersTop1CoveringIndexIsInvalidCleanupCovered(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("migrations/366_supply_chain_impact_supports_covering_winner_idx.sql")
	if err != nil {
		t.Fatalf("read migration 366: %v", err)
	}
	body := migrationStatement(string(raw))
	if got := strings.Count(body, ";"); got != 1 {
		t.Fatalf("migration 366 holds %d statements, want exactly 1:\n%s", got, body)
	}
	for _, want := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_covering_winner_idx",
		"canonical_key",
		"priority_score DESC",
		"has_payload_finding_id DESC",
		"fact_id",
		"INCLUDE (scope_id, generation_id, key_presence, cve_id)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("migration 366 body lacks %q:\n%s", want, body)
		}
	}
	got := concurrentIndexNamesForInvalidCleanup(string(raw))
	if len(got) != 1 || got[0] != "supply_chain_impact_finding_supports_covering_winner_idx" {
		t.Errorf("invalid-cleanup names for migration 366 = %v, want exactly the covering winner index", got)
	}
}
