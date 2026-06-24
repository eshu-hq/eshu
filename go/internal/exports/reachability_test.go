// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exports

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestSARIFExporterIncludesReachabilityEnvelopeProperties(t *testing.T) {
	t.Parallel()

	snapshot := Snapshot{
		Scope:       Scope{Kind: ScopeKindRepository, RepositoryID: "repo-main"},
		GeneratedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		Findings: []Finding{
			{
				FindingID:    "finding-govulncheck-reachable",
				AdvisoryID:   "GHSA-reach-0001",
				PackageID:    "pkg-golang-example",
				RepositoryID: "repo-main",
				ImpactStatus: "affected_exact",
				Reachability: &Reachability{
					State:      "reachable",
					Confidence: "strong",
					Source:     "govulncheck",
					Evidence:   "symbol_reachable",
				},
			},
			{
				FindingID:    "finding-nuget-reachable",
				AdvisoryID:   "GHSA-reach-0002",
				PackageID:    "pkg:nuget/newtonsoft.json",
				RepositoryID: "repo-main",
				ImpactStatus: "affected_exact",
				Reachability: &Reachability{
					State:      "reachable",
					Confidence: "partial",
					Source:     "nuget",
					Evidence:   "nuget_dependency_path",
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := NewSARIFExporter().Export(&buf, snapshot, Options{}); err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		`"eshu.reachabilityState": "reachable"`,
		`"eshu.reachabilityConfidence": "strong"`,
		`"eshu.reachabilitySource": "govulncheck"`,
		`"eshu.reachabilityConfidence": "partial"`,
		`"eshu.reachabilitySource": "nuget"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("SARIF output missing %s:\n%s", want, out)
		}
	}
}
