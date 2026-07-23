// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"
)

// TestBuildCodeRootVerdictsLexicalScopeRestriction is the #5500 regression
// suite, run against the REAL production registry
// (rubyRepoWideControllerRegistry). It proves the lexical-prefix candidate
// restriction (1) resolves a previously suffix_only_ambiguous ref EXACTLY and
// lets it downgrade when its true, lexically-scoped referent is a genuine
// non-controller, and (2) still resolves a ref whose true referent sits in an
// OUTER enclosing lexical scope, not just the immediate one.
func TestBuildCodeRootVerdictsLexicalScopeRestriction(t *testing.T) {
	tests := []struct {
		name        string
		input       CodeReachabilityProjectionInput
		wantVerdict string
		wantReason  string
	}{
		{
			// #5500 (RED pre-fix / GREEN post-fix): OrdersController is declared
			// inside namespace "Admin" (QualifiedName "Admin::OrdersController").
			// Its unqualified base ref "Base" has a same-lexical-scope corpus
			// class "Admin::Base" that resolves onward to the reject-set, plus an
			// unrelated corpus noise class "Reporting::Base" that ALSO does not
			// confirm. Pre-#5500: "Base" had zero ExactMatches and two broad,
			// unscoped SuffixMatches candidates; since neither confirms via the
			// probe, the walk kept it ambiguous forever. The lexical restriction
			// resolves "Base" to the TRUE referent "Admin::Base" and lets the
			// walk correctly downgrade.
			name: "namespaced ref resolves exactly via lexical scope and downgrades",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "Admin::OrdersController", QualifiedBases: []string{"Base"}},
				{Name: "Base", QualifiedName: "Admin::Base", QualifiedBases: []string{"ActiveRecord::Base"}},
				{Name: "Base", QualifiedName: "Reporting::Base", QualifiedBases: []string{"ActiveRecord::Base"}},
			}),
			wantVerdict: CodeRootVerdictDowngraded,
		},
		{
			// #5500: the true referent "Admin::Base" is declared one lexical
			// level OUT from OrdersController's own namespace ("Admin::V1"), not
			// in the immediate enclosing namespace. Real Ruby constant lookup
			// walks enclosing lexical scopes outward before falling to
			// top-level, so this must still resolve and confirm.
			name: "namespaced ref resolves through an outer enclosing lexical scope",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "Admin::V1::OrdersController", QualifiedBases: []string{"Base"}},
				{Name: "Base", QualifiedName: "Admin::Base", QualifiedBases: []string{"ApplicationController"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "accepted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, downgraded, _ := BuildCodeRootVerdicts(tt.input)
			row, ok := verdictByEntity(rows, "m:index")
			if !ok {
				t.Fatalf("expected a verdict row, got none (rows=%+v)", rows)
			}
			if row.Verdict != tt.wantVerdict {
				t.Fatalf("verdict = %q, want %q (basis=%+v)", row.Verdict, tt.wantVerdict, row.Basis)
			}
			if tt.wantReason != "" && row.Basis.Reason != tt.wantReason {
				t.Fatalf("basis.reason = %q, want %q (basis=%+v)", row.Basis.Reason, tt.wantReason, row.Basis)
			}
			_, isDown := downgraded[row.EntityID]
			if (tt.wantVerdict == CodeRootVerdictDowngraded) != isDown {
				t.Fatalf("downgraded set membership = %v, want %v", isDown, tt.wantVerdict == CodeRootVerdictDowngraded)
			}
		})
	}
}

