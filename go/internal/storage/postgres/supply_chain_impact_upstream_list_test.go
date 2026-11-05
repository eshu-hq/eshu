// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"slices"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
)

func upstreamCVEIDs(n int) []string {
	ids := make([]string, 0, n)
	for i := range n {
		ids = append(ids, fmt.Sprintf("CVE-2024-%05d", i))
	}
	return ids
}

// TestUpstreamListIsChunkedOnTheKeyProbe is the #9914 plan guard. The cve key probe
// walks the whole advisory index on a custom plan at 172 to 199 ids and from about
// 400, so no statement may carry more than the bound. The union of the chunks is the
// whole list, each id once, and the row sets merge.
func TestUpstreamListIsChunkedOnTheKeyProbe(t *testing.T) {
	t.Parallel()

	ids := upstreamCVEIDs(500)
	routed := &supplyChainImpactRoutingDB{branchRows: map[string][][2]string{}, facts: map[string]string{}}
	routed.branchRows[factimpact.BranchVulnerabilityUpstreamCVEID] = [][2]string{{"kev-1", "vulnerability.known_exploited"}}
	routed.facts["kev-1"] = "vulnerability.known_exploited"
	loaded, stats, err := NewFactStore(routed).ListActiveSupplyChainImpactFactsWithStats(context.Background(),
		supplychaincore.SupplyChainImpactFactFilter{UpstreamCVEIDs: ids})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFactsWithStats: %v", err)
	}
	calls := routed.callsFor(factimpact.BranchVulnerabilityUpstreamCVEID)
	wantStatements := (len(ids) + factimpact.MaxCVEIDsPerStatement - 1) / factimpact.MaxCVEIDsPerStatement
	if len(calls) != wantStatements {
		t.Fatalf("statements = %d, want %d for %d ids at %d", len(calls), wantStatements, len(ids), factimpact.MaxCVEIDsPerStatement)
	}
	var bound []string
	for _, call := range calls {
		list := boundList(t, call, factimpact.UpstreamCVEIDs)
		if len(list) > factimpact.MaxCVEIDsPerStatement {
			t.Errorf("one statement bound %d ids", len(list))
		}
		bound = append(bound, list...)
	}
	if !slices.Equal(bound, ids) {
		t.Errorf("chunks bound %d ids, want every id once and in order", len(bound))
	}
	if len(loaded) != 1 || loaded[0].FactID != "kev-1" {
		t.Errorf("loaded = %#v, want the one row, deduplicated across chunks", loaded)
	}
	statements := 0
	for _, branch := range stats.Branches {
		if branch.Branch == factimpact.BranchVulnerabilityUpstreamCVEID {
			statements++
		}
	}
	if statements != wantStatements {
		t.Errorf("branch stats = %d, want one per statement so an operator sees each duration", statements)
	}
}

// upstreamLowestMeasuredWalk is the smallest id count at which a QA plan-only
// custom-plan sweep walked advisory_lookup_v2 instead of probing cve_lookup_v2. It
// probes again from 200 to 388, but that window moves with random_page_cost, and 172
// itself moves with the stored correlation of advisory_lookup_v2, so no size at or
// above this one is allowed. The planner prices each id at the default 0.005 and sums
// them up to 200 ids (scalararraysel), so the estimate climbs toward the whole kind
// just under 200. This guards the bound; it does not read the planner.
const upstreamLowestMeasuredWalk = 172

// upstreamChunkSizes returns each distinct statement size the production loader
// emits across upstream lists of 1 to longestList ids, mapped to the shortest list
// that emits it.
func upstreamChunkSizes(t *testing.T, longestList int) map[int]int {
	t.Helper()
	sizes := map[int]int{}
	for length := 1; length <= longestList; length++ {
		routed := &supplyChainImpactRoutingDB{branchRows: map[string][][2]string{}, facts: map[string]string{}}
		if _, _, err := NewFactStore(routed).ListActiveSupplyChainImpactFacts(context.Background(),
			supplychaincore.SupplyChainImpactFactFilter{UpstreamCVEIDs: upstreamCVEIDs(length)}); err != nil {
			t.Fatalf("ListActiveSupplyChainImpactFacts(%d ids): %v", length, err)
		}
		for _, call := range routed.callsFor(factimpact.BranchVulnerabilityUpstreamCVEID) {
			size := len(boundList(t, call, factimpact.UpstreamCVEIDs))
			if _, seen := sizes[size]; !seen {
				sizes[size] = length
			}
		}
	}
	return sizes
}

// TestUpstreamChunksStayBelowTheLowestMeasuredWalk: no upstream list may emit a
// statement that large. A list shorter than the bound runs whole and a longer one
// leaves a remainder, so the sizes come from the production chunker, over lists long
// enough to emit every size it can.
func TestUpstreamChunksStayBelowTheLowestMeasuredWalk(t *testing.T) {
	t.Parallel()

	for size, length := range upstreamChunkSizes(t, 2*factimpact.MaxCVEIDsPerStatement+1) {
		if size >= upstreamLowestMeasuredWalk {
			t.Errorf("a list of %d ids emits a statement of %d ids, at or above the lowest measured walk %d (bound %d)",
				length, size, upstreamLowestMeasuredWalk, factimpact.MaxCVEIDsPerStatement)
		}
	}
}

// TestUpstreamListAloneRunsSignalStatementsOnly: a follow-up round whose only new
// input is the upstream list reads the signal arm and the suppression statement,
// which sees the whole list, and nothing that loads an advisory or a product.
func TestUpstreamListAloneRunsSignalStatementsOnly(t *testing.T) {
	t.Parallel()

	ids := upstreamCVEIDs(300)
	routed := &supplyChainImpactRoutingDB{branchRows: map[string][][2]string{}, facts: map[string]string{}}
	if _, _, err := NewFactStore(routed).ListActiveSupplyChainImpactFacts(context.Background(),
		supplychaincore.SupplyChainImpactFactFilter{UpstreamCVEIDs: ids}); err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts: %v", err)
	}
	var labels []string
	for _, call := range routed.calls {
		if call.Label != "identity" && !slices.Contains(labels, call.Label) { // The identity stream runs on every call.
			labels = append(labels, call.Label)
		}
	}
	slices.Sort(labels)
	if want := []string{"vulnerability_suppression", factimpact.BranchVulnerabilityUpstreamCVEID}; !slices.Equal(labels, want) {
		t.Errorf("statements = %v, want %v", labels, want)
	}
	suppression := routed.callsFor("vulnerability_suppression")
	if len(suppression) != 1 || !slices.Equal(boundList(t, suppression[0], factimpact.UpstreamCVEIDs), ids) {
		t.Errorf("suppression statements = %d, want 1 with the whole list", len(suppression))
	}
}

// TestUpstreamListIsCleaned: a list of blanks is an empty list, so the loader
// issues nothing.
func TestUpstreamListIsCleaned(t *testing.T) {
	t.Parallel()

	routed := &supplyChainImpactRoutingDB{branchRows: map[string][][2]string{}, facts: map[string]string{}}
	if _, _, err := NewFactStore(routed).ListActiveSupplyChainImpactFacts(context.Background(),
		supplychaincore.SupplyChainImpactFactFilter{UpstreamCVEIDs: []string{"", "  "}}); err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts: %v", err)
	}
	if len(routed.calls) != 0 {
		t.Errorf("calls = %#v, want none", routed.calls)
	}
}
