// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

func identityRoutingDB(identityRows int) *supplyChainImpactRoutingDB {
	routed := &supplyChainImpactRoutingDB{
		branchRows: map[string][][2]string{
			"vulnerability_cve_id":      {{"vuln-affected-package:core", "vulnerability.affected_package"}},
			"vulnerability_suppression": {{"vuln-suppression:one", "vulnerability.suppression"}},
		},
		facts: map[string]string{
			"vuln-affected-package:core": "vulnerability.affected_package",
			"vuln-suppression:one":       "vulnerability.suppression",
		},
	}
	for n := 1; n <= identityRows; n++ {
		routed.identity = append(routed.identity, supplyChainImpactSupportID(n))
	}
	return routed
}

// TestListActiveSupplyChainImpactFactsPagesIdentityIndependently pins the
// canonical identity stream's own keyset: it pages to exhaustion with the
// last support ID as the cursor, and the load order stays core, identity,
// suppression.
func TestListActiveSupplyChainImpactFactsPagesIdentityIndependently(t *testing.T) {
	t.Parallel()

	routed := identityRoutingDB(listFactsByKindPageSize + 2)
	loaded, truncated, err := NewFactStore(routed).ListActiveSupplyChainImpactFacts(context.Background(),
		supplychaincore.SupplyChainImpactFactFilter{CVEIDs: []string{"CVE-2026-5466"}, SubjectDigests: []string{"sha256:a"}})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts() error = %v, want nil", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false")
	}
	if got, want := len(loaded), listFactsByKindPageSize+4; got != want {
		t.Fatalf("len(loaded) = %d, want %d", got, want)
	}
	if loaded[0].FactID != "vuln-affected-package:core" || loaded[len(loaded)-1].FactID != "vuln-suppression:one" {
		t.Fatalf("load order = %q ... %q, want core first and suppression last", loaded[0].FactID, loaded[len(loaded)-1].FactID)
	}
	identity := routed.callsFor("identity")
	if len(identity) != 2 {
		t.Fatalf("identity pages = %d, want 2", len(identity))
	}
	if got, want := identity[1].Args[5], routed.identity[listFactsByKindPageSize-1]; got != want {
		t.Fatalf("second identity cursor = %q, want %q", got, want)
	}
	if got := identity[0].Args[0].([]string); len(got) != 1 || got[0] != "sha256:a" {
		t.Fatalf("identity digests bound %v, want the subject digests", got)
	}
}

// TestListActiveSupplyChainImpactFactsRejectsInvalidStreams pins the load
// contract errors: a non-suppression statement returning a suppression row,
// a malformed or regressing identity stream, and a payload page that does not
// return a resolved ID.
func TestListActiveSupplyChainImpactFactsRejectsInvalidStreams(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		mutate  func(*supplyChainImpactRoutingDB)
		wantErr string
	}{
		{
			name: "suppression row from a core branch",
			mutate: func(r *supplyChainImpactRoutingDB) {
				r.branchRows["vulnerability_cve_id"] = append(r.branchRows["vulnerability_cve_id"], [2]string{"vuln-suppression:two", "vulnerability.suppression"})
			},
			wantErr: "branch vulnerability_cve_id returned vulnerability.suppression fact",
		},
		{
			name: "identity row from a core branch",
			mutate: func(r *supplyChainImpactRoutingDB) {
				r.branchRows["vulnerability_cve_id"] = append(r.branchRows["vulnerability_cve_id"], [2]string{"identity:x", supplyChainImpactContainerImageIdentityFactKind})
			},
			wantErr: "returned identity fact",
		},
		{
			name: "malformed identity fact ID",
			mutate: func(r *supplyChainImpactRoutingDB) {
				r.identity = []string{"reducer_container_image_identity_support:zz"}
			},
			wantErr: "invalid identity fact ID",
		},
		{
			name: "identity cursor regression",
			mutate: func(r *supplyChainImpactRoutingDB) {
				r.identity = []string{supplyChainImpactSupportID(2), supplyChainImpactSupportID(1)}
			},
			wantErr: "identity cursor did not advance",
		},
		{
			name:    "payload page drops a resolved fact",
			mutate:  func(r *supplyChainImpactRoutingDB) { r.missing = map[string]bool{"vuln-affected-package:core": true} },
			wantErr: "payload page returned 0 of 1 facts",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			routed := identityRoutingDB(0)
			test.mutate(routed)
			_, _, err := NewFactStore(routed).ListActiveSupplyChainImpactFacts(context.Background(),
				supplychaincore.SupplyChainImpactFactFilter{CVEIDs: []string{"CVE-2026-5466"}})
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

// TestListActiveSupplyChainImpactFactsDeduplicatesAcrossBranches pins that a
// fact matching two branches (two keys) is fetched and returned once.
func TestListActiveSupplyChainImpactFactsDeduplicatesAcrossBranches(t *testing.T) {
	t.Parallel()

	routed := identityRoutingDB(0)
	routed.branchRows["affected_package_package_id"] = [][2]string{{"vuln-affected-package:core", "vulnerability.affected_package"}}
	loaded, stats, err := NewFactStore(routed).ListActiveSupplyChainImpactFactsWithStats(context.Background(),
		supplychaincore.SupplyChainImpactFactFilter{CVEIDs: []string{"CVE-2026-5466"}, PackageIDs: []string{"npm://x"}})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got, want := len(loaded), 2; got != want {
		t.Fatalf("len(loaded) = %d, want %d (one core row matched twice, one suppression)", got, want)
	}
	if stats.ResolvedFactIDs != 2 || stats.PayloadPages != 2 {
		t.Fatalf("stats resolved=%d pages=%d, want 2 and 2", stats.ResolvedFactIDs, stats.PayloadPages)
	}
	if routed.commits != 1 {
		t.Fatalf("snapshot commits = %d, want one read-only transaction per call", routed.commits)
	}
}
