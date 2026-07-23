// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"
)

// verdictInputWithRoute builds a minimal projection input for the #5494 route
// liveness join: one ancestry-confirmed Ruby controller action root (base
// class is always ApplicationController, so ancestry always keeps) plus the
// repo-wide route facts to join against.
func verdictInputWithRoute(rootEntityID, classContext, actionName string, routes RubyRailsRouteFacts) CodeReachabilityProjectionInput {
	return CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepositoryID: "repo-1",
		Roots: []CodeReachabilityRoot{{
			EntityID:     rootEntityID,
			RootKinds:    []string{CodeRootKindRubyRailsControllerAction},
			ClassContext: classContext,
			ActionName:   actionName,
		}},
		RubyClasses: []RubyClassEntity{
			{Name: classContext, QualifiedName: classContext, QualifiedBases: []string{"ApplicationController"}},
		},
		RubyRoutes: routes,
		ObservedAt: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
	}
}

// TestBuildCodeRootVerdictsRouteLiveness is the #5494 failing-then-green
// fixture: an ancestry-confirmed Rails controller action with NO backing
// route must downgrade dead when the repo's route surface is exact-only and
// proven observed; an action WITH a route, an ambiguous (dynamic-route) repo,
// and a repo with no observed route data must all stay confirmed.
func TestBuildCodeRootVerdictsRouteLiveness(t *testing.T) {
	tests := []struct {
		name              string
		actionName        string
		routes            RubyRailsRouteFacts
		wantVerdict       string
		wantDowngraded    bool
		wantReason        string
		wantRouteEvidence string
	}{
		{
			name:              "no route data observed for repo keeps (inconclusive)",
			actionName:        "orphan",
			routes:            RubyRailsRouteFacts{},
			wantVerdict:       CodeRootVerdictConfirmed,
			wantReason:        "accepted",
			wantRouteEvidence: RouteEvidenceNoData,
		},
		{
			name:       "exact route matches this action: confirmed with positive route evidence",
			actionName: "index",
			routes: RubyRailsRouteFacts{
				HasAnyRouteEvidence: true,
				RoutedHandlers:      map[string]struct{}{"OrdersController.index": {}},
			},
			wantVerdict:       CodeRootVerdictConfirmed,
			wantReason:        "accepted",
			wantRouteEvidence: RouteEvidenceRouted,
		},
		{
			name:       "exact-only route surface with zero match downgrades dead",
			actionName: "orphan",
			routes: RubyRailsRouteFacts{
				HasAnyRouteEvidence: true,
				RoutedHandlers:      map[string]struct{}{"OrdersController.index": {}},
			},
			wantVerdict:       CodeRootVerdictDowngraded,
			wantDowngraded:    true,
			wantReason:        ReasonRouteUnreachable,
			wantRouteEvidence: RouteEvidenceUnrouted,
		},
		{
			name:       "dynamic/unmodeled routes anywhere in repo keeps every action",
			actionName: "orphan",
			routes: RubyRailsRouteFacts{
				HasAnyRouteEvidence: true,
				HasUnmodeledRoutes:  true,
				RoutedHandlers:      map[string]struct{}{"OrdersController.index": {}},
			},
			wantVerdict:       CodeRootVerdictConfirmed,
			wantReason:        "accepted",
			wantRouteEvidence: RouteEvidenceAmbiguous,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := verdictInputWithRoute("m:"+tt.actionName, "OrdersController", tt.actionName, tt.routes)
			rows, downgraded, _ := BuildCodeRootVerdicts(input)
			row, ok := verdictByEntity(rows, "m:"+tt.actionName)
			if !ok {
				t.Fatalf("expected a verdict row, got none (rows=%+v)", rows)
			}
			if row.Verdict != tt.wantVerdict {
				t.Fatalf("verdict = %q, want %q (basis=%+v)", row.Verdict, tt.wantVerdict, row.Basis)
			}
			if row.Basis.Reason != tt.wantReason {
				t.Fatalf("basis.reason = %q, want %q (basis=%+v)", row.Basis.Reason, tt.wantReason, row.Basis)
			}
			if row.Basis.RouteEvidence != tt.wantRouteEvidence {
				t.Fatalf("basis.route_evidence = %q, want %q (basis=%+v)", row.Basis.RouteEvidence, tt.wantRouteEvidence, row.Basis)
			}
			_, isDown := downgraded[row.EntityID]
			if isDown != tt.wantDowngraded {
				t.Fatalf("downgraded[%q] = %v, want %v", row.EntityID, isDown, tt.wantDowngraded)
			}
		})
	}
}

