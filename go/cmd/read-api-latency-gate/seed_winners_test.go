// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"testing"
	"time"
)

func testWinnersAnchors() []winnersFactAnchor {
	return []winnersFactAnchor{
		{ScopeID: "seed-scope-terraform_state-0000", GenerationID: "seed-scope-terraform_state-0000-gen-0"},
		{ScopeID: "seed-scope-terraform_state-0001", GenerationID: "seed-scope-terraform_state-0001-gen-0"},
	}
}

// TestBuildWinnersFindingFactsCountsAndAnchors pins the deterministic shape
// of the synthetic findings corpus the winners leg rebuilds from: exact
// count, round-robin anchor assignment onto active generations only, and
// byte-identical rebuilds across calls (the rebuild derives canonical keys
// from these payloads, so nondeterminism here would make the winners table
// unreproducible run to run).
func TestBuildWinnersFindingFactsCountsAndAnchors(t *testing.T) {
	anchors := testWinnersAnchors()
	first := BuildWinnersFindingFacts(anchors, 20, 5)
	if len(first) != 20 {
		t.Fatalf("BuildWinnersFindingFacts returned %d facts, want 20", len(first))
	}
	second := BuildWinnersFindingFacts(anchors, 20, 5)
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first build: %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second build: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("BuildWinnersFindingFacts is not deterministic across calls")
	}
	seenScope := map[string]bool{}
	for i, f := range first {
		if f.FactID == "" {
			t.Fatalf("fact %d has empty FactID", i)
		}
		wantScope := anchors[i%len(anchors)].ScopeID
		if f.ScopeID != wantScope {
			t.Fatalf("fact %d scope %q, want round-robin anchor %q", i, f.ScopeID, wantScope)
		}
		if f.GenerationID != anchors[i%len(anchors)].GenerationID {
			t.Fatalf("fact %d generation %q is not its anchor's active generation", i, f.GenerationID)
		}
		seenScope[f.ScopeID] = true
	}
	if len(seenScope) != len(anchors) {
		t.Fatalf("facts anchor on %d scopes, want all %d anchors used", len(seenScope), len(anchors))
	}
}

// TestBuildWinnersFindingFactsGroupsDedupToWinners pins the group structure
// the rebuild collapses: every group of sourcesPerGroup facts shares one
// canonical-key field set (cve/advisory/package/purl/version/status/repo/
// digest) while carrying distinct fact IDs, so one rebuild produces exactly
// total/sourcesPerGroup winners with source_count > 1.
func TestBuildWinnersFindingFactsGroupsDedupToWinners(t *testing.T) {
	const total, sources = 20, 5
	facts := BuildWinnersFindingFacts(testWinnersAnchors(), total, sources)
	keys := map[string]int{}
	ids := map[string]bool{}
	for _, f := range facts {
		key, err := json.Marshal(canonicalKeyFields(f.Payload))
		if err != nil {
			t.Fatalf("marshal canonical fields: %v", err)
		}
		keys[string(key)]++
		if ids[f.FactID] {
			t.Fatalf("duplicate FactID %q", f.FactID)
		}
		ids[f.FactID] = true
	}
	if len(keys) != total/sources {
		t.Fatalf("got %d canonical keys, want %d groups", len(keys), total/sources)
	}
	for key, n := range keys {
		if n != sources {
			t.Fatalf("canonical key %s has %d sources, want %d", key, n, sources)
		}
	}
}

// TestBuildWinnersFindingFactsRejectsEmptyAnchors pins the fail-closed
// behavior: with no scope to anchor on there is no eligible active
// generation, so the builder returns nothing instead of facts that the
// active-facts join would silently drop (a vacuous winners leg).
func TestBuildWinnersFindingFactsRejectsEmptyAnchors(t *testing.T) {
	if got := BuildWinnersFindingFacts(nil, 20, 5); len(got) != 0 {
		t.Fatalf("BuildWinnersFindingFacts with no anchors returned %d facts, want none", len(got))
	}
	if got := BuildWinnersFindingFacts(testWinnersAnchors(), 0, 5); len(got) != 0 {
		t.Fatalf("BuildWinnersFindingFacts with total 0 returned %d facts, want none", len(got))
	}
}

