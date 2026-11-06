// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"
)

// TestSupplyChainImpactExploitedTierFixtures is the #9882 item-4.5 contract:
// an additive exploited tier alongside priority_score. Fixture list per the
// narrowed owner scope (CISA KEV + EPSS + NVD only; Exploit-DB #9881 and EU
// KEV #9960 are not planned): KEV, SSVC-active (reserved rung, fed by 4.1
// CVE-ADP SSVC when it lands; no collector emits it today), EPSS-only, none.
func TestSupplyChainImpactExploitedTierFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		knownExploited bool
		ssvc           string
		epssProb       string
		epssPct        string
		wantTier       string
	}{
		{name: "kev", knownExploited: true, wantTier: SupplyChainExploitedTierKEV},
		{name: "ssvc active reserved rung", ssvc: "active", wantTier: SupplyChainExploitedTierSSVCActive},
		{name: "epss only probability", epssProb: "0.03", wantTier: SupplyChainExploitedTierEPSS},
		{name: "epss only percentile", epssPct: "0.82", wantTier: SupplyChainExploitedTierEPSS},
		{name: "none", wantTier: SupplyChainExploitedTierNone},
		{name: "malformed epss is none", epssProb: "not-a-number", wantTier: SupplyChainExploitedTierNone},
		{name: "negative epss probability is none", epssProb: "-0.2", wantTier: SupplyChainExploitedTierNone},
		{name: "out-of-range epss probability is none", epssProb: "999", wantTier: SupplyChainExploitedTierNone},
		{name: "out-of-range epss percentile is none", epssPct: "1.5", wantTier: SupplyChainExploitedTierNone},
		{name: "boundary zero epss is epss", epssProb: "0", wantTier: SupplyChainExploitedTierEPSS},
		{name: "boundary one epss is epss", epssProb: "1", wantTier: SupplyChainExploitedTierEPSS},
		{name: "kev outranks ssvc input", knownExploited: true, ssvc: "active", wantTier: SupplyChainExploitedTierKEV},
		{name: "kev outranks epss", knownExploited: true, epssProb: "0.9", wantTier: SupplyChainExploitedTierKEV},
		{name: "unknown ssvc state is not active", ssvc: "none", epssProb: "0.03", wantTier: SupplyChainExploitedTierEPSS},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := supplyChainExploitedTier(tc.knownExploited, tc.ssvc, tc.epssProb, tc.epssPct); got != tc.wantTier {
				t.Fatalf("supplyChainExploitedTier() = %q, want %q", got, tc.wantTier)
			}
		})
	}
}

// TestSupplyChainImpactExploitedTierOrderHoldsRegardlessOfCVSS proves the
// #9882 rank promise at the finding level: a KEV-listed low-CVSS finding
// outranks an EPSS-only critical-CVSS finding, which outranks a critical
// finding with no exploit signal. The score derivation is untouched.
func TestSupplyChainImpactExploitedTierOrderHoldsRegardlessOfCVSS(t *testing.T) {
	t.Parallel()

	kevLow := withSupplyChainImpactPriority(SupplyChainImpactFinding{
		CVEID:          "CVE-2026-4101",
		Status:         SupplyChainImpactPossiblyAffected,
		CVSSScore:      4.5,
		SeverityVector: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N",
		KnownExploited: true,
		RepositoryID:   "repo://example/low",
	})
	epssCritical := withSupplyChainImpactPriority(SupplyChainImpactFinding{
		CVEID:           "CVE-2026-4102",
		Status:          SupplyChainImpactPossiblyAffected,
		CVSSScore:       9.8,
		SeverityVector:  "CVSS:3.1/AV:N/AC:K/PR:N/UI:N/S:U/C:H/I:H/A:H",
		EPSSProbability: "0.72",
		EPSSPercentile:  "0.97",
		RepositoryID:    "repo://example/critical-epss",
	})
	plainCritical := withSupplyChainImpactPriority(SupplyChainImpactFinding{
		CVEID:          "CVE-2026-4103",
		Status:         SupplyChainImpactPossiblyAffected,
		CVSSScore:      9.8,
		SeverityVector: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		RepositoryID:   "repo://example/critical-plain",
	})

	if kevLow.ExploitedTier != SupplyChainExploitedTierKEV {
		t.Fatalf("kevLow.ExploitedTier = %q, want %q", kevLow.ExploitedTier, SupplyChainExploitedTierKEV)
	}
	if epssCritical.ExploitedTier != SupplyChainExploitedTierEPSS {
		t.Fatalf("epssCritical.ExploitedTier = %q, want %q", epssCritical.ExploitedTier, SupplyChainExploitedTierEPSS)
	}
	if plainCritical.ExploitedTier != SupplyChainExploitedTierNone {
		t.Fatalf("plainCritical.ExploitedTier = %q, want %q", plainCritical.ExploitedTier, SupplyChainExploitedTierNone)
	}

	kevRank := supplyChainExploitedTierRank(kevLow.ExploitedTier)
	epssRank := supplyChainExploitedTierRank(epssCritical.ExploitedTier)
	noneRank := supplyChainExploitedTierRank(plainCritical.ExploitedTier)
	if kevRank <= epssRank || epssRank <= noneRank {
		t.Fatalf("tier ranks = kev %d, epss %d, none %d, want strictly descending", kevRank, epssRank, noneRank)
	}
	if kevLow.PriorityScore >= epssCritical.PriorityScore {
		t.Fatalf(
			"expected the score to still favor CVSS (kevLow score %d < epssCritical score %d), proving the tier is additive, not a score replacement",
			kevLow.PriorityScore,
			epssCritical.PriorityScore,
		)
	}
}

// TestSupplyChainImpactExploitedTierNeverPromotesImpactStatus is the #9882
// affected_exact guard: no exploited tier turns missing version evidence into
// affected_exact, and priority never rewrites status or missing evidence.
func TestSupplyChainImpactExploitedTierNeverPromotesImpactStatus(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		finding SupplyChainImpactFinding
	}{
		{
			name: "kev with missing version evidence",
			finding: SupplyChainImpactFinding{
				CVEID:           "CVE-2026-4201",
				Status:          SupplyChainImpactPossiblyAffected,
				CVSSScore:       9.8,
				SeverityVector:  "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
				KnownExploited:  true,
				MissingEvidence: []string{"installed_version"},
				RepositoryID:    "repo://example/kev-missing",
			},
		},
		{
			name: "epss with missing version evidence",
			finding: SupplyChainImpactFinding{
				CVEID:           "CVE-2026-4202",
				Status:          SupplyChainImpactPossiblyAffected,
				CVSSScore:       9.8,
				SeverityVector:  "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
				EPSSProbability: "0.9",
				EPSSPercentile:  "0.99",
				MissingEvidence: []string{"installed_version"},
				RepositoryID:    "repo://example/epss-missing",
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := withSupplyChainImpactPriority(tc.finding)
			if got.Status == SupplyChainImpactAffectedExact {
				t.Fatalf("Status = affected_exact after priority with tier %q; priority must never prove affected truth", got.ExploitedTier)
			}
			if got.Status != tc.finding.Status {
				t.Fatalf("Status = %q, want unchanged %q", got.Status, tc.finding.Status)
			}
			if len(got.MissingEvidence) != len(tc.finding.MissingEvidence) {
				t.Fatalf("MissingEvidence = %#v, want unchanged %#v", got.MissingEvidence, tc.finding.MissingEvidence)
			}
		})
	}
}
