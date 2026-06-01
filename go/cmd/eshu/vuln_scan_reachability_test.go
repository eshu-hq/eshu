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
		RepositoryID:   "repo://example/go",
		Count:          1,
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
}