// TestWinnersFindingVersionedRowsShape pins the row contract of the supports seed
// (#10880 PR-C2 o): the rows are the VersionedRow the impact writer hands its
// supports step, the finding kind and the seed's anchor on every row, and a payload
// carrying every field the canonical-key SQL and the winner body select read.
func TestWinnersFindingVersionedRowsShape(t *testing.T) {
	facts := BuildWinnersFindingFacts(testWinnersAnchors(), 10, 5)
	now := time.Now().UTC().Truncate(time.Second)
	rows, err := winnersFindingVersionedRows(facts, now)
	if err != nil {
		t.Fatalf("winnersFindingVersionedRows: %v", err)
	}
	if len(rows) != len(facts) {
		t.Fatalf("winnersFindingVersionedRows returned %d rows for %d facts", len(rows), len(facts))
	}
	requiredPayloadKeys := []string{
		"finding_id", "cve_id", "advisory_id", "package_id", "purl",
		"observed_version", "impact_status", "repository_id",
		"ecosystem", "cvss_score", "priority_score", "priority_bucket",
		"detection_profile", "match_reason", "suppression_state",
		"service_ids", "workload_ids", "environments", "image_ref",
	}
	for i, row := range rows {
		if row.FactKind != winnersSeedFactKind || row.IsTombstone {
			t.Fatalf("row %d fact_kind %q tombstone %v, want a live %q row", i, row.FactKind, row.IsTombstone, winnersSeedFactKind)
		}
		if row.FactID != facts[i].FactID || row.ScopeID != facts[i].ScopeID || row.GenerationID != facts[i].GenerationID {
			t.Fatalf("row %d anchor (%s,%s,%s), want (%s,%s,%s)", i, row.FactID, row.ScopeID, row.GenerationID, facts[i].FactID, facts[i].ScopeID, facts[i].GenerationID)
		}
		if row.StableFactKey == "" || row.SourceConfidence == "" {
			t.Fatalf("row %d carries no stable key or source confidence", i)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			t.Fatalf("row %d payload is not JSON: %v", i, err)
		}
		for _, key := range requiredPayloadKeys {
			if _, ok := payload[key]; !ok {
				t.Fatalf("row %d payload misses required key %q", i, key)
			}
		}
		if payload["suppression_state"] != "active" {
			t.Fatalf("row %d suppression_state %v, want active (suppressed rows never reach winners)", i, payload["suppression_state"])
		}
	}
}

// TestWinnersSeedFactKindMatchesReadModel pins the kind string against the
// value the query read model filters on: a drift here would seed facts the
// rebuild's active_facts CTE never sees.
func TestWinnersSeedFactKindMatchesReadModel(t *testing.T) {
	if winnersSeedFactKind != "reducer_supply_chain_impact_finding" {
		t.Fatalf("winnersSeedFactKind %q does not match the impact read model fact kind", winnersSeedFactKind)
	}
}

// TestExpectedWinnerGroupsMatchesTheBuilder pins the winners count the seed
// must prove: one canonical-key group rebuilds to exactly one winner, so the
// expected row count is the number of distinct groups BuildWinnersFindingFacts
// emits, including a final partial group.
func TestExpectedWinnerGroupsMatchesTheBuilder(t *testing.T) {
	t.Parallel()

	anchors := []winnersFactAnchor{{ScopeID: "s", GenerationID: "g"}}
	for _, tc := range []struct{ total, perGroup int }{{20000, 5}, {11, 5}, {5, 5}, {1, 5}, {0, 5}} {
		groups := map[any]bool{}
		for _, f := range BuildWinnersFindingFacts(anchors, tc.total, tc.perGroup) {
			groups[f.Payload["finding_id"]] = true
		}
		if got, want := expectedWinnerGroups(tc.total, tc.perGroup), int64(len(groups)); got != want {
			t.Errorf("expectedWinnerGroups(%d, %d) = %d, builder emits %d groups", tc.total, tc.perGroup, got, want)
		}
	}
	if got := expectedWinnerGroups(20000, 5); got != 4000 {
		t.Errorf("expectedWinnerGroups(20000, 5) = %d, want 4000 (the default corpus)", got)
	}
}

// TestCheckWinnersCountRefusesAShortOrLongTable proves the seed fails when the
// rebuild produced a different number of winners than the seeded groups: a
// partial rebuild must not pass on count > 0.
func TestCheckWinnersCountRefusesAShortOrLongTable(t *testing.T) {
	t.Parallel()

	if err := checkWinnersCount(4000, 4000); err != nil {
		t.Fatalf("checkWinnersCount(4000, 4000) = %v, want nil", err)
	}
	for _, got := range []int64{0, 3999, 4001} {
		if err := checkWinnersCount(got, 4000); err == nil {
			t.Errorf("checkWinnersCount(%d, 4000) = nil, want an error", got)
		}
	}
}
