// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"
)

// verdictInput builds a minimal projection input for the pure verdict builder:
// one Ruby controller action root and a repo-wide class set.
func verdictInput(rootEntityID, classContext string, classes []RubyClassEntity) CodeReachabilityProjectionInput {
	return CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepositoryID: "repo-1",
		Roots: []CodeReachabilityRoot{{
			EntityID:     rootEntityID,
			RootKinds:    []string{CodeRootKindRubyRailsControllerAction},
			ClassContext: classContext,
		}},
		RubyClasses: classes,
		ObservedAt:  time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
	}
}

func verdictByEntity(rows []CodeRootVerdictRow, entityID string) (CodeRootVerdictRow, bool) {
	for _, row := range rows {
		if row.EntityID == entityID {
			return row, true
		}
	}
	return CodeRootVerdictRow{}, false
}

func TestBuildCodeRootVerdicts(t *testing.T) {
	tests := []struct {
		name           string
		input          CodeReachabilityProjectionInput
		wantVerdict    string // "" means no row expected
		wantDowngraded bool
		wantReason     string
		wantMissingCtx int
	}{
		{
			// #5376 P1 REGRESSION: the base "Admin::Base" resolves via qualified
			// name to the same-repo Base@Admin::Base < ActionController::Base. The
			// old simple-name registry keyed Base as "Base" so this qualified
			// reference never resolved and the GENUINE controller was downgraded.
			name: "namespaced base resolves via qualified name to accepted base",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "OrdersController", QualifiedBases: []string{"Admin::Base"}},
				{Name: "Base", QualifiedName: "Admin::Base", QualifiedBases: []string{"ActionController::Base"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "accepted",
		},
		{
			name: "chain resolves in-corpus onward to a rejected framework base",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "OrdersController", QualifiedBases: []string{"BaseController"}},
				{Name: "BaseController", QualifiedName: "BaseController", QualifiedBases: []string{"ApplicationRecord"}},
				{Name: "ApplicationRecord", QualifiedName: "ApplicationRecord", QualifiedBases: []string{"ActiveRecord::Base"}},
			}),
			wantVerdict:    CodeRootVerdictDowngraded,
			wantDowngraded: true,
			wantReason:     "rejected_framework_base",
		},
		{
			// F1 impostor guard: the gem base "Payments::Base" must NOT resolve to
			// the same-last-segment corpus class "Reporting::Base"; it stays
			// unresolved-qualified and KEEPS.
			name: "impostor qualified gem base does not resolve to same-segment corpus class",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "OrdersController", QualifiedBases: []string{"Payments::Base"}},
				{Name: "Base", QualifiedName: "Reporting::Base", QualifiedBases: []string{"ActionController::Base"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "unresolved_qualified",
		},
		{
			name: "unresolved qualified base keeps (F1 floor)",
			input: verdictInput("m:index", "AuthBase", []RubyClassEntity{
				{Name: "AuthBase", QualifiedName: "AuthBase", QualifiedBases: []string{"SomeGem::Railsy"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "unresolved_qualified",
		},
		{
			// Unqualified base ref "Base" resolves to the namespaced class
			// Admin::Base (k=1 last-segment suffix match).
			name: "unqualified ref resolves to namespaced class",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "OrdersController", QualifiedBases: []string{"Base"}},
				{Name: "Base", QualifiedName: "Admin::Base", QualifiedBases: []string{"ActionController::Base"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "accepted",
		},
		{
			name: "reopened-class union reaches accepted base",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "OrdersController", QualifiedBases: []string{"ActionController::Base"}},
				{Name: "OrdersController", QualifiedName: "OrdersController", QualifiedBases: nil},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "accepted",
		},
		{
			name: "collision conflict kept when any path confirms",
			input: verdictInput("m:list_users", "UsersController", []RubyClassEntity{
				{Name: "UsersController", QualifiedName: "UsersController", QualifiedBases: []string{"BaseController"}},
				// BaseController reopened/colliding: one path to Rails, one to Thor.
				{Name: "BaseController", QualifiedName: "BaseController", QualifiedBases: []string{"ActionController::Base"}},
				{Name: "BaseController", QualifiedName: "BaseController", QualifiedBases: []string{"Thor"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "accepted",
		},
		{
			name: "cycle is keep-biased for controller name",
			input: verdictInput("m:show", "FooController", []RubyClassEntity{
				{Name: "FooController", QualifiedName: "FooController", QualifiedBases: []string{"BarController"}},
				{Name: "BarController", QualifiedName: "BarController", QualifiedBases: []string{"FooController"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "cycle",
		},
		{
			name:           "missing class_context writes no row and counts inconclusive",
			input:          verdictInput("m:index", "   ", nil),
			wantVerdict:    "",
			wantDowngraded: false,
			wantMissingCtx: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, downgraded, stats := BuildCodeRootVerdicts(tt.input)
			if stats.InconclusiveMissingContext != tt.wantMissingCtx {
				t.Fatalf("InconclusiveMissingContext = %d, want %d", stats.InconclusiveMissingContext, tt.wantMissingCtx)
			}
			row, ok := verdictByEntity(rows, "m:index")
			if !ok {
				row, ok = verdictByEntity(rows, "m:list_users")
			}
			if !ok {
				row, ok = verdictByEntity(rows, "m:show")
			}
			if tt.wantVerdict == "" {
				if ok {
					t.Fatalf("expected no verdict row, got %+v", row)
				}
				if len(downgraded) != 0 {
					t.Fatalf("expected empty downgraded set, got %v", downgraded)
				}
				return
			}
			if !ok {
				t.Fatalf("expected a verdict row, got none (rows=%+v)", rows)
			}
			if row.Verdict != tt.wantVerdict {
				t.Fatalf("verdict = %q, want %q (basis=%+v)", row.Verdict, tt.wantVerdict, row.Basis)
			}
			if tt.wantReason != "" && row.Basis.Reason != tt.wantReason {
				t.Fatalf("basis.reason = %q, want %q (basis=%+v)", row.Basis.Reason, tt.wantReason, row.Basis)
			}
			if row.RootKind != CodeRootKindRubyRailsControllerAction {
				t.Fatalf("root_kind = %q, want %q", row.RootKind, CodeRootKindRubyRailsControllerAction)
			}
			if row.ScopeID != "scope-1" || row.GenerationID != "gen-1" || row.RepositoryID != "repo-1" {
				t.Fatalf("partition columns not propagated: %+v", row)
			}
			_, isDown := downgraded[row.EntityID]
			if isDown != tt.wantDowngraded {
				t.Fatalf("downgraded[%q] = %v, want %v", row.EntityID, isDown, tt.wantDowngraded)
			}
			// Confirmed rows must never be in the downgraded set (query acts only
			// on downgraded; a confirmed row must never suppress a root).
			if row.Verdict == CodeRootVerdictConfirmed && isDown {
				t.Fatalf("confirmed row must not be in downgraded set")
			}
		})
	}
}

// TestBuildCodeRootVerdictsP0Rev2 is the #5376 P0 rev-2 regression suite. Its
// TEST-SHAPE RULE: every impostor case uses UNEQUAL-length names where the
// shorter is a STRICT segment suffix of the longer, so the proper-suffix path is
// actually exercised (the earlier equal-length guard could not suffix-match and
// was false-green). Tests 1, 2, 4, 7b, 8b are the RED-first cases against the
// pre-rev-2 walk.
func TestBuildCodeRootVerdictsP0Rev2(t *testing.T) {
	tests := []struct {
		name             string
		input            CodeReachabilityProjectionInput
		wantVerdict      string
		wantReason       string
		chainMustExclude string
	}{
		{
			// Test 1 (RED): the proven P0 — Api::Base is a gem base with NO exact
			// corpus match, only a proper-suffix impostor Internal::Api::Base <
			// ActiveRecord::Base. Must KEEP, and the impostor hop must NOT be
			// attributed to Api::Base.
			name: "proven P0 suffix-only impostor keeps",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "OrdersController", QualifiedBases: []string{"Api::Base"}},
				{Name: "Base", QualifiedName: "Internal::Api::Base", QualifiedBases: []string{"ActiveRecord::Base"}},
			}),
			wantVerdict:      CodeRootVerdictConfirmed,
			wantReason:       "suffix_only_ambiguous",
			chainMustExclude: "Internal::Api::Base",
		},
		{
			// Test 2 (RED): unqualified suffix-only ref "Base" must terminate at
			// ambiguous-KEEP, never slip to the simple-name downgrade.
			name: "k=1 unqualified suffix-only ref keeps",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "OrdersController", QualifiedBases: []string{"Base"}},
				{Name: "Base", QualifiedName: "Foo::Base", QualifiedBases: []string{"ActiveRecord::Base"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "suffix_only_ambiguous",
		},
		{
			// Test 3 (GREEN): a suffix candidate that confirms promotes to accepted.
			name: "suffix-only ref probe-confirms to accepted",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "OrdersController", QualifiedBases: []string{"Base"}},
				{Name: "Base", QualifiedName: "Admin::Base", QualifiedBases: []string{"ActionController::Base"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "accepted",
		},
		{
			// Test 4 (RED): a suffix impostor mid-chain (Core::Base ->
			// Legacy::Core::Base < ApplicationRecord) must not downgrade the chain.
			name: "mid-chain suffix impostor keeps",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "OrdersController", QualifiedBases: []string{"Admin::Base"}},
				{Name: "Base", QualifiedName: "Admin::Base", QualifiedBases: []string{"Core::Base"}},
				{Name: "Base", QualifiedName: "Legacy::Core::Base", QualifiedBases: []string{"ApplicationRecord"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "suffix_only_ambiguous",
		},
		{
			// Test 5 (GREEN): true-positive downgrade preserved.
			name: "true-positive reject-set downgrade preserved",
			input: verdictInput("m:index", "FooController", []RubyClassEntity{
				{Name: "FooController", QualifiedName: "FooController", QualifiedBases: []string{"ApplicationRecord"}},
				{Name: "ApplicationRecord", QualifiedName: "ApplicationRecord", QualifiedBases: []string{"ActiveRecord::Base"}},
			}),
			wantVerdict: CodeRootVerdictDowngraded,
			wantReason:  "rejected_framework_base",
		},
		{
			// Test 7a (GREEN): exact impostor Base<ActiveRecord::Base beaten by a
			// confirming suffix candidate Api::V1::Base<ActionController::API.
			name: "exact impostor beaten by confirming suffix candidate",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "OrdersController", QualifiedBases: []string{"Base"}},
				{Name: "Base", QualifiedName: "Base", QualifiedBases: []string{"ActiveRecord::Base"}},
				{Name: "Base", QualifiedName: "Api::V1::Base", QualifiedBases: []string{"ActionController::API"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "accepted",
		},
		{
			// Test 7b (RED): exact impostor Base<ActiveRecord::Base beaten by a
			// suffix candidate that F1-KEEPS (Api::V1::Base < SomeGem::Thing).
			name: "exact impostor beaten by F1-keeping suffix candidate",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "OrdersController", QualifiedBases: []string{"Base"}},
				{Name: "Base", QualifiedName: "Base", QualifiedBases: []string{"ActiveRecord::Base"}},
				{Name: "Base", QualifiedName: "Api::V1::Base", QualifiedBases: []string{"SomeGem::Thing"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
		},
		{
			// Test 8a (GREEN): a simple non-controller base with zero candidates
			// downgrades.
			name: "simple non-controller base with zero candidates downgrades",
			input: verdictInput("m:index", "FooController", []RubyClassEntity{
				{Name: "FooController", QualifiedName: "FooController", QualifiedBases: []string{"Thor"}},
			}),
			wantVerdict: CodeRootVerdictDowngraded,
			wantReason:  "unresolved_non_controller",
		},
		{
			// Test 8b (RED): conventional simple base "Base" with zero corpus
			// candidates keeps (the conventional-name guard).
			name: "conventional simple base with zero candidates keeps",
			input: verdictInput("m:index", "FooController", []RubyClassEntity{
				{Name: "FooController", QualifiedName: "FooController", QualifiedBases: []string{"Base"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "suffix_only_ambiguous",
		},
		{
			// Test 9 (GREEN): a corpus Legacy::ActiveRecord::Base shadows a literal
			// ActiveRecord::Base ref; the suffix step is checked before the
			// reject-set, so it keeps.
			name: "reject-set literal shadowed by suffix candidate keeps",
			input: verdictInput("m:index", "FooController", []RubyClassEntity{
				{Name: "FooController", QualifiedName: "FooController", QualifiedBases: []string{"ActiveRecord::Base"}},
				{Name: "Base", QualifiedName: "Legacy::ActiveRecord::Base", QualifiedBases: []string{"SomeGem::Thing"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "suffix_only_ambiguous",
		},
		{
			// Test 11a (GREEN): homonym defining classes; one confirms -> confirmed.
			name: "entry homonyms one confirms keeps",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "Shop::OrdersController", QualifiedBases: []string{"ApplicationController"}},
				{Name: "OrdersController", QualifiedName: "Admin::OrdersController", QualifiedBases: []string{"ApplicationRecord"}},
			}),
			wantVerdict: CodeRootVerdictConfirmed,
			wantReason:  "accepted",
		},
		{
			// Test 11b (GREEN): homonym defining classes; every candidate
			// authoritatively downgrades -> downgraded (entry multimap authoritative).
			name: "entry homonyms all downgrade downgrades",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedName: "Shop::OrdersController", QualifiedBases: []string{"ApplicationRecord"}},
				{Name: "OrdersController", QualifiedName: "Admin::OrdersController", QualifiedBases: []string{"ApplicationRecord"}},
				{Name: "ApplicationRecord", QualifiedName: "ApplicationRecord", QualifiedBases: []string{"ActiveRecord::Base"}},
			}),
			wantVerdict: CodeRootVerdictDowngraded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, _, _ := BuildCodeRootVerdicts(tt.input)
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
			if tt.chainMustExclude != "" {
				for _, hop := range row.Basis.Chain {
					if hop == tt.chainMustExclude {
						t.Fatalf("chain must not attribute impostor hop %q: %+v", tt.chainMustExclude, row.Basis.Chain)
					}
				}
			}
		})
	}
}

// TestSuffixAmbiguousNeverEntersBFSRemoval proves the D1/D6 safety property: a
// suffix_only_ambiguous verdict is CONFIRMED, so it is never in the downgraded
// set and removeDowngradedRailsControllerRoots keeps its BFS root — a
// suffix-ambiguous controller can never have its reachability suppressed.
func TestSuffixAmbiguousNeverEntersBFSRemoval(t *testing.T) {
	input := verdictInput("m:index", "OrdersController", []RubyClassEntity{
		{Name: "OrdersController", QualifiedName: "OrdersController", QualifiedBases: []string{"Api::Base"}},
		{Name: "Base", QualifiedName: "Internal::Api::Base", QualifiedBases: []string{"ActiveRecord::Base"}},
	})
	rows, downgraded, stats := BuildCodeRootVerdicts(input)
	row, ok := verdictByEntity(rows, "m:index")
	if !ok || row.Verdict != CodeRootVerdictConfirmed || row.Basis.Reason != "suffix_only_ambiguous" {
		t.Fatalf("want confirmed suffix_only_ambiguous, got ok=%v row=%+v", ok, row)
	}
	if len(downgraded) != 0 {
		t.Fatalf("suffix_only_ambiguous must not be in the downgraded set, got %v", downgraded)
	}
	if stats.SuffixAmbiguousKept != 1 {
		t.Fatalf("SuffixAmbiguousKept = %d, want 1", stats.SuffixAmbiguousKept)
	}
	kept := removeDowngradedRailsControllerRoots(input.Roots, downgraded)
	if _, ok := findRoot(kept, "m:index"); !ok {
		t.Fatalf("suffix-ambiguous controller root must be retained in the BFS root set")
	}
}

// TestBuildCodeRootVerdictsDepthCapKept builds a chain longer than the shared
// walk's cap and proves the resulting verdict is confirmed (keep-biased), never
// a downgrade, even though the deep tail resolves to a non-controller base.
func TestBuildCodeRootVerdictsDepthCapKept(t *testing.T) {
	classes := []RubyClassEntity{{Name: "C0Controller", QualifiedBases: []string{"C1"}}}
	for i := 1; i <= 11; i++ {
		classes = append(classes, RubyClassEntity{Name: itoaN("C", i), QualifiedBases: []string{itoaN("C", i+1)}})
	}
	classes = append(classes, RubyClassEntity{Name: "C12", QualifiedBases: []string{"ActiveRecord::Base"}})
	rows, downgraded, _ := BuildCodeRootVerdicts(verdictInput("m:index", "C0Controller", classes))
	row, ok := verdictByEntity(rows, "m:index")
	if !ok || row.Verdict != CodeRootVerdictConfirmed {
		t.Fatalf("deep chain from *Controller must be confirmed, got ok=%v row=%+v", ok, row)
	}
	if len(downgraded) != 0 {
		t.Fatalf("depth cap must not downgrade, got %v", downgraded)
	}
}

// TestBuildCodeRootVerdictsOnlyRailsKind proves a root that carries a non-rails
// root kind produces no verdict row: the verdict builder acts only on
// ruby.rails_controller_action.
func TestBuildCodeRootVerdictsOnlyRailsKind(t *testing.T) {
	in := verdictInput("m:index", "OrdersController", []RubyClassEntity{
		{Name: "OrdersController", QualifiedBases: []string{"ActiveRecord::Base"}},
	})
	in.Roots[0].RootKinds = []string{"ruby.script_entrypoint"}
	rows, downgraded, stats := BuildCodeRootVerdicts(in)
	if len(rows) != 0 {
		t.Fatalf("non-rails root kind must produce no verdict row, got %+v", rows)
	}
	if len(downgraded) != 0 || stats.InconclusiveMissingContext != 0 {
		t.Fatalf("unexpected downgraded/stats: %v %+v", downgraded, stats)
	}
}

// TestRemoveDowngradedRailsControllerRootsConsistency proves the BFS root-set
// filter matches the query semantics: a downgraded root loses only its
// ruby.rails_controller_action kind; if that was its only kind it drops out, but
// a root that also carries another kind stays a root.
func TestRemoveDowngradedRailsControllerRoots(t *testing.T) {
	roots := []CodeReachabilityRoot{
		{EntityID: "only-rails", RootKinds: []string{CodeRootKindRubyRailsControllerAction}},
		{EntityID: "multi-kind", RootKinds: []string{CodeRootKindRubyRailsControllerAction, "ruby.rails_callback_method"}},
		{EntityID: "unrelated", RootKinds: []string{"ruby.script_entrypoint"}},
	}
	downgraded := map[string]struct{}{"only-rails": {}, "multi-kind": {}}
	got := removeDowngradedRailsControllerRoots(roots, downgraded)

	if _, ok := findRoot(got, "only-rails"); ok {
		t.Fatalf("only-rails should be dropped from the BFS root set")
	}
	multi, ok := findRoot(got, "multi-kind")
	if !ok {
		t.Fatalf("multi-kind should stay a root via its other kind")
	}
	for _, k := range multi.RootKinds {
		if k == CodeRootKindRubyRailsControllerAction {
			t.Fatalf("multi-kind must lose the downgraded rails_controller_action kind, got %v", multi.RootKinds)
		}
	}
	if _, ok := findRoot(got, "unrelated"); !ok {
		t.Fatalf("unrelated root must be untouched")
	}
}

func findRoot(roots []CodeReachabilityRoot, entityID string) (CodeReachabilityRoot, bool) {
	for _, r := range roots {
		if r.EntityID == entityID {
			return r, true
		}
	}
	return CodeReachabilityRoot{}, false
}

func itoaN(prefix string, n int) string {
	digits := ""
	if n == 0 {
		digits = "0"
	}
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return prefix + digits
}