// TestBuildCodeRootVerdictsLexicalScopeCoincidentalInnerMatchDoesNotMask is the
// #5500 P0 regression, run against the REAL production registry
// (rubyRepoWideControllerRegistry). classNamespaceOf cannot distinguish a
// genuinely nested-module-block declaration from Ruby's COMPACT COLON form
// (`class Admin::OrdersController < Base` with NO enclosing `module Admin`
// block) — qualifiedClassName produces the identical qualified name for both.
// For the compact form, real Ruby Module.nesting for the bare "Base"
// reference does NOT include "Admin", so the true referent is the TOP-LEVEL
// "Base" class. A coincidentally-named, unrelated "Admin::Base" class must
// NEVER mask that true referent from the candidate set: SuffixMatches only
// returns STRICT offset>0 matches, so the offset-0 top-level "Base" is
// unreachable any other way once masked, and a genuinely live controller
// would be falsely downgraded.
func TestBuildCodeRootVerdictsLexicalScopeCoincidentalInnerMatchDoesNotMask(t *testing.T) {
	input := verdictInput("m:index", "OrdersController", []RubyClassEntity{
		{Name: "OrdersController", QualifiedName: "Admin::OrdersController", QualifiedBases: []string{"Base"}},
		{Name: "Base", QualifiedName: "Admin::Base", QualifiedBases: []string{"ActiveRecord::Base"}}, // coincidental, unrelated, non-controller
		{Name: "Base", QualifiedName: "Base", QualifiedBases: []string{"ApplicationController"}},     // the TRUE top-level referent, a genuine controller
	})
	rows, downgraded, _ := BuildCodeRootVerdicts(input)
	row, ok := verdictByEntity(rows, "m:index")
	if !ok {
		t.Fatalf("expected a verdict row, got none (rows=%+v)", rows)
	}
	if row.Verdict != CodeRootVerdictConfirmed {
		t.Fatalf("genuine controller must be CONFIRMED (the true top-level Base referent must stay in the candidate set), got %+v", row)
	}
	if _, isDown := downgraded[row.EntityID]; isDown {
		t.Fatalf("genuine controller must not be in the downgraded set")
	}
}

// TestBuildCodeRootVerdictsAbsoluteReferenceBypassesLexicalScope is the #5733
// P1 regression (codex review of #5500), run against the REAL production
// registry (rubyRepoWideControllerRegistry) and the REAL parser-shaped
// QualifiedBases value. An ABSOLUTE reference ("::Base", Ruby's
// `class Admin::OrdersController < ::Base`) resolves starting at Object with
// NO enclosing-namespace search — a different rule than the bare, relative
// "Base". The corpus has an unrelated, coincidentally-named "Admin::Base"
// that shares OrdersController's own enclosing namespace and resolves onward
// to the reject-set; the real "::Base" is external to the corpus (e.g. a
// gem). Before the fix, rubyRepoWideControllerRegistry.DeclaredBasesOf
// returned "::Base" verbatim, but rubycontroller's normalizeBases stripped
// the leading "::" unconditionally before it ever reached the lexical-scope
// search, making the absolute reference indistinguishable from a relative
// one and wrongly resolving it onto "Admin::Base" — downgrading (and thus
// recommending deletion of) a genuinely live controller action.
func TestBuildCodeRootVerdictsAbsoluteReferenceBypassesLexicalScope(t *testing.T) {
	input := verdictInput("m:index", "OrdersController", []RubyClassEntity{
		{Name: "OrdersController", QualifiedName: "Admin::OrdersController", QualifiedBases: []string{"::Base"}},
		{Name: "Base", QualifiedName: "Admin::Base", QualifiedBases: []string{"ActiveRecord::Base"}}, // coincidental, unrelated, non-controller
	})
	rows, downgraded, _ := BuildCodeRootVerdicts(input)
	row, ok := verdictByEntity(rows, "m:index")
	if !ok {
		t.Fatalf("expected a verdict row, got none (rows=%+v)", rows)
	}
	if row.Verdict != CodeRootVerdictConfirmed {
		t.Fatalf("an absolute reference whose true top-level referent is absent from the corpus must stay CONFIRMED (keep-biased ambiguous), not resolve onto an unrelated namespace-mate, got %+v", row)
	}
	if row.Basis.Reason != "suffix_only_ambiguous" {
		t.Fatalf("basis.reason = %q, want %q (basis=%+v)", row.Basis.Reason, "suffix_only_ambiguous", row.Basis)
	}
	if _, isDown := downgraded[row.EntityID]; isDown {
		t.Fatalf("genuine controller must not be in the downgraded set")
	}
}

