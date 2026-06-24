// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSecurityAlertReconciliationsDoesNotCopyProviderVersionIntoObservedVersion(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/eshu-hq/eshu"
	packageID := "npm://registry.npmjs.org/provider-only"
	alert := securityAlertEnvelope("alert-provider-only-version", repoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(43),
		"provider_state":        "open",
		"package_id":            packageID,
		"ecosystem":             "npm",
		"package_name":          "provider-only",
		"manifest_path":         "package-lock.json",
		"cve_ids":               []string{"CVE-2026-0043"},
		"installed_version":     "9.9.9",
		"observed_version":      "9.9.9",
	})

	decisions := BuildSecurityAlertReconciliations([]facts.Envelope{alert})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.Status, SecurityAlertReconciliationProviderOnly; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if decision.ObservedVersion != "" {
		t.Fatalf("ObservedVersion = %q, want blank because provider payload is not Eshu package evidence", decision.ObservedVersion)
	}
	if decision.DependencyEvidenceID != "" {
		t.Fatalf("DependencyEvidenceID = %q, want blank for provider-only alert", decision.DependencyEvidenceID)
	}
}

func TestBuildSecurityAlertReconciliationsReportsMissingAndMalformedObservedVersions(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/eshu-hq/eshu"
	tests := []struct {
		name            string
		packageID       string
		dependencyRange string
		wantObserved    string
		wantMissing     string
	}{
		{
			name:            "range_only_manifest",
			packageID:       "npm://registry.npmjs.org/range-only",
			dependencyRange: "^1.2.0",
			wantMissing:     "installed package version missing",
		},
		{
			name:            "malformed_version",
			packageID:       "npm://registry.npmjs.org/malformed-version",
			dependencyRange: "not-a-version",
			wantObserved:    "not-a-version",
			wantMissing:     "installed package version malformed",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			alert := securityAlertEnvelope("alert-"+tc.name, repoID, map[string]any{
				"provider":              "github_dependabot",
				"provider_alert_number": int64(44),
				"provider_state":        "open",
				"package_id":            tc.packageID,
				"ecosystem":             "npm",
				"package_name":          strings.TrimPrefix(tc.packageID, "npm://registry.npmjs.org/"),
				"manifest_path":         "package-lock.json",
				"cve_ids":               []string{"CVE-2026-0044"},
			})
			consumption := packageConsumptionCorrelationEnvelope("consume-"+tc.name, repoID, tc.packageID, "package-lock.json")
			consumption.Payload["dependency_range"] = tc.dependencyRange

			decisions := BuildSecurityAlertReconciliations([]facts.Envelope{alert, consumption})

			if got, want := len(decisions), 1; got != want {
				t.Fatalf("len(decisions) = %d, want %d", got, want)
			}
			decision := decisions[0]
			if got, want := decision.Status, SecurityAlertReconciliationUnmatched; got != want {
				t.Fatalf("Status = %q, want %q", got, want)
			}
			if got, want := decision.ObservedVersion, tc.wantObserved; got != want {
				t.Fatalf("ObservedVersion = %q, want %q", got, want)
			}
			if got, want := decision.RequestedRange, tc.dependencyRange; got != want {
				t.Fatalf("RequestedRange = %q, want %q", got, want)
			}
			assertContainsString(t, decision.PackageMissingEvidence, tc.wantMissing)
		})
	}
}
