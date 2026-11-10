// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

var (
	aliasMatchNow      = time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	aliasMatchAuthored = time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
)

func aliasMatchSuppression(scope vulnerabilitySuppressionScope) vulnerabilitySuppression {
	return vulnerabilitySuppression{
		SuppressionID: "suppression-alias-10409",
		Source:        chain.VulnerabilitySuppressionSourceVEX,
		Justification: chain.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    aliasMatchAuthored,
		Scope:         scope,
	}
}

// TestAliasMatchCrossFieldAdvisoryInCVEFieldUngrouped is ruling D1's
// cross-field case on a fallback (ungrouped) finding: the matchable set is
// {CVEID, AdvisoryID}, so a GHSA id carried in scope cve_id matches the
// finding's advisory id.
func TestAliasMatchCrossFieldAdvisoryInCVEFieldUngrouped(t *testing.T) {
	t.Parallel()
	finding := SupplyChainImpactFinding{CVEID: "CVE-2026-0001", AdvisoryID: "GHSA-aaaa-bbbb"}
	s := aliasMatchSuppression(vulnerabilitySuppressionScope{CVEID: "GHSA-aaaa-bbbb"})
	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{s}, aliasMatchNow)
	if decision.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("State = %q, want %q: GHSA id in cve_id field must match finding advisory_id (D1 cross-field)", decision.State, SupplyChainSuppressionStateNotAffected)
	}
}

// TestAliasMatchCrossFieldCVEInAdvisoryFieldGrouped covers the upstream-CVE
// case: the finding's CVEID need not be a group member, and a suppression
// naming it in advisory_id still matches through the matchable set.
func TestAliasMatchCrossFieldCVEInAdvisoryFieldGrouped(t *testing.T) {
	t.Parallel()
	finding := SupplyChainImpactFinding{
		CVEID:                "CVE-2026-9999",
		AdvisoryID:           "DSA-1234-1",
		AdvisoryGroupMembers: []string{"DSA-1234-1", "GHSA-cccc"},
	}
	s := aliasMatchSuppression(vulnerabilitySuppressionScope{AdvisoryID: "CVE-2026-9999"})
	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{s}, aliasMatchNow)
	if decision.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("State = %q, want %q: CVE id in advisory_id field must match finding cve_id (D1 union)", decision.State, SupplyChainSuppressionStateNotAffected)
	}
}

// TestAliasMatchBothFieldsTwoGroupsNoMatch pins conjunctive semantics: a
// scope naming one member of this group and an id of another group matches
// neither.
func TestAliasMatchBothFieldsTwoGroupsNoMatch(t *testing.T) {
	t.Parallel()
	finding := SupplyChainImpactFinding{
		CVEID:                "CVE-1",
		AdvisoryID:           "GHSA-x",
		AdvisoryGroupMembers: []string{"CVE-1", "CVE-2", "GHSA-x"},
	}
	s := aliasMatchSuppression(vulnerabilitySuppressionScope{CVEID: "CVE-2", AdvisoryID: "GHSA-other-group"})
	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{s}, aliasMatchNow)
	if decision.State == SupplyChainSuppressionStateNotAffected {
		t.Fatalf("State = %q: scope naming ids from two groups must not hide either group", decision.State)
	}
}

// TestAliasMatchDecisionRecordsMatchedMember proves D1's audit clause: the
// decision names the matched member id, the scope field that supplied it,
// and that the match came through the group rather than a direct naming.
func TestAliasMatchDecisionRecordsMatchedMember(t *testing.T) {
	t.Parallel()
	finding := SupplyChainImpactFinding{
		CVEID:                "CVE-1",
		AdvisoryID:           "GHSA-x",
		AdvisoryGroupMembers: []string{"CVE-1", "GHSA-x", "GHSA-y"},
	}
	s := aliasMatchSuppression(vulnerabilitySuppressionScope{CVEID: "GHSA-y"})
	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{s}, aliasMatchNow)
	if decision.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("State = %q, want %q", decision.State, SupplyChainSuppressionStateNotAffected)
	}
	if decision.MatchedID != "GHSA-y" || decision.MatchedField != "cve_id" || decision.MatchedVia != SuppressionMatchedViaGroupMember {
		t.Fatalf("Matched = (%q, %q, %q), want (GHSA-y, cve_id, group_member)", decision.MatchedID, decision.MatchedField, decision.MatchedVia)
	}

	direct := aliasMatchSuppression(vulnerabilitySuppressionScope{AdvisoryID: "GHSA-x"})
	decision = EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{direct}, aliasMatchNow)
	if decision.MatchedID != "GHSA-x" || decision.MatchedField != "advisory_id" || decision.MatchedVia != SuppressionMatchedViaDirect {
		t.Fatalf("Matched = (%q, %q, %q), want (GHSA-x, advisory_id, direct)", decision.MatchedID, decision.MatchedField, decision.MatchedVia)
	}
}