// buildNamespacedVerdictBenchInput constructs a representative, heavily
// namespaced Rails-shaped corpus mirroring the #5376 evidence note's
// representative-volume scale (~500 classes): each controller is nested two
// module levels deep (Mod{i}::Sub{i}) and every controller extends a base
// literally named "Base" so namesByLastSegment["Base"] collects hundreds of
// candidates — the shape the issue documents as inflating the
// SuffixAmbiguousKept counter on a heavily namespaced corpus. The first
// unresolvableCount controllers get a base that is NOT resolvable anywhere in
// the lexical chain (a genuine non-controller, falling through to the
// pre-#5500 broad SuffixMatches search over hundreds of same-named "Base"
// candidates — the added-cost case); the rest split 3:1 between an
// immediate-namespace base (lexical hit on the innermost candidate) and an
// outer-enclosing-scope base (hit one level out), both genuine controllers.
func buildNamespacedVerdictBenchInput(controllers, unresolvableCount int) CodeReachabilityProjectionInput {
	classes := make([]RubyClassEntity, 0, controllers*2)
	roots := make([]CodeReachabilityRoot, 0, controllers)
	for i := 0; i < controllers; i++ {
		outer := fmt.Sprintf("Mod%d", i%50)
		inner := fmt.Sprintf("Sub%d", i%17)
		namespace := outer + "::" + inner
		controllerName := fmt.Sprintf("Widget%dController", i)
		qualifiedController := namespace + "::" + controllerName

		var baseQualified string
		var baseBases []string
		switch {
		case i < unresolvableCount: // unresolvable anywhere in the lexical chain, non-controller.
			baseQualified = fmt.Sprintf("Unrelated%d::Base", i)
			baseBases = []string{"ActiveRecord::Base"}
		case i%4 == 3: // 1-in-4 of the rest: outer-enclosing-scope base, genuine controller.
			baseQualified = outer + "::Base"
			baseBases = []string{"ApplicationController"}
		default: // immediate-namespace base, genuine controller.
			baseQualified = namespace + "::Base"
			baseBases = []string{"ApplicationController"}
		}

		classes = append(
			classes,
			RubyClassEntity{Name: controllerName, QualifiedName: qualifiedController, QualifiedBases: []string{"Base"}},
			RubyClassEntity{Name: "Base", QualifiedName: baseQualified, QualifiedBases: baseBases},
		)
		roots = append(roots, CodeReachabilityRoot{
			EntityID:     fmt.Sprintf("m:%d", i),
			RootKinds:    []string{CodeRootKindRubyRailsControllerAction},
			ClassContext: controllerName,
		})
	}
	return CodeReachabilityProjectionInput{
		ScopeID:      "scope-bench",
		GenerationID: "gen-bench",
		RepositoryID: "repo-bench",
		Roots:        roots,
		RubyClasses:  classes,
	}
}

// BenchmarkBuildCodeRootVerdictsNamespacedCorpus is the #5500
// prove-the-theory-first WORST-CASE benchmark: it measures BuildCodeRootVerdicts'
// end-to-end cost (registry build + walk) over a representative, heavily
// namespaced 500-controller corpus so the lexical-prefix candidate
// restriction added to onwardHop is exercised on every hop, across its best
// case (immediate-scope hit), outer-scope hit, and full-miss-then-broad-fallback
// paths (20% of controllers). Compare ns/op against the pre-#5500 commit on the
// same corpus size.
// Run: go test ./internal/reducer -run='^$' -bench=BenchmarkBuildCodeRootVerdictsNamespacedCorpus -benchmem
func BenchmarkBuildCodeRootVerdictsNamespacedCorpus(b *testing.B) {
	input := buildNamespacedVerdictBenchInput(500, 100) // 20% unresolvable-anywhere.
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildCodeRootVerdicts(input)
	}
}

// BenchmarkBuildCodeRootVerdictsNamespacedCorpusTypical is the #5500 TYPICAL-case
// companion: the same 500-controller namespaced corpus shape, but with the
// evidence-5376-doc's own representative ratio (~99% correctly-based
// controllers, see evidence-5376-code-root-verdicts.md) instead of the 20%
// worst-case unresolvable-anywhere fraction. This is the throughput number that
// matters for a real Rails corpus, where nearly every controller resolves on
// the first (innermost) lexical candidate.
// Run: go test ./internal/reducer -run='^$' -bench=BenchmarkBuildCodeRootVerdictsNamespacedCorpusTypical -benchmem
func BenchmarkBuildCodeRootVerdictsNamespacedCorpusTypical(b *testing.B) {
	input := buildNamespacedVerdictBenchInput(500, 5) // ~1% unresolvable-anywhere.
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildCodeRootVerdicts(input)
	}
}
