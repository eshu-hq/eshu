// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// aliasDenyConfig builds a deny-only pass config for the tests below.
func aliasDenyConfig(t *testing.T, deny ...string) supplyChainAliasGroupConfig {
	t.Helper()
	return supplyChainAliasGroupConfigFromLists(nil, deny)
}

// aliasDenyCVEIDs returns the CVE ids left in the index after the pass, in
// index order.
func aliasDenyCVEIDs(index *supplyChainImpactIndex) []string {
	out := make([]string, 0, len(index.cves))
	for _, cve := range index.cves {
		out = append(out, cve.CVEID)
	}
	return out
}

func aliasDenyContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// TestAliasDenyKeepsEmptyAdvisoryIDObservations pins finding (1): a deny
// list must not drop native observations that carry no AdvisoryID (and so
// own no alias-group record). Only a recordless observation that itself
// names a listed id goes away.
func TestAliasDenyKeepsEmptyAdvisoryIDObservations(t *testing.T) {
	t.Parallel()

	config := aliasDenyConfig(t, "GHSA-2026-denied", "CVE-2026-9003")
	index := &supplyChainImpactIndex{
		cves: []supplychainmodel.ImpactCVE{
			{FactID: "denied-cve", CVEID: "CVE-2026-9001", AdvisoryID: "GHSA-2026-denied", Aliases: []string{"CVE-2026-9001"}},
			{FactID: "bare-cve", CVEID: "CVE-2026-9002", AdvisoryID: ""},
			{FactID: "bare-denied-cve", CVEID: "CVE-2026-9003", AdvisoryID: ""},
		},
	}

	groupOSAdvisoriesByAlias(index, config)

	kept := aliasDenyCVEIDs(index)
	if !aliasDenyContains(kept, "CVE-2026-9002") {
		t.Fatalf("index.cves = %v, want the empty-AdvisoryID CVE-2026-9002 observation to survive the deny pass", kept)
	}
	if aliasDenyContains(kept, "CVE-2026-9001") {
		t.Fatalf("index.cves = %v, want the denied record's CVE-2026-9001 observation dropped", kept)
	}
	if aliasDenyContains(kept, "CVE-2026-9003") {
		t.Fatalf("index.cves = %v, want the recordless CVE-2026-9003 observation dropped: its own CVE id is denied", kept)
	}
}

// TestAliasDenyDropsDeniedRecordProducts pins finding (2): the products a
// denied record names contribute to no finding. Products under a key the
// denied record owns alone go away; products under a key shared with a kept
// record stay with the kept record's finding.
func TestAliasDenyDropsDeniedRecordProducts(t *testing.T) {
	t.Parallel()

	config := aliasDenyConfig(t, "GHSA-2026-denied")
	index := &supplyChainImpactIndex{
		cves: []supplychainmodel.ImpactCVE{
			{FactID: "denied-cve", CVEID: "CVE-2026-9001", AdvisoryID: "GHSA-2026-denied", Aliases: []string{"CVE-2026-9001"}},
			{FactID: "denied-cve-2", CVEID: "CVE-2026-9009", AdvisoryID: "GHSA-2026-denied", Aliases: []string{"CVE-2026-9009"}},
			{FactID: "kept-cve", CVEID: "CVE-2026-9002", AdvisoryID: "GHSA-2026-kept", Aliases: []string{"GHSA-2026-kept"}},
			{FactID: "kept-twin", CVEID: "CVE-2026-9001", AdvisoryID: "GHSA-2026-kept", Aliases: []string{"GHSA-2026-kept"}},
		},
		affectedPackages: map[string][]supplychainmodel.AffectedPackage{
			"CVE-2026-9009": {{FactID: "denied-pkg", CVEID: "CVE-2026-9009", AdvisoryID: "GHSA-2026-denied"}},
			"CVE-2026-9002": {{FactID: "kept-pkg", CVEID: "CVE-2026-9002", AdvisoryID: "GHSA-2026-kept"}},
		},
		affectedProducts: map[string][]supplychainmodel.AffectedProduct{
			"CVE-2026-9009": {{FactID: "denied-product", CVEID: "CVE-2026-9009"}},
			"CVE-2026-9001": {{FactID: "shared-product", CVEID: "CVE-2026-9001"}},
			"CVE-2026-9002": {{FactID: "kept-product", CVEID: "CVE-2026-9002"}},
		},
	}

	groupOSAdvisoriesByAlias(index, config)

	if _, ok := index.affectedProducts["CVE-2026-9009"]; ok {
		t.Fatalf("affectedProducts still holds CVE-2026-9009 = %#v, want the denied record's products removed", index.affectedProducts["CVE-2026-9009"])
	}
	if products := index.affectedProducts["CVE-2026-9001"]; len(products) != 1 {
		t.Fatalf("affectedProducts[CVE-2026-9001] = %#v, want the shared key to stay: the kept record still names it", index.affectedProducts["CVE-2026-9001"])
	}
	if products := index.affectedProducts["CVE-2026-9002"]; len(products) != 1 {
		t.Fatalf("affectedProducts[CVE-2026-9002] = %#v, want the kept record's product to stay", index.affectedProducts["CVE-2026-9002"])
	}
	for _, pkgs := range index.affectedPackages {
		for _, pkg := range pkgs {
			if pkg.AdvisoryID == "GHSA-2026-denied" {
				t.Fatalf("affectedPackages still holds denied package %#v, want every denied-advisory package removed", pkg)
			}
		}
	}
}

