// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestReportViolationsFiltersAndSorts(t *testing.T) {
	r := Report{Findings: []Finding{
		{Path: "go/internal/query/taghistory", Verdict: VerdictGluedCompound, Evidence: "tag+history"},
		{Path: "go/internal/collector/preflight", Verdict: VerdictAcceptable, Evidence: "single word"},
		{Path: "go/internal/reducer/workloadinstance", Verdict: VerdictGluedCompound, Evidence: "workload+instance"},
	}}

	got := r.Violations()
	if len(got) != 2 {
		t.Fatalf("Violations() returned %d findings, want 2 (acceptable finding must be excluded): %+v", len(got), got)
	}
	if got[0].Path != "go/internal/query/taghistory" || got[1].Path != "go/internal/reducer/workloadinstance" {
		t.Fatalf("Violations() not sorted by path: %+v", got)
	}
}

func TestReportViolationsEmptyWhenAllAcceptable(t *testing.T) {
	r := Report{Findings: []Finding{
		{Path: "go/internal/query/entity", Verdict: VerdictAcceptable},
	}}
	if got := r.Violations(); len(got) != 0 {
		t.Fatalf("Violations() = %+v, want empty", got)
	}
}

func TestWriteJSONRoundTrips(t *testing.T) {
	r := Report{Findings: []Finding{
		{
			Path: "go/internal/reducer/workloadinstance", Name: "workloadinstance", Kind: "directory",
			Verdict: VerdictGluedCompound, Confidence: "high", Disposition: DispositionBlock,
			Evidence: "reads as WORKLOAD + INSTANCE glued", SuggestedSplit: []string{"workload", "instance"},
		},
	}}
	var buf bytes.Buffer
	if err := r.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	out := buf.String()
	for _, want := range []string{`"glued_compound"`, `"workload"`, `"instance"`, `"block"`} {
		if !strings.Contains(out, want) {
			t.Errorf("WriteJSON output missing %q; got: %s", want, out)
		}
	}
}

func TestWriteHumanFormatsEachViolation(t *testing.T) {
	violations := []Finding{
		{Path: "go/internal/reducer/workloadinstance", Evidence: "workload+instance glued", Confidence: "high", SuggestedSplit: []string{"workload", "instance"}},
	}
	var buf bytes.Buffer
	WriteHuman(&buf, violations)
	out := buf.String()
	if !strings.Contains(out, "go/internal/reducer/workloadinstance") {
		t.Errorf("WriteHuman missing path: %s", out)
	}
	if !strings.Contains(out, "workload+instance glued") {
		t.Errorf("WriteHuman missing evidence: %s", out)
	}
	if !strings.Contains(out, "high confidence") {
		t.Errorf("WriteHuman missing confidence: %s", out)
	}
}