// TestChangedSuppressionIDsIncludeAdvisoryBothSides is ruling D4's
// failing-first case: an advisory-id-only suppression change must enter the
// changed-id set on both sides, and must not count as unreachable.
func TestChangedSuppressionIDsIncludeAdvisoryBothSides(t *testing.T) {
	t.Parallel()
	rows := []suppressionDiffRow{
		{SuppressionID: "adv-only", CurrentAdvisoryID: "GHSA-new", PriorAdvisoryID: "GHSA-old"},
		{SuppressionID: "cve-only", CurrentCVEID: "CVE-2026-1"},
		{SuppressionID: "neither"},
	}
	got := changedSuppressionIDs(rows)
	for _, want := range []string{"GHSA-new", "GHSA-old", "CVE-2026-1"} {
		found := false
		for _, id := range got {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("changedSuppressionIDs() = %#v, missing %q", got, want)
		}
	}
	if n := countSuppressionsWithoutIdentityScope(rows); n != 1 {
		t.Fatalf("countSuppressionsWithoutIdentityScope() = %d, want 1 (only the row naming neither id)", n)
	}
}

// TestAliasMatchScopeMismatchReasonOmitsMatchedIdentity proves ruling D1's
// adjacency clause: an alias-adjacent suppression narrowed by package reports
// scope_mismatch, and the reason must not claim the matched identity differs.
func TestAliasMatchScopeMismatchReasonOmitsMatchedIdentity(t *testing.T) {
	t.Parallel()
	finding := SupplyChainImpactFinding{
		CVEID:                "CVE-1",
		AdvisoryID:           "GHSA-x",
		PackageID:            "pkg:npm/real",
		AdvisoryGroupMembers: []string{"CVE-1", "CVE-2", "GHSA-x"},
	}
	s := aliasMatchSuppression(vulnerabilitySuppressionScope{CVEID: "CVE-2", PackageID: "pkg:npm/other"})
	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{s}, aliasMatchNow)
	if decision.State != SupplyChainSuppressionStateScopeMismatch {
		t.Fatalf("State = %q, want %q", decision.State, SupplyChainSuppressionStateScopeMismatch)
	}
	if strings.Contains(decision.Reason, "cve_id=") {
		t.Fatalf("Reason = %q: must not report cve_id mismatch when the id matched a group member", decision.Reason)
	}
	if !strings.Contains(decision.Reason, "package_id=") {
		t.Fatalf("Reason = %q: must report the narrowing package mismatch", decision.Reason)
	}
}

// TestSuppressionIndexMatchesFullScan is the D2 differential: the per-pass
// index is a pure prefilter, so applying suppressions through it must yield
// byte-identical decisions to the full scan over a seeded mixed corpus
// (grouped and ungrouped findings; identity, package, repo, digest, and
// anchorless scopes; expired and provider candidates; case and whitespace
// variants; one non-ASCII lookalike that must ride the fallback).
func TestSuppressionIndexMatchesFullScan(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(10409, 42))
	ids := []string{"CVE-2026-1", "CVE-2026-2", "GHSA-a", "GHSA-b", "DSA-1", "cve-2026-1", " GHSA-a ", "CVE-2026-9", "pkg:npm/Kelvin"}
	pkgs := []string{"pkg:npm/one", "pkg:npm/two", "PKG:NPM/ONE", ""}
	repos := []string{"repo://acme/api", "repo://acme/web", ""}
	pick := func(xs []string) string { return xs[rng.IntN(len(xs))] }

	findings := make([]SupplyChainImpactFinding, 0, 60)
	for i := 0; i < 60; i++ {
		f := SupplyChainImpactFinding{
			CVEID:         pick(ids[:6]),
			AdvisoryID:    pick(ids[:6]),
			PackageID:     pick(pkgs),
			RepositoryID:  pick(repos),
			SubjectDigest: fmt.Sprintf("sha256:d%d", rng.IntN(4)),
		}
		if rng.IntN(2) == 0 {
			f.AdvisoryGroupMembers = []string{"CVE-2026-1", "CVE-2026-2", "GHSA-a", "GHSA-b"}
		}
		findings = append(findings, f)
	}
	findings = append(findings, SupplyChainImpactFinding{CVEID: "CVE-2026-1", PackageID: "pkg:npm/kelvin"})

	suppressions := make([]vulnerabilitySuppression, 0, 120)
	sources := []string{chain.VulnerabilitySuppressionSourceVEX, chain.VulnerabilitySuppressionSourcePolicy, chain.VulnerabilitySuppressionSourceProviderDismissal}
	for i := 0; i < 120; i++ {
		s := vulnerabilitySuppression{
			SuppressionID: fmt.Sprintf("s-%03d", i),
			Source:        sources[rng.IntN(len(sources))],
			Justification: chain.VulnerabilitySuppressionJustificationNotAffected,
			AuthoredAt:    aliasMatchAuthored.Add(time.Duration(rng.IntN(72)) * time.Hour),
			Scope: vulnerabilitySuppressionScope{
				CVEID:         pick(append(ids, "")),
				AdvisoryID:    pick(append(ids, "", "")),
				PackageID:     pick(pkgs),
				RepositoryID:  pick(repos),
				SubjectDigest: pick([]string{"", "sha256:d1", "SHA256:D2"}),
			},
		}
		if rng.IntN(4) == 0 {
			s.ExpiresAt = aliasMatchAuthored
		}
		suppressions = append(suppressions, s)
	}
	// Anchorless and Kelvin-lookalike package scopes exercise the fallback.
	suppressions = append(suppressions,
		vulnerabilitySuppression{SuppressionID: "anchorless", Source: chain.VulnerabilitySuppressionSourcePolicy, Justification: chain.VulnerabilitySuppressionJustificationIgnored, AuthoredAt: aliasMatchAuthored},
		vulnerabilitySuppression{SuppressionID: "kelvin", Source: chain.VulnerabilitySuppressionSourcePolicy, Justification: chain.VulnerabilitySuppressionJustificationFalsePositive, AuthoredAt: aliasMatchAuthored, Scope: vulnerabilitySuppressionScope{PackageID: "pkg:npm/Kelvin"}},
	)

	indexed := append([]SupplyChainImpactFinding(nil), findings...)
	applySupplyChainImpactSuppressions(indexed, suppressions, aliasMatchNow)
	for i, finding := range findings {
		want := EvaluateSupplyChainSuppression(finding, suppressions, aliasMatchNow)
		if indexed[i].Suppression != want {
			t.Fatalf("finding %d (%#v): indexed decision %#v != full-scan %#v", i, finding, indexed[i].Suppression, want)
		}
	}
}