// TestAliasDenyDropsRecordNamingDeniedAlias pins finding (3): deny drops a
// record that names a listed id through its aliases, not just through its
// advisory or CVE ids. The record contributes no observation, package, or
// product, and the untouched peer stays a singleton.
func TestAliasDenyDropsRecordNamingDeniedAlias(t *testing.T) {
	t.Parallel()

	config := aliasDenyConfig(t, "CVE-2026-9007")
	index := &supplyChainImpactIndex{
		cves: []supplychainmodel.ImpactCVE{
			{FactID: "alias-cve", CVEID: "GHSA-2026-0007", AdvisoryID: "GHSA-2026-0007", Aliases: []string{"GHSA-2026-0007", "CVE-2026-9007"}},
			{FactID: "peer-cve", CVEID: "CVE-2026-0008", AdvisoryID: "CVE-2026-0008", Aliases: []string{"CVE-2026-0008"}},
		},
		affectedPackages: map[string][]supplychainmodel.AffectedPackage{
			"GHSA-2026-0007": {{FactID: "alias-pkg", CVEID: "GHSA-2026-0007", AdvisoryID: "GHSA-2026-0007"}},
			"CVE-2026-0008":  {{FactID: "peer-pkg", CVEID: "CVE-2026-0008", AdvisoryID: "CVE-2026-0008"}},
		},
	}

	groupOSAdvisoriesByAlias(index, config)

	kept := aliasDenyCVEIDs(index)
	if len(kept) != 1 || kept[0] != "CVE-2026-0008" {
		t.Fatalf("index.cves = %v, want only the untouched peer CVE-2026-0008", kept)
	}
	if _, ok := index.affectedPackages["GHSA-2026-0007"]; ok {
		t.Fatalf("affectedPackages still holds GHSA-2026-0007, want the alias-denied record's packages removed")
	}
	if got := index.aliasStats.cells[supplyChainAliasGroupKey{result: aliasGroupResultDenied}]; got != 1 {
		t.Fatalf("denied cell = %d, want 1 for the record naming CVE-2026-9007 through its aliases", got)
	}
}

