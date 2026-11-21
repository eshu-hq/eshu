// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import "strings"

// MatchSecurityAlertConsumption selects, from consumptions, the evidence that
// best matches alert's repository and package identity. It returns three
// values: the exact match (same repository, same PackageID, and either no
// ManifestPath on the alert or a RelativePath equal to it — a zero-value
// SecurityAlertConsumption, i.e. empty FactID, means no exact match), the
// newest stale candidate (a same-repository/package consumption observed
// after alert's provider UpdatedAt whose RelativePath no longer matches —
// again zero-value when none), and a bool that is true when either the exact
// or (absent an exact match) the stale candidate set is ambiguous, meaning
// more than one candidate ties across different repositories and the caller
// must not pick one. SecurityAlertRepositoryScopeMatches decides repository
// identity for every candidate.
func MatchSecurityAlertConsumption(
	alert ProviderSecurityAlert,
	consumptions []SecurityAlertConsumption,
) (SecurityAlertConsumption, SecurityAlertConsumption, bool) {
	exactCandidates, staleCandidates := securityAlertConsumptionCandidates(alert, consumptions)
	exact, exactAmbiguous := selectSecurityAlertConsumption(exactCandidates)
	stale, staleAmbiguous := selectSecurityAlertConsumption(staleCandidates)
	return exact, stale, exactAmbiguous || (exact.FactID == "" && staleAmbiguous)
}

// ExactSecurityAlertConsumptions returns every exact candidate
// MatchSecurityAlertConsumption chooses among (same repository, same
// PackageID, and the alert's ManifestPath or none), in input order, plus that
// function's ambiguity bit for the same input. The impact path reads it to
// classify each installed version once (#11121): a lockfile can hold several
// versions of the alerted package, and one pick would judge them all on one
// row. SelectSecurityAlertConsumption picks within a subset by the same rule.
func ExactSecurityAlertConsumptions(
	alert ProviderSecurityAlert,
	consumptions []SecurityAlertConsumption,
) ([]SecurityAlertConsumption, bool) {
	exactCandidates, staleCandidates := securityAlertConsumptionCandidates(alert, consumptions)
	exact, exactAmbiguous := selectSecurityAlertConsumption(exactCandidates)
	_, staleAmbiguous := selectSecurityAlertConsumption(staleCandidates)
	return exactCandidates, exactAmbiguous || (exact.FactID == "" && staleAmbiguous)
}

// SelectSecurityAlertConsumption picks one row from candidates by the rule
// MatchSecurityAlertConsumption uses: the newest ObservedAt, then the lower
// FactID. It reports ambiguity when the candidates span more than one
// repository, and then returns the zero value.
func SelectSecurityAlertConsumption(candidates []SecurityAlertConsumption) (SecurityAlertConsumption, bool) {
	return selectSecurityAlertConsumption(candidates)
}

// securityAlertConsumptionCandidates splits consumptions into the exact and
// stale candidate sets for alert; both matchers above read it, so they cannot
// disagree about which rows are candidates.
func securityAlertConsumptionCandidates(
	alert ProviderSecurityAlert,
	consumptions []SecurityAlertConsumption,
) (exactCandidates, staleCandidates []SecurityAlertConsumption) {
	for _, consumption := range consumptions {
		if !SecurityAlertRepositoryScopeMatches(alert, consumption) || consumption.PackageID != alert.PackageID {
			continue
		}
		if alert.ManifestPath == "" || consumption.RelativePath == alert.ManifestPath {
			exactCandidates = append(exactCandidates, consumption)
			continue
		}
		if !alert.updatedAtTime.IsZero() && consumption.ObservedAt.After(alert.updatedAtTime) {
			staleCandidates = append(staleCandidates, consumption)
		}
	}
	return exactCandidates, staleCandidates
}

// SecurityAlertRepositoryScopeMatches reports whether alert and consumption
// identify the same repository. It prefers an exact, non-empty RepositoryID
// match on both sides; only when either RepositoryID is blank does it fall
// back to comparing repository names case-insensitively
// (normalizeSecurityAlertRepositoryName). A blank RepositoryID on both sides
// is never treated as a match — the fallback still requires a non-empty
// normalized name — so two alerts/consumptions that both lack identity never
// collide.
func SecurityAlertRepositoryScopeMatches(
	alert ProviderSecurityAlert,
	consumption SecurityAlertConsumption,
) bool {
	alertRepositoryID := strings.TrimSpace(alert.RepositoryID)
	consumptionRepositoryID := strings.TrimSpace(consumption.RepositoryID)
	if alertRepositoryID != "" && consumptionRepositoryID != "" && alertRepositoryID == consumptionRepositoryID {
		return true
	}
	alertRepositoryName := normalizeSecurityAlertRepositoryName(alert.RepositoryName)
	consumptionRepositoryName := normalizeSecurityAlertRepositoryName(consumption.RepositoryName)
	return alertRepositoryName != "" && alertRepositoryName == consumptionRepositoryName
}

func selectSecurityAlertConsumption(candidates []SecurityAlertConsumption) (SecurityAlertConsumption, bool) {
	if len(candidates) == 0 {
		return SecurityAlertConsumption{}, false
	}
	selected := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.RepositoryID != selected.RepositoryID {
			return SecurityAlertConsumption{}, true
		}
		if securityAlertConsumptionIsNewerStaleCandidate(candidate, selected) {
			selected = candidate
		}
	}
	return selected, false
}

func securityAlertConsumptionIsNewerStaleCandidate(
	candidate SecurityAlertConsumption,
	current SecurityAlertConsumption,
) bool {
	if current.FactID == "" {
		return true
	}
	if candidate.ObservedAt.After(current.ObservedAt) {
		return true
	}
	if candidate.ObservedAt.Equal(current.ObservedAt) {
		return candidate.FactID < current.FactID
	}
	return false
}

func normalizeSecurityAlertRepositoryName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func securityAlertRepositoryNameFromID(repositoryID string) string {
	trimmed := strings.TrimSpace(repositoryID)
	if trimmed == "" {
		return ""
	}
	if slash := strings.LastIndex(trimmed, "/"); slash >= 0 && slash+1 < len(trimmed) {
		return strings.TrimSpace(trimmed[slash+1:])
	}
	return ""
}

func matchSecurityAlertImpact(alert ProviderSecurityAlert, impacts []SecurityAlertImpact) SecurityAlertImpact {
	for _, impact := range impacts {
		if impact.RepositoryID != alert.RepositoryID || impact.PackageID != alert.PackageID {
			continue
		}
		if SecurityAlertIDMatches(alert.CVEIDs, impact.CVEID) ||
			SecurityAlertIDMatches(alert.GHSAIDs, impact.AdvisoryID) {
			return impact
		}
	}
	return SecurityAlertImpact{}
}

// SecurityAlertIDMatches reports whether want (trimmed) case-insensitively
// equals any entry of values (each also trimmed before comparison). A blank
// want always returns false, so an alert or impact finding with no CVE/GHSA
// identifier never matches by omission. Used to compare a
// ProviderSecurityAlert's CVEIDs/GHSAIDs against a SecurityAlertImpact's
// CVEID/AdvisoryID.
func SecurityAlertIDMatches(values []string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}
