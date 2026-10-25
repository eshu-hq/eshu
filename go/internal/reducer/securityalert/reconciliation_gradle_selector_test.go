// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// A Gradle selector in a build.gradle manifest is not an installed version
// (issue #8988). The alert must carry no observed version and must report the
// version as missing, the same as any other range, not as malformed.
func TestBuildSecurityAlertReconciliationsTreatsGradleSelectorsAsRanges(t *testing.T) {
	t.Parallel()
	repoID := "repo://github/example/app"
	packageID := "maven://repo.maven.apache.org/maven2/org.example:lib"
	for _, selector := range []string{"1.+", "+", "latest.release", "Latest.Integration", "1.0!!", "[1.0,2.0)!!1.5", "[1.0,2.0)"} {
		t.Run(selector, func(t *testing.T) {
			t.Parallel()
			alert := securityAlertEnvelope("alert-"+selector, repoID, map[string]any{
				"provider":              "github_dependabot",
				"provider_alert_number": int64(45),
				"provider_state":        "open",
				"package_id":            packageID,
				"ecosystem":             "maven",
				"package_name":          "org.example:lib",
				"manifest_path":         "build.gradle",
				"cve_ids":               []string{"CVE-2026-0045"},
			})
			consumption := packageConsumptionCorrelationEnvelope("consume-"+selector, repoID, packageID, "build.gradle")
			consumption.Payload["dependency_range"] = selector

			decisions := BuildSecurityAlertReconciliations([]facts.Envelope{alert, consumption}, nil)
			if len(decisions) != 1 {
				t.Fatalf("len(decisions) = %d, want 1", len(decisions))
			}
			if got := decisions[0].ObservedVersion; got != "" {
				t.Fatalf("ObservedVersion = %q, want blank for selector %q", got, selector)
			}
			assertContainsString(t, decisions[0].PackageMissingEvidence, securityAlertInstalledVersionMissing)
		})
	}
}
