// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// wrapperBypassFixture builds the issue-prescribed positive case: target T
// with direct callers {W, B}, where W is called by three functions and B
// calls T directly from another package.
func wrapperBypassFixture() (target string, direct []WrapperCallerRow, fanIn map[string]int, stats map[string]WrapperCandidateStats) {
	target = "fn-target"
	direct = []WrapperCallerRow{
		{EntityID: "fn-wrap", Name: "wrap", Package: "pkg/wrap", EdgeMethod: string(codeprovenance.MethodDeclared), EdgeConfidence: 0.95},
		{EntityID: "fn-bypass", Name: "bypass", Package: "pkg/other", EdgeMethod: string(codeprovenance.MethodDeclared), EdgeConfidence: 0.95},
	}
	fanIn = map[string]int{"fn-wrap": 3, "fn-bypass": 1}
	// fn-wrap is thin: complexity 2, two distinct callees with the target
	// accounting for half.
	stats = map[string]WrapperCandidateStats{
		"fn-wrap":   {Complexity: 2, CalleeCount: 2, TargetCalls: 1},
		"fn-bypass": {Complexity: 9, CalleeCount: 4, TargetCalls: 1},
	}
	return target, direct, fanIn, stats
}

func TestSelectCanonicalWrapperPositive(t *testing.T) {
	t.Parallel()

	target, direct, fanIn, stats := wrapperBypassFixture()
	wrapper, bypassers, verdict := SelectCanonicalWrapper(WrapperBypassInput{
		TargetID: target,
		Direct:   direct,
		FanIn:    fanIn,
		Stats:    stats,
		Params:   DefaultWrapperBypassParams(),
	})
	if verdict.Suppressed {
		t.Fatalf("positive fixture suppressed: %s", verdict.Reason)
	}
	if wrapper.EntityID != "fn-wrap" {
		t.Fatalf("wrapper = %q, want fn-wrap", wrapper.EntityID)
	}
	if len(bypassers) != 1 || bypassers[0].EntityID != "fn-bypass" {
		t.Fatalf("bypassers = %+v, want exactly fn-bypass", bypassers)
	}
	if verdict.Confidence != 0.95 {
		t.Fatalf("confidence = %v, want weakest contributing edge 0.95", verdict.Confidence)
	}
	if verdict.Inferred {
		t.Fatal("positive fixture flagged inferred, want all-declared edges clean")
	}
}

// TestSelectCanonicalWrapperSamePackageDirectIsNotBypass covers the first
// negative case: a direct caller inside the wrapper's own package is not a
// bypasser, so with only such a caller there is no finding.
func TestSelectCanonicalWrapperSamePackageDirectIsNotBypass(t *testing.T) {
	t.Parallel()

	_, direct, fanIn, stats := wrapperBypassFixture()
	direct[1] = WrapperCallerRow{EntityID: "fn-near", Name: "near", Package: "pkg/wrap", EdgeMethod: string(codeprovenance.MethodDeclared), EdgeConfidence: 0.95}
	_, bypassers, verdict := SelectCanonicalWrapper(WrapperBypassInput{
		TargetID: "fn-target",
		Direct:   direct,
		FanIn:    fanIn,
		Stats:    stats,
		Params:   DefaultWrapperBypassParams(),
	})
	if !verdict.Suppressed {
		t.Fatalf("same-package direct admitted with bypassers %+v, want suppression", bypassers)
	}
	if len(bypassers) != 0 {
		t.Fatalf("bypassers = %+v, want none (same-package direct is not a bypass)", bypassers)
	}
}

// TestSelectCanonicalWrapperPeerWrappersSuppress covers the second negative
// case: two peer wrappers with comparable fan-in mean no unique canonical
// wrapper, so the finding suppresses instead of picking one.
func TestSelectCanonicalWrapperPeerWrappersSuppress(t *testing.T) {
	t.Parallel()

	direct := []WrapperCallerRow{
		{EntityID: "fn-wrap-a", Name: "wrapA", Package: "pkg/a", EdgeMethod: string(codeprovenance.MethodDeclared), EdgeConfidence: 0.95},
		{EntityID: "fn-wrap-b", Name: "wrapB", Package: "pkg/b", EdgeMethod: string(codeprovenance.MethodDeclared), EdgeConfidence: 0.95},
	}
	fanIn := map[string]int{"fn-wrap-a": 12, "fn-wrap-b": 11}
	stats := map[string]WrapperCandidateStats{
		"fn-wrap-a": {Complexity: 2, CalleeCount: 2, TargetCalls: 1},
		"fn-wrap-b": {Complexity: 2, CalleeCount: 2, TargetCalls: 1},
	}
	_, _, verdict := SelectCanonicalWrapper(WrapperBypassInput{
		TargetID: "fn-target",
		Direct:   direct,
		FanIn:    fanIn,
		Stats:    stats,
		Params:   DefaultWrapperBypassParams(),
	})
	if !verdict.Suppressed {
		t.Fatal("peer wrappers with comparable fan-in admitted, want suppression")
	}
}

// TestSelectCanonicalWrapperInferredWrapperIsAmbiguous covers the ambiguous
// case: the wrapper reaches the target only through an inferred edge, so the
// finding admits with the inferred evidence surfaced, never silently.
func TestSelectCanonicalWrapperInferredWrapperIsAmbiguous(t *testing.T) {
	t.Parallel()

	target, direct, fanIn, stats := wrapperBypassFixture()
	direct[0] = WrapperCallerRow{EntityID: "fn-wrap", Name: "wrap", Package: "pkg/wrap", EdgeMethod: string(codeprovenance.MethodTypeInferred), EdgeConfidence: 0.80}
	_, bypassers, verdict := SelectCanonicalWrapper(WrapperBypassInput{
		TargetID: target,
		Direct:   direct,
		FanIn:    fanIn,
		Stats:    stats,
		Params:   DefaultWrapperBypassParams(),
	})
	if verdict.Suppressed {
		t.Fatalf("inferred-wrapper fixture suppressed: %s", verdict.Reason)
	}
	if !verdict.Inferred {
		t.Fatal("inferred wrapper edge not surfaced, want Inferred with reason")
	}
	if verdict.Reason == "" {
		t.Fatal("ambiguous finding carries no reason, want the inferred-edge basis named")
	}
	if len(bypassers) != 1 {
		t.Fatalf("bypassers = %+v, want fn-bypass", bypassers)
	}
	if verdict.Confidence != 0.80 {
		t.Fatalf("confidence = %v, want weakest contributing edge 0.80", verdict.Confidence)
	}
}

// TestSelectCanonicalWrapperHeavyCandidateSuppresses proves the thinness
// gate: a high-fan-in direct caller with high cyclomatic complexity is not
// a wrapper, so the finding suppresses instead of crowning it.
func TestSelectCanonicalWrapperHeavyCandidateSuppresses(t *testing.T) {
	t.Parallel()

	target, direct, fanIn, stats := wrapperBypassFixture()
	heavy := stats["fn-wrap"]
	heavy.Complexity = 42
	stats["fn-wrap"] = heavy
	_, _, verdict := SelectCanonicalWrapper(WrapperBypassInput{
		TargetID: target,
		Direct:   direct,
		FanIn:    fanIn,
		Stats:    stats,
		Params:   DefaultWrapperBypassParams(),
	})
	if !verdict.Suppressed {
		t.Fatal("high-complexity candidate admitted as wrapper, want suppression")
	}
}
