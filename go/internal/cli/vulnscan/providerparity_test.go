// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"bytes"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/vulnerabilityparity"
	"github.com/eshu-hq/eshu/go/internal/vulnerabilityparityproof"
)

func TestProviderParityEvidenceFromReadinessRequiresFreshEvidence(t *testing.T) {
	readiness := map[string]any{
		"evidence_sources": []any{
			map[string]any{
				"family":     "package.consumption",
				"fact_count": float64(1),
				"freshness":  "stale",
			},
			map[string]any{
				"family":     "vulnerability.advisory",
				"fact_count": float64(1),
				"freshness":  "fresh",
			},
		},
	}

	evidence := ParityEvidenceFromReadiness(readiness)
	if evidence.HasDependency {
		t.Fatalf("stale package.consumption evidence counted as fresh dependency evidence")
	}
	if !evidence.HasAdvisory {
		t.Fatalf("fresh vulnerability.advisory evidence was not counted")
	}
}

func TestRenderProviderParitySummaryReportsMismatchRowCount(t *testing.T) {
	data := map[string]any{
		"repositories_checked": 1,
		"provider_alert_count": 4,
		"eshu_finding_count":   3,
		"mismatch_classes": []vulnerabilityparityproof.ClassCount{
			{Class: string(vulnerabilityparity.ClassEshuOnly), Count: 1},
			{Class: string(vulnerabilityparity.ClassProviderOnly), Count: 2},
		},
	}
	out := &bytes.Buffer{}
	if err := RenderParitySummary(out, data); err != nil {
		t.Fatalf("RenderParitySummary returned error: %v", err)
	}
	if got, want := out.String(), "Provider parity: repositories=1 provider_alerts=4 eshu_findings=3 mismatches=3\n"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}