// TestBuildCodeRootVerdictsRootOnlyRoutedControllerKept is the P0 fix
// regression (coordinator review of #5494, head 26ba26d2d): a controller
// routed ONLY via Rails' `root "welcome#index"` shorthand -- which the parser
// cannot resolve into an exact route_entries handler, so it stamps
// has_unmodeled_routes=true instead of silently omitting it -- must stay
// CONFIRMED, never route-downgraded. Before the parser fix this construct set
// neither RoutedHandlers NOR HasUnmodeledRoutes, so WelcomeController#index
// would have been indistinguishable from a genuinely dead action and
// downgraded to route_unreachable -- a live controller called dead.
func TestBuildCodeRootVerdictsRootOnlyRoutedControllerKept(t *testing.T) {
	input := verdictInputWithRoute("m:index", "WelcomeController", "index", RubyRailsRouteFacts{
		HasAnyRouteEvidence: true,
		HasUnmodeledRoutes:  true,
		RoutedHandlers:      map[string]struct{}{},
	})
	rows, downgraded, stats := BuildCodeRootVerdicts(input)
	row, ok := verdictByEntity(rows, "m:index")
	if !ok {
		t.Fatalf("expected a verdict row, got none (rows=%+v)", rows)
	}
	if row.Verdict != CodeRootVerdictConfirmed {
		t.Fatalf("root-only-routed WelcomeController#index verdict = %q, want confirmed (basis=%+v)", row.Verdict, row.Basis)
	}
	if row.Basis.RouteEvidence != RouteEvidenceAmbiguous {
		t.Fatalf("root-only-routed WelcomeController#index route_evidence = %q, want %q", row.Basis.RouteEvidence, RouteEvidenceAmbiguous)
	}
	if _, isDown := downgraded[row.EntityID]; isDown {
		t.Fatalf("root-only-routed WelcomeController#index must not be downgraded, downgraded=%v", downgraded)
	}
	if stats.RouteDowngraded != 0 {
		t.Fatalf("stats.RouteDowngraded = %d, want 0", stats.RouteDowngraded)
	}
	kept := removeDowngradedRailsControllerRoots(input.Roots, downgraded)
	if _, ok := findRoot(kept, "m:index"); !ok {
		t.Fatalf("root-only-routed WelcomeController#index must remain a BFS root")
	}
}

