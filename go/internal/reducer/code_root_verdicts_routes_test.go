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
