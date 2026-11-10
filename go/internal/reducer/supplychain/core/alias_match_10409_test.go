// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
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
