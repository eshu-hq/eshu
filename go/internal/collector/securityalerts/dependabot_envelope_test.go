// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalerts

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestGitHubDependabotAlertEnvelopePreservesRepositoryAlertFields(t *testing.T) {
	t.Parallel()

	ctx := EnvelopeContext{
		ScopeID:             "repo://github/eshu-hq/eshu",
		GenerationID:        "generation-1",
		CollectorInstanceID: "security-alert-fixture",
		ObservedAt:          time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC),
		SourceURI:           "https://api.github.com/repos/eshu-hq/eshu/dependabot/alerts?token=redacted",
	}
	envelope, err := NewGitHubDependabotAlertEnvelope(ctx, GitHubDependabotAlert{
		Number:    42,
		State:     "open",
		HTMLURL:   "https://github.com/eshu-hq/eshu/security/dependabot/42?access_token=redacted&plain=1",
		CreatedAt: "2026-05-20T12:00:00Z",
		UpdatedAt: "2026-05-23T10:15:00Z",
		Dependency: GitHubDependabotDependency{
			ManifestPath: "package-lock.json",
			Scope:        "runtime",
			Package: GitHubDependabotPackage{
				Ecosystem: "npm",
				Name:      "@scope/left-pad",
			},
			Relationship: "direct",
		},
		SecurityAdvisory: GitHubDependabotSecurityAdvisory{
			GHSAID:      "GHSA-abcd-1234",
			CVEID:       "CVE-2026-0001",
			Summary:     "Synthetic advisory",
			Description: "Synthetic advisory description",
			Severity:    "critical",
			CVSS: GitHubDependabotCVSS{
				Score:  9.8,
				Vector: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
			},
			EPSS: GitHubDependabotEPSS{
				Percentage: "0.721",
				Percentile: "0.981",
			},
			CWEs: []GitHubDependabotCWE{
				{CWEID: "CWE-79", Name: "Cross-site Scripting"},
				{CWEID: "CWE-79", Name: "Cross-site Scripting"},
			},
		},
		SecurityVulnerability: GitHubDependabotSecurityVulnerability{
			VulnerableVersionRange: "<1.2.3",
			FirstPatchedVersion:    GitHubDependabotVersion{Identifier: "1.2.3"},
			Package: GitHubDependabotPackage{
				Ecosystem: "npm",
				Name:      "@scope/left-pad",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewGitHubDependabotAlertEnvelope() error = %v, want nil", err)
	}

	if got, want := envelope.FactKind, facts.SecurityAlertRepositoryAlertFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := envelope.SchemaVersion, facts.SecurityAlertSchemaVersionV1; got != want {
		t.Fatalf("SchemaVersion = %q, want %q", got, want)
	}
	if got, want := envelope.CollectorKind, CollectorKind; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := envelope.SourceConfidence, facts.SourceConfidenceReported; got != want {
		t.Fatalf("SourceConfidence = %q, want %q", got, want)
	}
	if got, want := envelope.SourceRef.SourceURI, "https://api.github.com/repos/eshu-hq/eshu/dependabot/alerts"; got != want {
		t.Fatalf("SourceRef.SourceURI = %q, want %q", got, want)
	}

	wantPayload := map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(42),
		"provider_state":        "open",
		"repository_id":         "repo://github/eshu-hq/eshu",
		"ecosystem":             "npm",
		"package_name":          "@scope/left-pad",
		"package_id":            "npm://registry.npmjs.org/@scope/left-pad",
		"manifest_path":         "package-lock.json",
		"dependency_scope":      "runtime",
		"relationship":          "direct",
		"vulnerable_range":      "<1.2.3",
		"patched_version":       "1.2.3",
		"severity":              "critical",
		"summary":               "Synthetic advisory",
		"source_url":            "https://github.com/eshu-hq/eshu/security/dependabot/42?plain=1",
		"created_at":            "2026-05-20T12:00:00Z",
		"updated_at":            "2026-05-23T10:15:00Z",
	}
	for key, want := range wantPayload {
		if got := envelope.Payload[key]; got != want {
			t.Fatalf("Payload[%q] = %#v, want %#v", key, got, want)
		}
	}
	if got, want := envelope.Payload["ghsa_ids"], []string{"GHSA-abcd-1234"}; !stringSlicesEqual(got, want) {
		t.Fatalf("Payload[ghsa_ids] = %#v, want %#v", got, want)
	}
	if got, want := envelope.Payload["cve_ids"], []string{"CVE-2026-0001"}; !stringSlicesEqual(got, want) {
		t.Fatalf("Payload[cve_ids] = %#v, want %#v", got, want)
	}
	cwes, ok := envelope.Payload["cwes"].([]map[string]string)
	if !ok || len(cwes) != 1 || cwes[0]["cwe_id"] != "CWE-79" {
		t.Fatalf("Payload[cwes] = %#v, want one CWE-79 row", envelope.Payload["cwes"])
	}
}

func TestGitHubDependabotClientRequiresTokenAndAllowlistedRepository(t *testing.T) {
	t.Parallel()

	client := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:              "https://api.github.test",
		Token:                "token-value",
		AllowedRepositories:  []string{"eshu-hq/eshu"},
		RepositoryAlertLimit: 100,
	})
	if _, err := client.ListRepositoryAlerts(t.Context(), "other-org/private-repo"); err == nil {
		t.Fatal("ListRepositoryAlerts() error = nil, want allowlist error")
	}

	missingToken := NewGitHubDependabotClient(GitHubDependabotClientConfig{
		BaseURL:             "https://api.github.test",
		AllowedRepositories: []string{"eshu-hq/eshu"},
	})
	if _, err := missingToken.ListRepositoryAlerts(t.Context(), "eshu-hq/eshu"); err == nil {
		t.Fatal("ListRepositoryAlerts() error = nil, want missing token error")
	}
}

func stringSlicesEqual(got any, want []string) bool {
	values, ok := got.([]string)
	if !ok || len(values) != len(want) {
		return false
	}
	for i := range values {
		if values[i] != want[i] {
			return false
		}
	}
	return true
}