// TestBuildCodeRootVerdictsAppendOnlyRoutedControllerKept is the P1 fix
// regression (same defect class as the P0 fix above, narrower trigger): a
// controller routed ONLY inside a `Rails.application.routes.append do ... end`
// block (a real, documented Rails API engines/gems use to insert routes after
// the main set) must stay CONFIRMED, never route-downgraded. Before the P1
// fix, isRailsRoutesDraw matched ONLY method=="draw", so a call inside an
// .append/.prepend block resolved neither into an exact route_entries
// handler (context never resolved to "rails") NOR into has_unmodeled_routes
// (the ambiguity scan was never even triggered for it) -- an
// append-only-routed action in an otherwise exact-only repo would have been
// silently downgraded to route_unreachable.
//
// At the reducer's abstraction level (RubyRailsRouteFacts), an append-only
// block that itself contains an unmodeled construct (e.g. `root`) produces
// EXACTLY the same input shape as the P0 root-only-routed case above --
// HasUnmodeledRoutes=true, no matching RoutedHandlers entry -- because the
// reducer never sees which Rails::RouteSet method (draw/append/prepend)
// produced the signal, only the aggregated repo-wide boolean. The NEW ground
// this test exercises is that the PARSER now actually sets that signal for an
// append-only block at all (proven directly in
// internal/parser/ruby/framework_routes_ambiguity_test.go's
// TestParseFlagsUnmodeledRoutesInsideAppendAndPrependBlocks); this test pins
// the reducer contract end of that same regression under an
// append-labeled name for traceability.
func TestBuildCodeRootVerdictsAppendOnlyRoutedControllerKept(t *testing.T) {
	input := verdictInputWithRoute("m:index", "EngineController", "index", RubyRailsRouteFacts{
		HasAnyRouteEvidence: true,
		HasUnmodeledRoutes:  true,
		RoutedHandlers:      map[string]struct{}{},
	})
	rows, downgraded, stats := BuildCodeRootVerdicts(input)
	row, ok := verdictByEntity(rows, "m:index")
	if !ok {
		t.Fatalf("expected a verdict row, got none (rows=%+v)", rows)
	}
	if row.Verdict != CodeRootVerdictConfirmed {
		t.Fatalf("append-only-routed EngineController#index verdict = %q, want confirmed (basis=%+v)", row.Verdict, row.Basis)
	}
	if row.Basis.RouteEvidence != RouteEvidenceAmbiguous {
		t.Fatalf("append-only-routed EngineController#index route_evidence = %q, want %q", row.Basis.RouteEvidence, RouteEvidenceAmbiguous)
	}
	if _, isDown := downgraded[row.EntityID]; isDown {
		t.Fatalf("append-only-routed EngineController#index must not be downgraded, downgraded=%v", downgraded)
	}
	if stats.RouteDowngraded != 0 {
		t.Fatalf("stats.RouteDowngraded = %d, want 0", stats.RouteDowngraded)
	}
	kept := removeDowngradedRailsControllerRoots(input.Roots, downgraded)
	if _, ok := findRoot(kept, "m:index"); !ok {
		t.Fatalf("append-only-routed EngineController#index must remain a BFS root")
	}
}

// verdictInputWithBaseAndSubclass builds a projection input for the #5494 P1
// fix (PR #5742 codex review): a rails_controller_action root defined on
// baseClass, plus a SEPARATE subclass declaring `< baseClass` (both
// ancestry-confirmed via ApplicationController), and the given route facts.
func verdictInputWithBaseAndSubclass(rootEntityID, baseClass, subclass, actionName string, routes RubyRailsRouteFacts) CodeReachabilityProjectionInput {
	return CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepositoryID: "repo-1",
		Roots: []CodeReachabilityRoot{{
			EntityID:     rootEntityID,
			RootKinds:    []string{CodeRootKindRubyRailsControllerAction},
			ClassContext: baseClass,
			ActionName:   actionName,
		}},
		RubyClasses: []RubyClassEntity{
			{Name: baseClass, QualifiedName: baseClass, QualifiedBases: []string{"ApplicationController"}},
			{Name: subclass, QualifiedName: subclass, QualifiedBases: []string{baseClass}},
		},
		RubyRoutes: routes,
		ObservedAt: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
	}
}

// TestBuildCodeRootVerdictsActionInheritedByRoutedSubclassKept is the #5494 P1
// fail-safe regression (PR #5742 codex review, code_root_verdicts_routes.go
// ~line 115): an action DEFINED on a base controller is routed only through a
// SUBCLASS that inherits it without overriding it --
// `class BaseController; def health; end; end`,
// `class UsersController < BaseController; end`,
// `get "/health", to: "users#health"`. The exact route surface contains
// `UsersController.health`, never `BaseController.health` (the action is
// never redeclared on the routed class), so a lookup keyed ONLY on the
// defining class's own name falsely finds no match and downgrades a
// genuinely-reachable base action to route_unreachable -- the exact
// false-positive #5494 exists to prevent. BaseController#health must stay
// CONFIRMED because its subclass UsersController (resolved through the SAME
// #5376/#5500 ancestry registry, not a name guess) routes the inherited
// action.
func TestBuildCodeRootVerdictsActionInheritedByRoutedSubclassKept(t *testing.T) {
	input := verdictInputWithBaseAndSubclass("m:health", "BaseController", "UsersController", "health", RubyRailsRouteFacts{
		HasAnyRouteEvidence: true,
		RoutedHandlers:      map[string]struct{}{"UsersController.health": {}},
	})
	rows, downgraded, stats := BuildCodeRootVerdicts(input)
	row, ok := verdictByEntity(rows, "m:health")
	if !ok {
		t.Fatalf("expected a verdict row, got none (rows=%+v)", rows)
	}
	if row.Verdict != CodeRootVerdictConfirmed {
		t.Fatalf("BaseController#health verdict = %q, want confirmed (basis=%+v)", row.Verdict, row.Basis)
	}
	if row.Basis.RouteEvidence != RouteEvidenceRouted {
		t.Fatalf("BaseController#health route_evidence = %q, want %q (basis=%+v)", row.Basis.RouteEvidence, RouteEvidenceRouted, row.Basis)
	}
	if _, isDown := downgraded[row.EntityID]; isDown {
		t.Fatalf("BaseController#health must not be downgraded, downgraded=%v", downgraded)
	}
	if stats.RouteDowngraded != 0 {
		t.Fatalf("stats.RouteDowngraded = %d, want 0", stats.RouteDowngraded)
	}
	kept := removeDowngradedRailsControllerRoots(input.Roots, downgraded)
	if _, ok := findRoot(kept, "m:health"); !ok {
		t.Fatalf("BaseController#health must remain a BFS root")
	}
}