// TestAliasDenyHoldsOnCutLoadPass pins #11111: a truncated load disables
// alias grouping, but the deny list is an operator promise that holds on
// every pass. A cut pass therefore still drops a record naming a listed id
// (advisory id, CVE id, or alias) with its packages and products, and still
// drops a recordless observation naming one, while it groups nothing.
func TestAliasDenyHoldsOnCutLoadPass(t *testing.T) {
	t.Parallel()

	config := aliasDenyConfig(t, "GHSA-2026-denied", "CVE-2026-9007", "CVE-2026-9003")
	config.disabled = true
	index := &supplyChainImpactIndex{
		cves: []supplychainmodel.ImpactCVE{
			{FactID: "denied-cve", CVEID: "CVE-2026-9001", AdvisoryID: "GHSA-2026-denied", Aliases: []string{"CVE-2026-9001"}},
			{FactID: "alias-denied", CVEID: "GHSA-2026-0007", AdvisoryID: "GHSA-2026-0007", Aliases: []string{"GHSA-2026-0007", "CVE-2026-9007"}},
			{FactID: "bare-denied", CVEID: "CVE-2026-9003", AdvisoryID: ""},
			{FactID: "bare-kept", CVEID: "CVE-2026-9002", AdvisoryID: ""},
			{FactID: "kept-cve", CVEID: "CVE-2026-0008", AdvisoryID: "CVE-2026-0008", Aliases: []string{"CVE-2026-0008"}},
			{FactID: "upstream-copy", CVEID: "CVE-2026-9001", AdvisoryID: "DSA-1", UpstreamCopy: true},
		},
		affectedPackages: map[string][]supplychainmodel.AffectedPackage{
			"CVE-2026-9001":  {{FactID: "denied-pkg", CVEID: "CVE-2026-9001", AdvisoryID: "GHSA-2026-denied"}},
			"GHSA-2026-0007": {{FactID: "alias-pkg", CVEID: "GHSA-2026-0007", AdvisoryID: "GHSA-2026-0007"}},
			"CVE-2026-0008":  {{FactID: "kept-pkg", CVEID: "CVE-2026-0008", AdvisoryID: "CVE-2026-0008"}},
		},
		affectedProducts: map[string][]supplychainmodel.AffectedProduct{
			"GHSA-2026-0007": {{FactID: "alias-product", CVEID: "GHSA-2026-0007"}},
			"CVE-2026-0008":  {{FactID: "kept-product", CVEID: "CVE-2026-0008"}},
		},
	}

	groupOSAdvisoriesByAlias(index, config)

	var kept []string
	for _, cve := range index.cves {
		kept = append(kept, cve.FactID)
	}
	wantKept := []string{"bare-kept", "kept-cve", "upstream-copy"}
	if len(kept) != len(wantKept) {
		t.Fatalf("index.cves fact ids = %v, want %v: a cut pass must still apply the deny list", kept, wantKept)
	}
	for i := range wantKept {
		if kept[i] != wantKept[i] {
			t.Fatalf("index.cves fact ids = %v, want %v", kept, wantKept)
		}
	}
	for _, key := range []string{"CVE-2026-9001", "GHSA-2026-0007"} {
		if pkgs, ok := index.affectedPackages[key]; ok {
			t.Fatalf("affectedPackages[%s] = %#v, want the denied record's packages removed on a cut pass", key, pkgs)
		}
	}
	if _, ok := index.affectedProducts["GHSA-2026-0007"]; ok {
		t.Fatalf("affectedProducts still holds GHSA-2026-0007, want the denied record's products removed on a cut pass")
	}
	if len(index.affectedPackages["CVE-2026-0008"]) != 1 || len(index.affectedProducts["CVE-2026-0008"]) != 1 {
		t.Fatalf("the kept record lost evidence: packages %#v products %#v", index.affectedPackages["CVE-2026-0008"], index.affectedProducts["CVE-2026-0008"])
	}
	if len(index.aliasGroups) != 0 || index.aliasStats.groups != 0 {
		t.Fatalf("a cut pass grouped: aliasGroups %v groups %d, want none", index.aliasGroups, index.aliasStats.groups)
	}
	if got := index.aliasStats.cells[supplyChainAliasGroupKey{result: aliasGroupResultDenied}]; got != 2 {
		t.Fatalf("denied cell = %d, want 2 denied records counted on the cut pass", got)
	}
}

// TestAliasCutLoadWithoutDenyKeepsIndexUntouched pins that the deny sweep on a
// cut pass is a no-op with empty lists: nothing is dropped, rekeyed, or
// counted, which is the production default today.
func TestAliasCutLoadWithoutDenyKeepsIndexUntouched(t *testing.T) {
	t.Parallel()

	config := supplyChainAliasGroupConfig{disabled: true}
	index := &supplyChainImpactIndex{
		cves: []supplychainmodel.ImpactCVE{
			{FactID: "a", CVEID: "CVE-2026-0001", AdvisoryID: "GHSA-A", Aliases: []string{"CVE-2026-0002"}},
			{FactID: "b", CVEID: "CVE-2026-0002", AdvisoryID: "GHSA-B", Aliases: []string{"CVE-2026-0001"}},
		},
	}

	groupOSAdvisoriesByAlias(index, config)

	if got := aliasDenyCVEIDs(index); len(got) != 2 || got[0] != "CVE-2026-0001" || got[1] != "CVE-2026-0002" {
		t.Fatalf("index.cves = %v, want both records untouched and unrekeyed on a cut pass", got)
	}
	if len(index.aliasStats.cells) != 0 || index.aliasStats.groups != 0 {
		t.Fatalf("aliasStats = %+v, want empty on a cut pass with no deny list", index.aliasStats)
	}
}
