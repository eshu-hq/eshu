// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

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

	decisions := BuildSecurityAlertReconciliations([]facts.Envelope{alert}, nil)

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
		PackageID       string
		DependencyRange string
		wantObserved    string
		wantMissing     string
	}{
		{
			name:            "range_only_manifest",
			PackageID:       "npm://registry.npmjs.org/range-only",
			DependencyRange: "^1.2.0",
			wantMissing:     "installed package version missing",
		},
		{
			name:            "malformed_version",
			PackageID:       "npm://registry.npmjs.org/malformed-version",
			DependencyRange: "not-a-version",
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
				"package_id":            tc.PackageID,
				"ecosystem":             "npm",
				"package_name":          strings.TrimPrefix(tc.PackageID, "npm://registry.npmjs.org/"),
				"manifest_path":         "package-lock.json",
				"cve_ids":               []string{"CVE-2026-0044"},
			})
			consumption := packageConsumptionCorrelationEnvelope("consume-"+tc.name, repoID, tc.PackageID, "package-lock.json")
			consumption.Payload["dependency_range"] = tc.DependencyRange

			decisions := BuildSecurityAlertReconciliations([]facts.Envelope{alert, consumption}, nil)

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
			if got, want := decision.RequestedRange, tc.DependencyRange; got != want {
				t.Fatalf("RequestedRange = %q, want %q", got, want)
			}
			assertContainsString(t, decision.PackageMissingEvidence, tc.wantMissing)
		})
	}
}

// assertContainsString is declared locally rather than imported from the
// reducer root's copy (supply_chain_impact_version_match_helpers_test.go): Go
// test files never export across packages, and this seven-line
// case-insensitive membership check has no reducer-root dependency (issue
// #6061).
func assertContainsString(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return
		}
	}
	t.Fatalf("%#v does not contain %q", values, want)
}
