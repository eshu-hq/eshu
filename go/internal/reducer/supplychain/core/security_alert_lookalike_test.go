// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// TestSecurityAlertImpactFindingKeepsLookalikeEcosystem pins the ecosystem a
// provider-alert impact finding records (#9584): it folds ASCII letters only,
// so an alert whose ecosystem is spelled with U+0130 does not report itself as
// pypi, while an ASCII case variant still folds to pypi. The builder is called
// directly because the full impact path later drops the lookalike's
// possibly-affected finding.
func TestSecurityAlertImpactFindingKeepsLookalikeEcosystem(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ ecosystem, want string }{
		{"PYP\u0130", "pyp\u0130"},
		{"PYPI", "pypi"},
	} {
		repoID := "repo://github/example/api"
		packageID := "pypi://pypi.org/simple/requests"
		envelopes := []facts.Envelope{
			securityAlertEnvelope("alert-lookalike-ecosystem", repoID, map[string]any{
				"provider":              "github_dependabot",
				"provider_alert_number": int64(901),
				"provider_state":        "open",
				"package_id":            packageID,
				"ecosystem":             tc.ecosystem,
				"package_name":          "requests",
				"manifest_path":         "requirements.txt",
				"ghsa_ids":              []string{"GHSA-lookalike-0001"},
				"cve_ids":               []string{"CVE-2026-49584"},
				"vulnerable_range":      "< 2.0.0",
				"patched_version":       "2.0.0",
			}),
			packageConsumptionCorrelationEnvelope("consume-lookalike-ecosystem", repoID, packageID, "requirements.txt"),
		}
		envelopes[1].Payload["dependency_range"] = "1.0.0"
		alerts, _, err := securityalert.ExtractProviderSecurityAlertsWithQuarantine(envelopes)
		if err != nil || len(alerts) != 1 {
			t.Fatalf("%q: alerts = %d, err = %v, want 1 alert", tc.ecosystem, len(alerts), err)
		}
		finding, ok := buildSecurityAlertImpactFinding(alerts[0], securityalert.ExtractSecurityAlertConsumptions(envelopes), nil, supplyChainImpactIndex{})
		if !ok {
			t.Fatalf("%q: buildSecurityAlertImpactFinding() ok = false", tc.ecosystem)
		}
		if finding.Ecosystem != tc.want {
			t.Errorf("%q: finding ecosystem = %q, want %q", tc.ecosystem, finding.Ecosystem, tc.want)
		}
	}
}
