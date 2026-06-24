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
