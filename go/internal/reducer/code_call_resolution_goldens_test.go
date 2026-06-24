// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Receiver-constrained cross-file call-resolution precision corpus for epic
// #3154 / issue #3156.
//
// Each golden is human-authored expected truth (not serialized Eshu output)
// exercising the reducer call-resolution path through ExtractCodeCallRows. The
// corpus locks in current per-language precision and provenance/confidence, and
// — critically — guards against false positives: an ambiguous, dynamic, or
// missing-dependency call must never silently resolve to a wrong target.
//
// Cases divide into:
//   - positive: a CALLS/REFERENCES edge to wantCallee resolved by wantMethod;
//     confidence is derived from wantMethod per ADR #2222 (codeprovenance).
//   - negative (false-positive guard): no materialized edge may resolve the call
//     to any forbidCallees target.
//
// Where current resolution is conservative (weaker method than the precision
// ideal, but never fabricated), idealMethod + gapIssue record a tracked gap so
// the corpus documents where precision can still improve without hiding a false
// positive. The assertion is always on actual current behavior, so the corpus
// stays green and acts as a regression guard.

// resolutionCategory names the receiver-constrained precision dimension a golden
// exercises, matching the #3156 acceptance criteria.
type resolutionCategory string

const (
	categorySameNameMethods   resolutionCategory = "same_name_methods"
	categoryReceiverAmbiguity resolutionCategory = "receiver_ambiguity"
	categoryReexport          resolutionCategory = "reexport"
	categoryAlias             resolutionCategory = "alias"
	categoryDynamicImport     resolutionCategory = "dynamic_import"
	categoryMissingDependency resolutionCategory = "missing_dependency"
	categoryRepoFallback      resolutionCategory = "repo_fallback"
)

// callResolutionGolden is one cross-file call-resolution scenario.
type callResolutionGolden struct {
	name      string
	category  resolutionCategory
	envelopes []facts.Envelope

	// Positive expectation. Empty wantCallee marks a pure-negative case.
	wantCallee string
	wantMethod codeprovenance.Method
	// wantConfidence is the expected derived confidence for the resolved edge,
	// stated independently of wantMethod so a drift in the codeprovenance tier
	// table is caught even when the method is unchanged (and so #3158 consumers
	// have a fixed confidence truth per case). Required for positive cases.
	wantConfidence float64

	// Negative expectation: no resolved edge may target any of these.
	forbidCallees []string

	// idealMethod records the precision target when current behavior is weaker
	// than ideal but still honest (conservative). When set and different from
	// wantMethod, the runner logs a tracked KNOWN GAP.
	idealMethod codeprovenance.Method
	gapIssue    string

	// falsePositiveGap marks a case where current resolution is a KNOWN false
	// positive: a forbidCallees target is currently (wrongly) resolved. The
	// gap is documented, not hidden — the runner logs it loudly with the
	// tracking issue and does not fail, but it FAILS if the false positive is
	// no longer present, forcing the marker to be removed and the case promoted
	// to a strict negative guard once the resolver is fixed.
	falsePositiveGap string
}

// runCallResolutionGoldens executes a corpus and asserts both the positive
// resolution truth (method + derived confidence) and the false-positive guard.
func runCallResolutionGoldens(t *testing.T, goldens []callResolutionGolden) {
	t.Helper()
	for _, g := range goldens {
		g := g
		t.Run(string(g.category)+"/"+g.name, func(t *testing.T) {
			t.Parallel()

			_, rows := ExtractCodeCallRows(g.envelopes)

			resolvedForbidden := func(target string) (string, bool) {
				for _, row := range rows {
					if anyToString(row["callee_entity_id"]) == target {
						return anyToString(row["resolution_method"]), true
					}
				}
				return "", false
			}

			// False-positive guard. With no falsePositiveGap marker, any
			// forbidden target resolving is a hard failure. With a marker, every
			// forbidCallees entry is a DOCUMENTED known false positive (logged +
			// tracked, not failing) — and the case fails if ANY of them is no
			// longer a false positive, so the marker cannot outlive the bug it
			// tracks even when several targets are listed.
			documentedHits := 0
			for _, forbidden := range g.forbidCallees {
				method, hit := resolvedForbidden(forbidden)
				if g.falsePositiveGap == "" {
					if hit {
						t.Errorf("false positive: call resolved to forbidden target %q (method=%q)",
							forbidden, method)
					}
					continue
				}
				if !hit {
					continue
				}
				documentedHits++
				t.Logf("DOCUMENTED FALSE POSITIVE (%s): call resolves to %q via %q; tracked by %s",
					g.name, forbidden, method, g.falsePositiveGap)
			}
			if g.falsePositiveGap != "" && documentedHits != len(g.forbidCallees) {
				t.Errorf("false positive tracked by %s appears (partly) fixed for %q (%d/%d forbidden targets still resolve): remove falsePositiveGap and promote the fixed targets to strict negative guards",
					g.falsePositiveGap, g.name, documentedHits, len(g.forbidCallees))
			}

			if g.wantCallee == "" {
				return // pure-negative case: only the guard above applies
			}

			gotMethod, found := "", false
			for _, row := range rows {
				if anyToString(row["callee_entity_id"]) == g.wantCallee {
					gotMethod, found = anyToString(row["resolution_method"]), true
					break
				}
			}
			if !found {
				t.Fatalf("no resolved edge to %q (rows=%d)", g.wantCallee, len(rows))
			}
			if gotMethod != string(g.wantMethod) {
				t.Errorf("resolution_method = %q, want %q", gotMethod, g.wantMethod)
			}

			// Confidence is a derivation of resolution_method (ADR #2222),
			// asserted against the case's independently-stated wantConfidence so
			// a tier-table drift is caught even when the method matches.
			if g.wantConfidence == 0 {
				t.Fatalf("golden %q is a positive case but sets no wantConfidence", g.name)
			}
			if got := codeprovenance.Confidence(codeprovenance.Method(gotMethod)); got != g.wantConfidence {
				t.Errorf("derived confidence = %v, want %v", got, g.wantConfidence)
			}

			if g.idealMethod != "" && g.idealMethod != g.wantMethod {
				t.Logf("KNOWN GAP (%s): current method %q (confidence %v) is weaker than ideal %q (confidence %v); tracked by %s",
					g.name, g.wantMethod, codeprovenance.Confidence(g.wantMethod),
					g.idealMethod, codeprovenance.Confidence(g.idealMethod), g.gapIssue)
			}
		})
	}
}
