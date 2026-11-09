// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// carrySuppression builds a live operator suppression naming the given
// anchors for the carry/orphan tally tests.
func carrySuppression(id, cveID, advisoryID string, authored time.Time) vulnerabilitySuppression {
	return vulnerabilitySuppression{
		SuppressionID: id,
		Source:        chain.VulnerabilitySuppressionSourcePolicy,
		Justification: chain.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    authored,
		Scope:         vulnerabilitySuppressionScope{CVEID: cveID, AdvisoryID: advisoryID},
	}
}

func carryFinding(cveID string, members []string, suppression SupplyChainSuppressionDecision) SupplyChainImpactFinding {
	return SupplyChainImpactFinding{
		CVEID:                cveID,
		AdvisoryID:           cveID,
		AdvisoryGroupMembers: members,
		Suppression:          suppression,
	}
}

func hidingDecision(id string) SupplyChainSuppressionDecision {
	return SupplyChainSuppressionDecision{State: SupplyChainSuppressionStateNotAffected, SuppressionID: id}
}

// TestCountSuppressionCarryViaMemberAnchor is the #10408 L3 failing
// regression for the carried-coverage sub-signal: a new finding hidden by a
// suppression through a member anchor that is not its canonical key counts
// as carried, while a suppression naming the canonical key does not.
func TestCountSuppressionCarryViaMemberAnchor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	authored := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	member := carrySuppression("suppression-member", "CVE-2026-0402", "", authored)
	canonical := carrySuppression("suppression-canonical", "CVE-2026-0401", "", authored)
	findings := []SupplyChainImpactFinding{
		carryFinding("CVE-2026-0401", []string{"CVE-2026-0401", "CVE-2026-0402"}, hidingDecision("suppression-member")),
		carryFinding("CVE-2026-0401", []string{"CVE-2026-0401", "CVE-2026-0402"}, hidingDecision("suppression-canonical")),
	}
	got := countSuppressionCarry([]vulnerabilitySuppression{member, canonical}, findings, []string{"CVE-2026-0401", "CVE-2026-0402"}, now)
	if got.carried != 1 {
		t.Fatalf("carried = %d, want 1 (only the member-anchor hide counts)", got.carried)
	}
	if got.orphaned != 0 {
		t.Fatalf("orphaned = %d, want 0 (both suppressions still cite a fragment)", got.orphaned)
	}
}

// TestCountSuppressionOrphanedBySplit is the #10408 L3 failing regression
// for the split-orphan sub-signal: a live suppression naming a re-key
// touched id that no new finding cites anymore counts as orphaned, while an
// expired suppression, a suppression still cited through scope mismatch, and
// a suppression naming an untouched id do not.
func TestCountSuppressionOrphanedBySplit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	authored := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	orphan := carrySuppression("suppression-orphan", "", "GHSA-0402-cccc", authored)
	mismatch := carrySuppression("suppression-mismatch", "", "GHSA-0402-cccc", authored)
	expired := carrySuppression("suppression-expired", "", "GHSA-0402-cccc", authored)
	expired.ExpiresAtPresent = true
	expired.ExpiresAt = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	unrelated := carrySuppression("suppression-unrelated", "", "GHSA-9999-zzzz", authored)
	suppressions := []vulnerabilitySuppression{orphan, mismatch, expired, unrelated}
	findings := []SupplyChainImpactFinding{
		carryFinding("CVE-2026-0300", []string{"CVE-2026-0300", "GHSA-0300-aaaa"}, SupplyChainSuppressionDecision{
			State:         SupplyChainSuppressionStateScopeMismatch,
			SuppressionID: "suppression-mismatch",
		}),
		carryFinding("CVE-2026-0301", []string{"CVE-2026-0301"}, SupplyChainSuppressionDecision{
			State:         SupplyChainSuppressionStateExpired,
			SuppressionID: "suppression-expired",
		}),
	}
	touched := []string{"CVE-2026-0300", "GHSA-0300-aaaa", "GHSA-0402-cccc", "CVE-2026-0301"}
	got := countSuppressionCarry(suppressions, findings, touched, now)
	if got.orphaned != 1 {
		t.Fatalf("orphaned = %d, want 1 (only the live uncited suppression naming a touched dead id)", got.orphaned)
	}
	if got.carried != 0 {
		t.Fatalf("carried = %d, want 0 (no hiding decision cites a suppression)", got.carried)
	}
}

// TestCountSuppressionOrphanedNeedsRekey pins the re-key gate: the same
// uncited suppression on a pass that changed no group identity is ordinary
// coverage loss, not a split orphan, and must not count.
func TestCountSuppressionOrphanedNeedsRekey(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	authored := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	suppressions := []vulnerabilitySuppression{carrySuppression("suppression-orphan", "", "GHSA-0402-cccc", authored)}
	findings := []SupplyChainImpactFinding{
		carryFinding("CVE-2026-0300", []string{"CVE-2026-0300"}, SupplyChainSuppressionDecision{State: SupplyChainSuppressionStateActive}),
	}
	got := countSuppressionCarry(suppressions, findings, nil, now)
	if got.orphaned != 0 {
		t.Fatalf("orphaned = %d, want 0 (no group change, no split orphan)", got.orphaned)
	}
}
