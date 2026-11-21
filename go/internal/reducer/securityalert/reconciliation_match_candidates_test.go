// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import (
	"testing"
	"time"
)

// TestExactSecurityAlertConsumptionsAgreesWithMatch pins #11121's exported
// pair against MatchSecurityAlertConsumption: the same ambiguity bit, and
// SelectSecurityAlertConsumption over the exact candidates returns the same
// exact row, for one-repository, two-repository and stale-only inputs.
func TestExactSecurityAlertConsumptionsAgreesWithMatch(t *testing.T) {
	t.Parallel()
	updated := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	alert := ProviderSecurityAlert{updatedAtTime: updated}
	alert.RepositoryID, alert.RepositoryName, alert.PackageID, alert.ManifestPath = "repository:r_a", "a", "npm://registry.npmjs.org/x", "package-lock.json"
	row := func(id, repo, path string, at time.Time) SecurityAlertConsumption {
		return SecurityAlertConsumption{FactID: id, RepositoryID: repo, PackageID: alert.PackageID, RelativePath: path, ObservedAt: at}
	}
	later := updated.Add(time.Hour)
	for name, rows := range map[string][]SecurityAlertConsumption{
		"two versions one repository":  {row("b", "repository:r_a", "package-lock.json", later), row("a", "repository:r_a", "package-lock.json", later)},
		"exact rows span repositories": {row("a", "repository:r_a", "package-lock.json", later), {FactID: "c", RepositoryName: "a", PackageID: alert.PackageID, RelativePath: "package-lock.json"}},
		"stale only":                   {row("s", "repository:r_a", "other/package-lock.json", later)},
		"none":                         nil,
	} {
		exact, _, matchAmbiguous := MatchSecurityAlertConsumption(alert, rows)
		candidates, ambiguous := ExactSecurityAlertConsumptions(alert, rows)
		if ambiguous != matchAmbiguous {
			t.Fatalf("%s: ambiguous = %v, want %v", name, ambiguous, matchAmbiguous)
		}
		if name == "exact rows span repositories" && !ambiguous {
			t.Fatalf("%s: ambiguous = false, want true", name)
		}
		selected, _ := SelectSecurityAlertConsumption(candidates)
		if selected.FactID != exact.FactID {
			t.Fatalf("%s: selected %q, want %q", name, selected.FactID, exact.FactID)
		}
	}
}
