// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// --- baseline parse/render tests -----------------------------------------

func TestBaselineRoundTrip(t *testing.T) {
	counts := map[string]int{
		"scripts/b.sh":    2,
		"scripts/a.sh":    1,
		"scripts/zero.sh": 0, // must be omitted from rendered output
	}

	rendered := RenderBaseline(counts)

	parsed, err := ParseBaseline(strings.NewReader(rendered))
	if err != nil {
		t.Fatalf("parse rendered baseline: %v", err)
	}
	if parsed["scripts/a.sh"] != 1 || parsed["scripts/b.sh"] != 2 {
		t.Fatalf("unexpected parsed counts: %v", parsed)
	}
	if _, ok := parsed["scripts/zero.sh"]; ok {
		t.Fatalf("zero-count entries must be omitted from the rendered baseline")
	}

	idxA := strings.Index(rendered, "scripts/a.sh")
	idxB := strings.Index(rendered, "scripts/b.sh")
	if idxA == -1 || idxB == -1 || idxA > idxB {
		t.Fatalf("expected deterministic sorted output, got:\n%s", rendered)
	}
}

func TestParseBaseline_IgnoresCommentsAndBlankLines(t *testing.T) {
	input := "# header comment\n\n  \nscripts/a.sh 3\n# trailing comment\nscripts/b.sh 1\n"

	parsed, err := ParseBaseline(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseBaseline: %v", err)
	}
	if parsed["scripts/a.sh"] != 3 || parsed["scripts/b.sh"] != 1 {
		t.Fatalf("unexpected parsed counts: %v", parsed)
	}
}

// --- baseline comparison (burn-down) tests -------------------------------

func TestCheckBaseline_NewFileWithViolationFails(t *testing.T) {
	current := map[string][]Violation{
		"scripts/new.sh": {{Path: "scripts/new.sh", Line: 3, Size: 600}},
	}
	baseline := map[string]int{}

	result := CheckBaseline(current, baseline)

	if result.OK {
		t.Fatalf("expected failure for a new offending file not in the baseline")
	}
	if _, ok := result.Failures["scripts/new.sh"]; !ok {
		t.Fatalf("expected scripts/new.sh in failures, got %v", result.Failures)
	}
}

func TestCheckBaseline_IncreasedCountFails(t *testing.T) {
	current := map[string][]Violation{
		"scripts/existing.sh": {
			{Path: "scripts/existing.sh", Line: 3, Size: 600},
			{Path: "scripts/existing.sh", Line: 20, Size: 700},
		},
	}
	baseline := map[string]int{"scripts/existing.sh": 1}

	result := CheckBaseline(current, baseline)

	if result.OK {
		t.Fatalf("expected failure when a baselined file's count increases")
	}
	if _, ok := result.Failures["scripts/existing.sh"]; !ok {
		t.Fatalf("expected scripts/existing.sh in failures, got %v", result.Failures)
	}
}

func TestCheckBaseline_DecreasedOrEqualCountPasses(t *testing.T) {
	current := map[string][]Violation{
		"scripts/existing.sh": {{Path: "scripts/existing.sh", Line: 3, Size: 600}},
	}

	decreased := CheckBaseline(current, map[string]int{"scripts/existing.sh": 2})
	if !decreased.OK {
		t.Fatalf("expected pass when count decreased (burn-down), got failures: %v", decreased.Failures)
	}

	equal := CheckBaseline(current, map[string]int{"scripts/existing.sh": 1})
	if !equal.OK {
		t.Fatalf("expected pass when count stayed equal, got failures: %v", equal.Failures)
	}
}

func TestCheckBaseline_UnknownCleanFilePasses(t *testing.T) {
	current := map[string][]Violation{
		"scripts/clean.sh": {}, // no violations
	}
	baseline := map[string]int{}

	result := CheckBaseline(current, baseline)

	if !result.OK {
		t.Fatalf("expected pass for a clean file absent from the baseline, got failures: %v", result.Failures)
	}
}
