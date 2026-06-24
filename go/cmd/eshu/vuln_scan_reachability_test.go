// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"
)

func TestBuildVulnScanReportPreservesReachabilityEnvelope(t *testing.T) {
	t.Parallel()

	report := buildVulnScanReport(vulnScanRepoResult{
		Command:        "vuln-scan repo",
		ReadinessState: "ready_with_findings",
		RepositoryID:   "repo://example/reachability",
		Count:          2,
		Findings: []map[string]any{
			{
				"finding_id":           "finding-go-1",
				"cve_id":               "CVE-2026-3901",
				"impact_status":        "affected_exact",
				"confidence":           "exact",
				"runtime_reachability": "not_called",
				"reachability": map[string]any{
					"state":             "not_called",
					"confidence":        "strong",
					"source":            "govulncheck",
					"evidence":          "not_called",
					"language_maturity": "implemented",
				},
			},
			{
				"finding_id":           "finding-cargo-1",
				"cve_id":               "CVE-2026-3902",
				"impact_status":        "affected_exact",
				"confidence":           "exact",
				"runtime_reachability": "package_manifest",
				"reachability": map[string]any{
					"state":             "reachable",
					"confidence":        "partial",
					"source":            "cargo",
					"evidence":          "cargo_dependency_path",
					"language_maturity": "partial",
				},
			},
		},
	}, time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))

	if got, want := report.Findings[0].Affected.Status, "affected_exact"; got != want {
		t.Fatalf("Affected.Status = %q, want %q", got, want)
	}
	if report.Findings[0].Reachability == nil {
		t.Fatal("Reachability = nil, want envelope")
	}
	if got, want := report.Findings[0].Reachability.State, "not_called"; got != want {
		t.Fatalf("Reachability.State = %q, want %q", got, want)
	}
	if got, want := report.Findings[0].Reachability.Source, "govulncheck"; got != want {
		t.Fatalf("Reachability.Source = %q, want %q", got, want)
	}
	if report.Findings[1].Reachability == nil {
		t.Fatal("Reachability = nil for second finding, want envelope")
	}
	if got, want := report.Findings[1].Reachability.State, "reachable"; got != want {
		t.Fatalf("Second Reachability.State = %q, want %q", got, want)
	}
	if got, want := report.Findings[1].Reachability.Source, "cargo"; got != want {
		t.Fatalf("Reachability.Source = %q, want %q", got, want)
	}
}

func TestBuildVulnScanReportPreservesJSTSReachabilityConfidence(t *testing.T) {
	t.Parallel()

	result := vulnScanRepoResult{
		Command:        "vuln-scan repo",
		RepositoryID:   "repo-js",
		ReadinessState: "ready_with_findings",
		Count:          1,
		Findings: []map[string]any{
			{
				"finding_id":           "finding-js-ts",
				"impact_status":        "affected_exact",
				"confidence":           "exact",
				"runtime_reachability": "package_api_call",
				"reachability": map[string]any{
					"state":             "reachable",
					"confidence":        "partial",
					"source":            "parser_js_ts",
					"evidence":          "package_api_call",
					"language_maturity": "partial",
				},
			},
		},
	}

	report := buildVulnScanReport(result, vulnScanNow())
	if got, want := report.Findings[0].Affected.Confidence, "exact"; got != want {
		t.Fatalf("Affected.Confidence = %q, want %q", got, want)
	}
	if report.Findings[0].Reachability == nil {
		t.Fatal("Reachability = nil, want envelope")
	}
	if got, want := report.Findings[0].Reachability.Source, "parser_js_ts"; got != want {
		t.Fatalf("Reachability.Source = %q, want %q", got, want)
	}
	if got, want := report.Findings[0].Reachability.Confidence, "partial"; got != want {
		t.Fatalf("Reachability.Confidence = %q, want %q", got, want)
	}
}
