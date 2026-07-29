// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestEvaluateSupplyChainSuppressionOnlineSelectionMatchesStableSort(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	finding := SupplyChainImpactFinding{
		CVEID:         "CVE-2026-00420",
		SubjectDigest: "sha256:placeholder",
		Environments:  []string{"stage"},
	}
	activeOlder := selectionTestSuppression(
		"active-older",
		facts.VulnerabilitySuppressionSourcePolicy,
		facts.VulnerabilitySuppressionJustificationAcceptedRisk,
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		"stage",
	)
	activeTieZ := selectionTestSuppression(
		"active-z",
		facts.VulnerabilitySuppressionSourcePolicy,
		facts.VulnerabilitySuppressionJustificationNotAffected,
		time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		"stage",
	)
	activeTieA := selectionTestSuppression(
		"active-a",
		facts.VulnerabilitySuppressionSourcePolicy,
		facts.VulnerabilitySuppressionJustificationFalsePositive,
		activeTieZ.AuthoredAt,
		"stage",
	)
	provider := selectionTestSuppression(
		"provider-newer",
		facts.VulnerabilitySuppressionSourceProviderDismissal,
		facts.VulnerabilitySuppressionJustificationProviderDismissed,
		time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
		"stage",
	)
	expired := selectionTestSuppression(
		"expired-newest",
		facts.VulnerabilitySuppressionSourcePolicy,
		facts.VulnerabilitySuppressionJustificationNotAffected,
		time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC),
		"stage",
	)
	expired.ExpiresAtPresent = true
	expired.ExpiresAt = time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	mismatch := selectionTestSuppression(
		"mismatch",
		facts.VulnerabilitySuppressionSourcePolicy,
		facts.VulnerabilitySuppressionJustificationNotAffected,
		time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC),
		"prod",
	)

	cases := []struct {
		name         string
		suppressions []vulnerabilitySuppression
	}{
		{"all precedence classes", []vulnerabilitySuppression{mismatch, provider, activeOlder, activeTieZ, expired, activeTieA}},
		{"provider without active", []vulnerabilitySuppression{mismatch, expired, provider}},
		{"expired without active or provider", []vulnerabilitySuppression{mismatch, expired}},
		{"scope mismatch only", []vulnerabilitySuppression{mismatch}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := EvaluateSupplyChainSuppression(finding, tc.suppressions, now)
			want := evaluateSupplyChainSuppressionStableSortReference(finding, tc.suppressions, now)
			if got != want {
				t.Fatalf("decision = %#v, want stable-sort reference %#v", got, want)
			}
		})
	}
}

func selectionTestSuppression(
	id string,
	source string,
	justification string,
	authoredAt time.Time,
	environment string,
) vulnerabilitySuppression {
	return vulnerabilitySuppression{
		SuppressionID: id,
		Source:        source,
		Justification: justification,
		Author:        "shared_token",
		AuthoredAt:    authoredAt,
		Scope: vulnerabilitySuppressionScope{
			CVEID:         "CVE-2026-00420",
			SubjectDigest: "sha256:placeholder",
			Environment:   environment,
		},
	}
}

func evaluateSupplyChainSuppressionStableSortReference(
	finding SupplyChainImpactFinding,
	suppressions []vulnerabilitySuppression,
	now time.Time,
) SupplyChainSuppressionDecision {
	var active, provider, expired, mismatched []vulnerabilitySuppression
	for _, suppression := range suppressions {
		if !suppressionAdjacent(finding, suppression) {
			continue
		}
		if !suppressionScopeMatchesFinding(finding, suppression) {
			mismatched = append(mismatched, suppression)
			continue
		}
		if suppressionIsExpired(suppression, now) {
			expired = append(expired, suppression)
			continue
		}
		if suppression.Source == facts.VulnerabilitySuppressionSourceProviderDismissal {
			provider = append(provider, suppression)
			continue
		}
		active = append(active, suppression)
	}

	if picked := stableSortReferencePick(active); picked != nil {
		return decisionFromActiveOperatorSuppression(*picked)
	}
	if picked := stableSortReferencePick(provider); picked != nil {
		return decisionFromProviderSuppression(*picked)
	}
	if picked := stableSortReferencePick(expired); picked != nil {
		return decisionFromExpiredSuppression(*picked)
	}
	if picked := stableSortReferencePick(mismatched); picked != nil {
		return decisionFromScopeMismatch(finding, *picked)
	}
	return SupplyChainSuppressionDecision{State: SupplyChainSuppressionStateActive}
}

func stableSortReferencePick(suppressions []vulnerabilitySuppression) *vulnerabilitySuppression {
	if len(suppressions) == 0 {
		return nil
	}
	sort.SliceStable(suppressions, func(i, j int) bool {
		if !suppressions[i].AuthoredAt.Equal(suppressions[j].AuthoredAt) {
			return suppressions[i].AuthoredAt.After(suppressions[j].AuthoredAt)
		}
		return suppressions[i].SuppressionID < suppressions[j].SuppressionID
	})
	picked := suppressions[0]
	return &picked
}
