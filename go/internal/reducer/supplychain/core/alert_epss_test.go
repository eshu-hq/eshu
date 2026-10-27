// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildSupplyChainImpactFindingsCarriesAlertEPSS pins the decimal scale
// in GitHub's documented alert example after collector normalization.
func TestBuildSupplyChainImpactFindingsCarriesAlertEPSS(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/example/api"
	packageID := "pypi://pypi.org/django"
	alert := securityAlertEnvelope("alert-epss", repoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(174),
		"provider_state":        "open",
		"package_id":            packageID,
		"ecosystem":             "pypi",
		"package_name":          "django",
		"manifest_path":         "requirements.txt",
		"ghsa_ids":              []string{"GHSA-rf4j-j272-fj86"},
		"cve_ids":               []string{"CVE-2018-6188"},
		"vulnerable_range":      ">= 2.0.0, < 2.0.2",
		"patched_version":       "2.0.2",
		"severity":              "high",
		"epss":                  map[string]any{"percentage": "0.00045", "percentile": "0.16001e0"},
	})
	consumption := packageConsumptionCorrelationEnvelope("consume-epss", repoID, packageID, "requirements.txt")
	consumption.Payload["dependency_range"] = "2.0.1"

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{alert, consumption})
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	if got := findings[0].EPSSProbability; got != "0.00045" {
		t.Errorf("EPSSProbability = %q, want 0.00045", got)
	}
	if got := findings[0].EPSSPercentile; got != "0.16001e0" {
		t.Errorf("EPSSPercentile = %q, want 0.16001e0", got)
	}
	// The writer serializes these same fields into the durable finding fact.
	payload := supplyChainImpactPayload(SupplyChainImpactWrite{}, findings[0])
	if payload["epss_probability"] != "0.00045" || payload["epss_percentile"] != "0.16001e0" {
		t.Errorf("persisted EPSS payload = %#v, want both selected values", payload)
	}

	alert.Payload["epss"] = map[string]any{"percentage": "0.00045", "percentile": "NaN"}
	findings = BuildSupplyChainImpactFindings([]facts.Envelope{alert, consumption})
	if len(findings) != 1 {
		t.Fatalf("len(findings) with invalid percentile = %d, want 1", len(findings))
	}
	if got := findings[0].EPSSProbability; got != "0.00045" {
		t.Errorf("EPSSProbability with invalid percentile = %q, want 0.00045", got)
	}
	if got := findings[0].EPSSPercentile; got != "" {
		t.Errorf("EPSSPercentile with invalid percentile = %q, want empty", got)
	}
}

func TestSecurityAlertEPSSValueRejectsInvalidScale(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "negative", raw: "-0.1"},
		{name: "above one", raw: "1.1"},
		{name: "not a number", raw: "NaN"},
		{name: "infinite", raw: "+Inf"},
		{name: "garbage", raw: "unknown"},
		{name: "hex float", raw: "0x1p-1"},
		{name: "separator", raw: "0.1_2"},
		{name: "zero", raw: "0", want: "0"},
		{name: "one", raw: "1", want: "1"},
		{name: "fraction", raw: " 0.00361 ", want: "0.00361"},
	} {
		caseValue := test
		t.Run(caseValue.name, func(t *testing.T) {
			t.Parallel()
			if got := securityAlertEPSSValue(caseValue.raw); got != caseValue.want {
				t.Errorf("securityAlertEPSSValue(%q) = %q, want %q", caseValue.raw, got, caseValue.want)
			}
		})
	}
}

func BenchmarkBuildSupplyChainImpactFindingsAlertEPSS(b *testing.B) {
	repoID := "repo://github/example/api"
	packageID := "npm://registry.npmjs.org/fast-uri"
	alert := securityAlertEnvelope("alert-epss-benchmark", repoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(175),
		"provider_state":        "open",
		"package_id":            packageID,
		"ecosystem":             "npm",
		"package_name":          "fast-uri",
		"manifest_path":         "package-lock.json",
		"ghsa_ids":              []string{"GHSA-provider-epss"},
		"cve_ids":               []string{"CVE-2023-31190"},
		"vulnerable_range":      "< 3.1.0",
		"patched_version":       "3.1.0",
		"severity":              "high",
		"epss":                  map[string]any{"percentage": "0.00361", "percentile": "0.29658"},
	})
	consumption := packageConsumptionCorrelationEnvelope("consume-epss-benchmark", repoID, packageID, "package-lock.json")
	consumption.Payload["dependency_range"] = "3.0.3"
	envelopes := []facts.Envelope{alert, consumption}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if findings := BuildSupplyChainImpactFindings(envelopes); len(findings) != 1 {
			b.Fatalf("len(findings) = %d, want 1", len(findings))
		}
	}
}