// TestAliasMatchDSACounts pins ruling D1's upstream clause at bulletin
// scale: 20 findings filed under one DSA advisory id by distinct upstream
// CVEs (upstream ids never merge findings). A CVE-scoped suppression hides
// exactly the one finding whose cve_id it names; a DSA-scoped suppression
// hides all 20.
func TestAliasMatchDSACounts(t *testing.T) {
	t.Parallel()
	findings := make([]SupplyChainImpactFinding, 0, 20)
	for i := 0; i < 20; i++ {
		findings = append(findings, SupplyChainImpactFinding{
			CVEID:      fmt.Sprintf("CVE-2026-%04d", i),
			AdvisoryID: "DSA-9999-1",
		})
	}
	hidden := func(scope vulnerabilitySuppressionScope) int {
		s := aliasMatchSuppression(scope)
		n := 0
		for _, f := range findings {
			if EvaluateSupplyChainSuppression(f, []vulnerabilitySuppression{s}, aliasMatchNow).State == SupplyChainSuppressionStateNotAffected {
				n++
			}
		}
		return n
	}
	if got := hidden(vulnerabilitySuppressionScope{CVEID: "CVE-2026-0007"}); got != 1 {
		t.Fatalf("CVE-scoped hidden = %d, want exactly 1 of the DSA's 20 findings", got)
	}
	if got := hidden(vulnerabilitySuppressionScope{AdvisoryID: "DSA-9999-1"}); got != 20 {
		t.Fatalf("DSA-scoped hidden = %d, want all 20 findings", got)
	}
}

// TestAliasMatchNonMemberIDNeverMatches covers the negative space: an id
// reachable only outside the alias component (an upstream or related id the
// finding does not carry as cve_id, advisory_id, or member) never matches,
// and a fallback finding with no member set matches exact ids only.
func TestAliasMatchNonMemberIDNeverMatches(t *testing.T) {
	t.Parallel()
	grouped := SupplyChainImpactFinding{
		CVEID:                "CVE-1",
		AdvisoryID:           "GHSA-x",
		AdvisoryGroupMembers: []string{"CVE-1", "GHSA-x"},
	}
	for _, scope := range []vulnerabilitySuppressionScope{
		{CVEID: "CVE-UPSTREAM-ONLY"},
		{AdvisoryID: "GHSA-related-only"},
	} {
		s := aliasMatchSuppression(scope)
		if got := EvaluateSupplyChainSuppression(grouped, []vulnerabilitySuppression{s}, aliasMatchNow).State; got != SupplyChainSuppressionStateActive {
			t.Fatalf("scope %#v: State = %q, want active (non-member ids never widen matching)", scope, got)
		}
	}
	fallback := SupplyChainImpactFinding{CVEID: "CVE-1", AdvisoryID: "GHSA-x"}
	s := aliasMatchSuppression(vulnerabilitySuppressionScope{CVEID: "GHSA-unloaded-alias"})
	if got := EvaluateSupplyChainSuppression(fallback, []vulnerabilitySuppression{s}, aliasMatchNow).State; got != SupplyChainSuppressionStateActive {
		t.Fatalf("fallback finding: State = %q, want active (exact-only matching, no partial component)", got)
	}
}
