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
		wantMissingCtx int
	}{
		{
			name: "cross-file confirmed through intermediate to accepted base",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedBases: []string{"Admin::BaseController"}},
				{Name: "Admin::BaseController", QualifiedBases: []string{"ActionController::Base"}},
			}),
			wantVerdict:    CodeRootVerdictConfirmed,
			wantDowngraded: false,
		},
		{
			name: "cross-file downgraded resolves onward to ActiveRecord",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedBases: []string{"BaseController"}},
				{Name: "BaseController", QualifiedBases: []string{"ApplicationRecord"}},
				{Name: "ApplicationRecord", QualifiedBases: []string{"ActiveRecord::Base"}},
			}),
			wantVerdict:    CodeRootVerdictDowngraded,
			wantDowngraded: true,
		},
		{
			name: "reopened-class union reaches accepted base",
			input: func() CodeReachabilityProjectionInput {
				in := verdictInput("m:index", "OrdersController", []RubyClassEntity{
					// One reopening declares the real base, another declares none.
					{Name: "OrdersController", QualifiedBases: []string{"ActionController::Base"}},
					{Name: "OrdersController", QualifiedBases: nil},
				})
				return in
			}(),
			wantVerdict:    CodeRootVerdictConfirmed,
			wantDowngraded: false,
		},
		{
			name: "unresolved controller-suffixed gem base kept",
			input: verdictInput("m:index", "OrdersController", []RubyClassEntity{
				{Name: "OrdersController", QualifiedBases: []string{"SomeEngine::BaseController"}},
			}),
			wantVerdict:    CodeRootVerdictConfirmed,
			wantDowngraded: false,
		},
		{
			name: "collision conflict kept when any path confirms",
			input: verdictInput("m:list_users", "UsersController", []RubyClassEntity{
				{Name: "UsersController", QualifiedBases: []string{"BaseController"}},
				// BaseController reopened/colliding: one path to Rails, one to Thor.
				{Name: "BaseController", QualifiedBases: []string{"ActionController::Base"}},
				{Name: "BaseController", QualifiedBases: []string{"Thor"}},
			}),
			wantVerdict:    CodeRootVerdictConfirmed,
			wantDowngraded: false,
		},
		{
			name: "cycle is keep-biased for controller name",
			input: verdictInput("m:show", "FooController", []RubyClassEntity{
				{Name: "FooController", QualifiedBases: []string{"BarController"}},
				{Name: "BarController", QualifiedBases: []string{"FooController"}},
			}),
			wantVerdict:    CodeRootVerdictConfirmed,
			wantDowngraded: false,
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

// TestBuildCodeRootVerdictsDepthCapKept builds a chain longer than the shared
// walk's cap and proves the resulting verdict is confirmed (keep-biased), never
// a downgrade, even though the deep tail resolves to a non-controller base.
func TestBuildCodeRootVerdictsDepthCapKept(t *testing.T) {
	classes := []RubyClassEntity{{Name: "C0Controller", QualifiedBases: []string{"C1"}}}
	for i := 1; i <= 11; i++ {
		classes = append(classes, RubyClassEntity{Name: itoaN("C", i), QualifiedBases: []string{itoaN("C", i+1)}})
	}
	classes = append(classes, RubyClassEntity{Name: "C12", QualifiedBases: []string{"Sinatra::Base"}})
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
