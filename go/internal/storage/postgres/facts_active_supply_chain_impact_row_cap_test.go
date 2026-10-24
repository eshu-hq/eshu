// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
)

// rowCapRoutingDB primes one core branch with coreCount affected_package rows
// and the suppression branch with suppressionCount rows, in reverse byte
// order so the loader's own sort is what orders them.
func rowCapRoutingDB(coreCount, suppressionCount int) *supplyChainImpactRoutingDB {
	routed := &supplyChainImpactRoutingDB{branchRows: map[string][][2]string{}, facts: map[string]string{}}
	for i := coreCount; i >= 1; i-- {
		id := fmt.Sprintf("vuln-affected-package:row-cap-%04d", i)
		routed.branchRows["vulnerability_cve_id"] = append(routed.branchRows["vulnerability_cve_id"], [2]string{id, "vulnerability.affected_package"})
		routed.facts[id] = "vulnerability.affected_package"
	}
	for i := suppressionCount; i >= 1; i-- {
		id := fmt.Sprintf("vuln-suppression:row-cap-%04d", i)
		routed.branchRows["vulnerability_suppression"] = append(routed.branchRows["vulnerability_suppression"], [2]string{id, "vulnerability.suppression"})
		routed.facts[id] = "vulnerability.suppression"
	}
	return routed
}

func withSuppressionCap(t *testing.T, limit int) {
	t.Helper()
	original := maxSupplyChainImpactActiveEvidenceRowsPerCall
	maxSupplyChainImpactActiveEvidenceRowsPerCall = limit
	t.Cleanup(func() { maxSupplyChainImpactActiveEvidenceRowsPerCall = original })
}

// TestListActiveSupplyChainImpactFactsCapsRowsPerCall is the #5466 round-7
// P1-B proof on the #8880 loader: the suppression statement binds cap + 1,
// a load that sees more than the cap keeps exactly the first cap rows in byte
// order and reports truncation, and core evidence is never bounded.
func TestListActiveSupplyChainImpactFactsCapsRowsPerCall(t *testing.T) {
	withSuppressionCap(t, 3)
	routed := rowCapRoutingDB(700, 4)
	loaded, truncated, err := NewFactStore(routed).ListActiveSupplyChainImpactFacts(context.Background(),
		supplychaincore.SupplyChainImpactFactFilter{CVEIDs: []string{"CVE-2026-0700"}})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts() error = %v, want nil", err)
	}
	if !truncated {
		t.Fatal("truncated = false, want true for 4 suppressions against a 3-row cap")
	}
	if got, want := len(loaded), 700+3; got != want {
		t.Fatalf("len(loaded) = %d, want every core row plus the cap", got)
	}
	if loaded[0].FactID != "vuln-affected-package:row-cap-0001" || loaded[699].FactID != "vuln-affected-package:row-cap-0700" {
		t.Fatalf("core rows not in byte order: first %q last %q", loaded[0].FactID, loaded[699].FactID)
	}
	for i, want := range []string{"vuln-suppression:row-cap-0001", "vuln-suppression:row-cap-0002", "vuln-suppression:row-cap-0003"} {
		if got := loaded[700+i].FactID; got != want {
			t.Fatalf("suppression %d = %q, want %q (first cap rows in byte order)", i, got, want)
		}
	}
	suppression := routed.callsFor("vulnerability_suppression")
	if len(suppression) != 1 {
		t.Fatalf("suppression statements = %d, want 1", len(suppression))
	}
	branch := supplyChainImpactBranch(t, "vulnerability_suppression")
	for i, param := range branch.Params {
		if param == factimpact.SuppressionLimit && suppression[0].Args[i] != 4 {
			t.Fatalf("suppression LIMIT bound %v, want cap + 1 = 4", suppression[0].Args[i])
		}
	}
	if pages := len(routed.callsFor("payload_page")); pages != 3 {
		t.Fatalf("payload pages = %d, want 2 core pages of 500 and 200 plus 1 suppression page", pages)
	}
}

func TestListActiveSupplyChainImpactFactsExactCapIsNotTruncated(t *testing.T) {
	withSuppressionCap(t, 3)
	loaded, truncated, err := NewFactStore(rowCapRoutingDB(1, 3)).ListActiveSupplyChainImpactFacts(context.Background(),
		supplychaincore.SupplyChainImpactFactFilter{CVEIDs: []string{"CVE-2026-0700"}})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts() error = %v, want nil", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false for exactly the cap")
	}
	if got, want := len(loaded), 4; got != want {
		t.Fatalf("len(loaded) = %d, want %d", got, want)
	}
}

func TestListActiveSupplyChainImpactFactsDoesNotTruncateUnderTheCap(t *testing.T) {
	loaded, truncated, err := NewFactStore(rowCapRoutingDB(2, 1)).ListActiveSupplyChainImpactFacts(context.Background(),
		supplychaincore.SupplyChainImpactFactFilter{CVEIDs: []string{"CVE-2026-0700"}})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts() error = %v, want nil", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false far under the cap")
	}
	if got, want := len(loaded), 3; got != want {
		t.Fatalf("len(loaded) = %d, want %d", got, want)
	}
}
