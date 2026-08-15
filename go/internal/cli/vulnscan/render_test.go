// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderVulnScanRepoSummaryIncludesReadinessEvidenceAndRemediation(t *testing.T) {
	result := Result{
		ReadinessState: "unsupported",
		ScopeMode:      ScopeModeScoped,
		RepositoryID:   "repo-local",
		Count:          1,
		Readiness: map[string]any{
			"freshness":        "fresh",
			"missing_evidence": []any{"unsupported_targets"},
			"unsupported_targets": []any{
				map[string]any{"target_kind": "ecosystem", "reason": "matcher_not_available", "ecosystem": "swift", "count": float64(1)},
			},
			"counts": map[string]any{"evidence_facts_total": float64(82)},
		},
		Findings: []map[string]any{
			{
				"finding_id":        "finding-1",
				"cve_id":            "CVE-2026-0001",
				"package_name":      "ws",
				"impact_status":     "affected_exact",
				"fixed_version":     "8.17.1",
				"evidence_fact_ids": []any{"fact-package-1"},
			},
		},
	}
	out := &bytes.Buffer{}

	if err := RenderSummary(out, result); err != nil {
		t.Fatalf("RenderSummary() error = %v, want nil", err)
	}

	rendered := out.String()
	for _, want := range []string{
		"Readiness: state=unsupported freshness=fresh",
		"Missing evidence: unsupported_targets",
		"Unsupported targets: ecosystem/matcher_not_available count=1",
		"Evidence facts: 82",
		"finding-1 CVE-2026-0001 ws affected_exact fixed=8.17.1 evidence=fact-package-1",
	} {
		if !bytes.Contains([]byte(rendered), []byte(want)) {
			t.Fatalf("summary missing %q; output:\n%s", want, rendered)
		}
	}
}

// TestRenderSummaryFindingsLineMatchesTruncation pins the exact bytes of the
// findings line in both states. #6059 moved this renderer out of package main
// and rerouted its writes through writef, which turned one fmt.Fprint and one
// fmt.Fprintln into fmt.Fprintf calls. Those three produce the same bytes here,
// and this test is what says so — the CLI parity harness never reaches the text
// summary, because every case it can run without a live API exits before the
// summary is rendered.
func TestRenderSummaryFindingsLineMatchesTruncation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		truncated bool
		want      string
	}{
		{name: "complete", truncated: false, want: "Findings: 2\n"},
		{name: "truncated", truncated: true, want: "Findings: 2 (truncated)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			result := Result{
				ReadinessState: "ready_with_findings",
				ScopeMode:      ScopeModeScoped,
				RepositoryID:   "repo-local",
				Count:          2,
				Truncated:      tc.truncated,
			}
			if err := RenderSummary(out, result); err != nil {
				t.Fatalf("RenderSummary() error = %v, want nil", err)
			}
			if got := out.String(); !strings.Contains(got, tc.want) {
				t.Fatalf("summary missing %q; output:\n%s", tc.want, got)
			}
		})
	}
}
