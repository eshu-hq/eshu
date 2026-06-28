// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestBuildReportSummaries(t *testing.T) {
	c := reconcileFixture()
	rep := BuildReport(c, false)

	if rep.SchemaVersion == "" {
		t.Error("report missing schema version")
	}
	if rep.Blocking {
		t.Error("blocking should be false")
	}

	// Totals: 4 surfaces — 1 covered, 1 unresolved, 1 uncovered, 1 exempt.
	if rep.Totals.Total != 4 || rep.Totals.Covered != 1 || rep.Totals.Unresolved != 1 ||
		rep.Totals.Uncovered != 1 || rep.Totals.Exempt != 1 {
		t.Errorf("totals = %+v", rep.Totals)
	}
	// Satisfied = covered + exempt = 2 of 4 = 50%.
	if rep.Totals.PercentSatisfied != 50.0 {
		t.Errorf("percent satisfied = %v, want 50", rep.Totals.PercentSatisfied)
	}
	if len(rep.Stale) != 2 {
		t.Errorf("stale = %v", rep.Stale)
	}

	// Per-registry summaries present and sorted by registry.
	if len(rep.Summaries) == 0 {
		t.Fatal("no per-registry summaries")
	}
	for i := 1; i < len(rep.Summaries); i++ {
		if rep.Summaries[i-1].Registry > rep.Summaries[i].Registry {
			t.Errorf("summaries not sorted by registry at %d", i)
		}
	}
}

func TestBuildReportSurfaceRefsAndGapList(t *testing.T) {
	c := reconcileFixture()
	rep := BuildReport(c, true)
	byKey := map[string]SurfaceReport{}
	for _, s := range rep.Surfaces {
		byKey[s.Key] = s
	}
	if got := byKey["collector:aws"]; got.Ref != "present-ref" || got.Status != StatusCovered {
		t.Errorf("collector:aws surface report = %+v", got)
	}
	// Gap list = uncovered + unresolved, sorted.
	gaps := map[string]bool{}
	for _, g := range rep.Gaps {
		gaps[g] = true
	}
	if !gaps["collector:git"] || !gaps["parser:hcl"] {
		t.Errorf("gap list = %v, want collector:git and parser:hcl", rep.Gaps)
	}
	if gaps["collector:aws"] || gaps["capability:c.x"] {
		t.Error("covered/exempt must not appear in gap list")
	}
}

func TestMarshalReportDeterministic(t *testing.T) {
	c := reconcileFixture()
	a, err := MarshalReport(BuildReport(c, false))
	if err != nil {
		t.Fatalf("MarshalReport: %v", err)
	}
	b, _ := MarshalReport(BuildReport(c, false))
	if !bytes.Equal(a, b) {
		t.Error("MarshalReport is not deterministic across runs")
	}
	if a[len(a)-1] != '\n' {
		t.Error("report should end with a trailing newline")
	}
	var probe CoverageReport
	if err := json.Unmarshal(a, &probe); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}
}
