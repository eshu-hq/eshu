// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// kevAliasGroupEnvelopes seeds one alias-grouped pair: a GHSA record and a
// CVE record naming each other, sharing the fixture package. The group id is
// the CVE id (lowest-id rule), so a KEV fact on the GHSA member id can only
// reach the group finding through the alias risk-signal merge, never through
// direct-CVE equality.
func kevAliasGroupEnvelopes(ghsa, cve string) []facts.Envelope {
	return []facts.Envelope{
		aliasCVEFact("ghsa-cve", ghsa, "osv", ghsa, ghsa),
		aliasAffectedPackageFact("ghsa-pkg", ghsa, "osv", ghsa),
		aliasCVEFact("nvd-cve", cve, "nvd", cve, cve, ghsa),
		aliasAffectedPackageFact("nvd-pkg", cve, "nvd", cve),
		aliasGroupConsumption("cons-kev-alias"),
	}
}

// TestAliasGroupingPropagatesKEVFromAliasMember is the #10508 pin: one group
// member in the KEV catalog makes the grouped finding carry known_exploited.
// The KEV fact names the GHSA member, not the group id, so only the alias
// risk-signal merge (mergeAliasGroupRiskSignals, KnownExploited arm in
// os_advisory_alias_apply.go) can flag the CVE-keyed finding.
func TestAliasGroupingPropagatesKEVFromAliasMember(t *testing.T) {
	t.Parallel()

	envelopes := append(kevAliasGroupEnvelopes("GHSA-2026-0001", "CVE-2026-0001"),
		vulnerabilityKEVFact("kev-1", "GHSA-2026-0001"))

	byCVE := aliasGroupFindingsByCVE(t, envelopes)
	if len(byCVE) != 1 {
		t.Fatalf("findings = %d (%v), want exactly 1 CVE-keyed group finding", len(byCVE), sortedFindingKeys(byCVE))
	}
	got := byCVE["CVE-2026-0001"]
	if want := []string{"CVE-2026-0001", "GHSA-2026-0001"}; !slices.Equal(got.AdvisoryGroupMembers, want) {
		t.Fatalf("AdvisoryGroupMembers = %#v, want %#v (the KEV must arrive via grouping)", got.AdvisoryGroupMembers, want)
	}
	if !got.KnownExploited {
		t.Fatalf("KnownExploited = false, want true: GHSA-2026-0001 is KEV-listed and grouped under CVE-2026-0001")
	}
	if !slices.Contains(got.EvidenceFactIDs, "kev-1") {
		t.Fatalf("EvidenceFactIDs = %#v, want the KEV fact kev-1 cited as the flag source", got.EvidenceFactIDs)
	}
}

// TestAliasGroupingWithoutKEVMemberStaysUnflagged is the #10508 no-KEV-member
// control: the same group shape with no KEV listing anywhere must not carry
// known_exploited.
func TestAliasGroupingWithoutKEVMemberStaysUnflagged(t *testing.T) {
	t.Parallel()

	byCVE := aliasGroupFindingsByCVE(t, kevAliasGroupEnvelopes("GHSA-2026-0002", "CVE-2026-0002"))
	if len(byCVE) != 1 {
		t.Fatalf("findings = %d (%v), want exactly 1 CVE-keyed group finding", len(byCVE), sortedFindingKeys(byCVE))
	}
	got := byCVE["CVE-2026-0002"]
	if got.KnownExploited {
		t.Fatalf("KnownExploited = true, want false: no group member is KEV-listed")
	}
}
