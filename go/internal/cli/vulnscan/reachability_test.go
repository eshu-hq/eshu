// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"testing"
	"time"
)

func TestBuildVulnScanReportPreservesReachabilityEnvelope(t *testing.T) {
	t.Parallel()

	report := BuildReport(Result{
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

	result := Result{
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

	report := BuildReport(result, Now())
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

// TestBuildVulnScanReportPreservesScanProvenance pins the #10029 console
// surface: a Go finding whose reachability carries govulncheck scan
// provenance shows scan level, scan mode, scanner version, and trace count,
// while a provenance-absent finding keeps the prior shape with none of the
// keys.
func TestBuildVulnScanReportPreservesScanProvenance(t *testing.T) {
	t.Parallel()

	report := BuildReport(Result{
		Command:        "vuln-scan repo",
		ReadinessState: "ready_with_findings",
		RepositoryID:   "repo://example/scan-provenance",
		Count:          2,
		Findings: []map[string]any{
			{
				"finding_id":           "finding-go-provenance",
				"cve_id":               "CVE-2026-3903",
				"impact_status":        "affected_exact",
				"confidence":           "exact",
				"runtime_reachability": "not_called",
				"reachability": map[string]any{
					"state":             "not_called",
					"confidence":        "strong",
					"source":            "govulncheck",
					"evidence":          "not_called",
					"reason":            "govulncheck proved the vulnerable surface is not called from any entry point",
					"language_maturity": "implemented",
					"scan_level":        "symbol",
					"scan_mode":         "source",
					"scanner_version":   "v1.9.0",
					"trace_count":       float64(3),
				},
			},
			{
				"finding_id":           "finding-go-legacy",
				"cve_id":               "CVE-2026-3904",
				"impact_status":        "affected_exact",
				"confidence":           "exact",
				"runtime_reachability": "module_only",
				"reachability": map[string]any{
					"state":             "unknown",
					"confidence":        "unknown",
					"source":            "govulncheck",
					"evidence":          "module_only",
					"language_maturity": "implemented",
				},
			},
		},
	}, time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))

	got := report.Findings[0].Reachability
	if got == nil {
		t.Fatal("Reachability = nil, want envelope")
	}
	if got.ScanLevel != "symbol" {
		t.Fatalf("Reachability.ScanLevel = %q, want %q", got.ScanLevel, "symbol")
	}
	if got.ScanMode != "source" {
		t.Fatalf("Reachability.ScanMode = %q, want %q", got.ScanMode, "source")
	}
	if got.ScannerVersion != "v1.9.0" {
		t.Fatalf("Reachability.ScannerVersion = %q, want %q", got.ScannerVersion, "v1.9.0")
	}
	if got.TraceCount == nil || *got.TraceCount != 3 {
		t.Fatalf("Reachability.TraceCount = %v, want 3", got.TraceCount)
	}

	legacy := report.Findings[1].Reachability
	if legacy == nil {
		t.Fatal("legacy Reachability = nil, want envelope")
	}
	if legacy.ScanLevel != "" || legacy.ScanMode != "" || legacy.ScannerVersion != "" || legacy.TraceCount != nil {
		t.Fatalf("legacy Reachability = %+v, want no provenance keys", legacy)
	}
}
