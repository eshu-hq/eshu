// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
)

func TestSecurityAlertFreshnessHintIgnoresProviderAlertOrder(t *testing.T) {
	t.Parallel()

	first := testDependabotAlert()
	second := testDependabotAlert()
	second.Number = 8
	second.Dependency.ManifestPath = "apps/web/package-lock.json"
	second.SecurityAdvisory.CVEID = "CVE-2026-0002"

	forward := securityAlertFreshnessHint(securityAlertFreshnessInput{
		alerts:       []securityalerts.GitHubDependabotAlert{first, second},
		pagesFetched: 2,
		truncated:    true,
	})
	reversed := securityAlertFreshnessHint(securityAlertFreshnessInput{
		alerts:       []securityalerts.GitHubDependabotAlert{second, first},
		pagesFetched: 2,
		truncated:    true,
	})
	if forward == "" {
		t.Fatal("securityAlertFreshnessHint() = blank, want digest")
	}
	if got, want := reversed, forward; got != want {
		t.Fatalf("securityAlertFreshnessHint() changed when alert order changed: got %q, want %q", got, want)
	}
}

func TestSecurityAlertFreshnessHintChangesWhenProviderTruthChanges(t *testing.T) {
	t.Parallel()

	openAlert := testDependabotAlert()
	fixedAlert := testDependabotAlert()
	fixedAlert.State = "fixed"
	fixedAlert.FixedAt = "2026-05-25T17:00:00Z"

	openHint := securityAlertFreshnessHint(securityAlertFreshnessInput{
		alerts: []securityalerts.GitHubDependabotAlert{openAlert},
	})
	fixedHint := securityAlertFreshnessHint(securityAlertFreshnessInput{
		alerts: []securityalerts.GitHubDependabotAlert{fixedAlert},
	})
	if openHint == "" || fixedHint == "" {
		t.Fatalf("securityAlertFreshnessHint() returned blank digest: open=%q fixed=%q", openHint, fixedHint)
	}
	if openHint == fixedHint {
		t.Fatalf("securityAlertFreshnessHint() did not change after provider state changed: %q", openHint)
	}
}

// TestSecurityAlertFreshnessHintSeparatesRepositories proves org alerts that
// share a provider alert number across repositories keep their repository in
// the digest (#8805): swapping which repository owns which package is a
// provider-truth change, not a reorder.
func TestSecurityAlertFreshnessHintSeparatesRepositories(t *testing.T) {
	t.Parallel()

	alert := func(repository string, packageName string) securityalerts.GitHubDependabotAlert {
		value := orgAlert(1, repository)
		value.Dependency.Package.Name = packageName
		return value
	}
	before := securityAlertFreshnessHint(securityAlertFreshnessInput{alerts: []securityalerts.GitHubDependabotAlert{
		alert("example-org/alpha-repo", "left-pad"),
		alert("example-org/beta-repo", "right-pad"),
	}})
	swapped := securityAlertFreshnessHint(securityAlertFreshnessInput{alerts: []securityalerts.GitHubDependabotAlert{
		alert("example-org/alpha-repo", "right-pad"),
		alert("example-org/beta-repo", "left-pad"),
	}})
	reordered := securityAlertFreshnessHint(securityAlertFreshnessInput{alerts: []securityalerts.GitHubDependabotAlert{
		alert("example-org/beta-repo", "right-pad"),
		alert("example-org/alpha-repo", "left-pad"),
	}})
	if before == swapped {
		t.Fatal("freshness hint unchanged after packages moved between repositories")
	}
	if before != reordered {
		t.Fatalf("freshness hint changed on reorder: %q vs %q", before, reordered)
	}
}