// TestBuildCodeRootVerdictsBaseActionWithNoRoutingSubclassDowngrades proves the
// #5494 P1 fix does not over-broaden: a base-controller action with a
// subclass that exists but does NOT route it, in an otherwise exact-only
// route repo, must still downgrade -- a genuinely dead base action is not
// rescued merely because SOME subclass exists.
func TestBuildCodeRootVerdictsBaseActionWithNoRoutingSubclassDowngrades(t *testing.T) {
	input := verdictInputWithBaseAndSubclass("m:unused", "BaseController", "UsersController", "unused", RubyRailsRouteFacts{
		HasAnyRouteEvidence: true,
		RoutedHandlers:      map[string]struct{}{"UsersController.index": {}},
	})
	rows, downgraded, stats := BuildCodeRootVerdicts(input)
	row, ok := verdictByEntity(rows, "m:unused")
	if !ok {
		t.Fatalf("expected a verdict row, got none (rows=%+v)", rows)
	}
	if row.Verdict != CodeRootVerdictDowngraded {
		t.Fatalf("BaseController#unused verdict = %q, want downgraded (basis=%+v)", row.Verdict, row.Basis)
	}
	if row.Basis.Reason != ReasonRouteUnreachable {
		t.Fatalf("BaseController#unused reason = %q, want %q", row.Basis.Reason, ReasonRouteUnreachable)
	}
	if _, isDown := downgraded[row.EntityID]; !isDown {
		t.Fatalf("BaseController#unused must be downgraded, downgraded=%v", downgraded)
	}
	if stats.RouteDowngraded != 1 {
		t.Fatalf("stats.RouteDowngraded = %d, want 1", stats.RouteDowngraded)
	}
}

// TestRouteDowngradedRootRemovedFromBFSRootSet proves the #5494 downgrade
// flows through the SAME removeDowngradedRailsControllerRoots path #5376
// uses: a route-unreachable action loses its BFS root status exactly like an
// ancestry-downgraded one, so normal incoming-edge reachability (not blanket
// root grant) decides its fate downstream.
func TestRouteDowngradedRootRemovedFromBFSRootSet(t *testing.T) {
	input := verdictInputWithRoute("m:orphan", "OrdersController", "orphan", RubyRailsRouteFacts{
		HasAnyRouteEvidence: true,
		RoutedHandlers:      map[string]struct{}{"OrdersController.index": {}},
	})
	_, downgraded, stats := BuildCodeRootVerdicts(input)
	if len(downgraded) != 1 {
		t.Fatalf("expected exactly one downgraded entity, got %v", downgraded)
	}
	if stats.RouteDowngraded != 1 {
		t.Fatalf("stats.RouteDowngraded = %d, want 1", stats.RouteDowngraded)
	}
	kept := removeDowngradedRailsControllerRoots(input.Roots, downgraded)
	if _, ok := findRoot(kept, "m:orphan"); ok {
		t.Fatalf("route-unreachable action must be removed from the BFS root set")
	}
}
