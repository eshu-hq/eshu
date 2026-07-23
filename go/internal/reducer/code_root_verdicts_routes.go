// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/rubycontroller"
)

const (
	// ReasonRouteUnreachable marks a #5494 downgrade: the root's ancestry is a
	// genuine, confirmed Rails controller (rubycontroller.Decide kept it), the
	// repo's Rails route facts contain no matching handler for this action, and
	// the repo has zero detected unmodeled/unresolved route registrations
	// anywhere (RubyRailsRouteFacts.HasUnmodeledRoutes is false) -- every route
	// registration the parser observed in every routes.draw block resolved
	// into an exact entry. This is the ONLY route-based downgrade reason;
	// every other route outcome keeps.
	ReasonRouteUnreachable = "route_unreachable"

	// RouteEvidenceNoData: the repo shows no observed Rails route evidence at
	// all (no routes.rb was parsed, or it registered zero routes and zero
	// dynamic-route calls). Inconclusive -- the reducer proved nothing about
	// routing, so the action stays exactly as #5376 ancestry left it.
	RouteEvidenceNoData = "no_route_data"
	// RouteEvidenceAmbiguous: the repo's routes.rb contains at least one
	// route registration the parser cannot resolve into an exact
	// "controller#action" (a resources/resource DSL macro, or an explicit
	// `to:` target such as a namespaced "admin/posts#show"). The exact
	// route_entries surface for this repo cannot be proven complete, so
	// EVERY controller action in the repo keeps -- the false-negative-safer
	// bias #5494 requires.
	RouteEvidenceAmbiguous = "unmodeled_routes_present"
	// RouteEvidenceRouted: an exact route_entries handler matches this action.
	RouteEvidenceRouted = "routed"
	// RouteEvidenceUnrouted: the repo's route surface is exact-only (no
	// unmodeled routes anywhere) and proves no route reaches this action --
	// the #5494 positive dead-route signal.
	RouteEvidenceUnrouted = "route_unreachable"
)

// RubyRailsRouteFacts is the repo-wide Rails route-fact snapshot #5494 loads
// alongside RubyClasses so BuildCodeRootVerdicts can join an ancestry-confirmed
// controller action against real route evidence instead of granting every
// structurally-valid action unconditional root status.
type RubyRailsRouteFacts struct {
	// RoutedHandlers is the set of exact "ClassName.action" handler strings
	// this repo's routes.rb registered via a literal, fully-resolved
	// `to: "controller#action"` Rails route -- the same handler shape the
	// Ruby parser's framework_routes.go and the HANDLES_ROUTE materialization
	// pipeline both use (rubyControllerClassName(controller) + "." + action).
	// A root matching one of these has positive route evidence.
	RoutedHandlers map[string]struct{}
	// HasUnmodeledRoutes is true when ANY route registration the parser
	// cannot resolve exactly -- a resources/resource DSL macro, or an
	// explicit `to:` target that did not parse into a clean unqualified
	// controller#action -- was observed anywhere in the repo's routes
	// configuration (parser signal: framework_semantics.rails.has_unmodeled_routes,
	// see internal/parser/ruby/framework_routes.go). Its PRESENCE disables the
	// #5494 downgrade repo-wide: RoutedHandlers cannot be proven complete
	// when it is true.
	HasUnmodeledRoutes bool
	// HasAnyRouteEvidence is true when this repo's routes.rb was observed and
	// parsed successfully: either RoutedHandlers is non-empty or
	// HasUnmodeledRoutes is true. False means no route data was observed for
	// this repo at all (routes.rb missing/unparsed, or a non-Rails repo) --
	// an entirely different, keep-biased outcome from "route data exists and
	// proves this action unrouted".
	HasAnyRouteEvidence bool
}

// routeLivenessOutcome is the #5494 result of evaluating one ancestry-confirmed
// Rails controller action root against RubyRailsRouteFacts.
type routeLivenessOutcome struct {
	downgrade     bool
	reason        string
	terminal      string
	routeEvidence string
}

// evaluateRouteLiveness joins an ancestry-CONFIRMED Rails controller action
// root (classContext#actionName) against the repo-wide Rails route facts
// #5494 loads alongside RubyClasses. It downgrades ONLY when the route
// surface is exactly-modeled (HasUnmodeledRoutes is false -- every call the
// parser observed inside every Rails.application.routes.draw block in the
// repo resolved into an exact route entry, per the fail-safe scan in
// internal/parser/ruby/framework_routes_ambiguity.go) AND was actually
// observed (HasAnyRouteEvidence is true) AND no route_entries handler matches
// this action OR any genuine ROUTING SUBCLASS that inherits it (see
// routingDescendantNames doc below). This is NOT a claim that the action is
// provably unreachable by every possible means (a mounted Rails engine's own
// gem-internal routes, for example, are invisible to any static analysis of
// this repo's source) -- it is a claim that nothing THIS repo's own routes.rb
// source registers, that the parser can see and could not resolve exactly,
// was left unaccounted for. Every other outcome keeps: no data observed, an
// unmodeled/dynamic route present anywhere in the repo, or a positive handler
// match (direct or inherited). This mirrors the #5376 ancestry walk's
// keep-biased shape -- a downgrade requires positive evidence from an
// exactly-modeled surface, never an unexamined absence.
//
// routingDescendantNames is the #5494 P1 fix (PR #5742 codex review): the
// SIMPLE (last-segment) names of every class transitively reachable from
// classContext's resolved class identity through the repo-wide subclass index
// (rubyRoutingDescendantNames), i.e. every genuine subclass. An action
// DEFINED on a base controller may be ROUTED only through a subclass that
// inherits it without overriding it -- `class BaseController; def health; end;
// end`, `class UsersController < BaseController; end`,
// `get "/health", to: "users#health"` -- the exact route set contains
// `UsersController.health`, never `BaseController.health` (the action is
// never redeclared on the routed class). Checking classContext.action alone
// would falsely downgrade BaseController#health even though it IS reachable
// via the routed subclass -- the exact false-positive #5494 exists to
// prevent. Each candidate name is checked as its own independent handler
// lookup, so a routing subclass rescues the base action but an unrelated
// class sharing only the action's NAME never does (no candidate is added
// unless the caller resolved it as a genuine subclass through the ancestry
// registry, never a name guess).
func evaluateRouteLiveness(classContext, actionName string, routes RubyRailsRouteFacts, routingDescendantNames []string) routeLivenessOutcome {
	if !routes.HasAnyRouteEvidence {
		return routeLivenessOutcome{routeEvidence: RouteEvidenceNoData}
	}
	if routes.HasUnmodeledRoutes {
		return routeLivenessOutcome{routeEvidence: RouteEvidenceAmbiguous}
	}
	handler := rubyRailsRouteHandlerKey(classContext, actionName)
	if handler == "" {
		// No action name to join on (e.g. a pre-#5494 loaded root): treat like
		// any other data gap rather than guessing a match or a miss.
		return routeLivenessOutcome{routeEvidence: RouteEvidenceNoData}
	}
	if _, routed := routes.RoutedHandlers[handler]; routed {
		return routeLivenessOutcome{routeEvidence: RouteEvidenceRouted}
	}
	for _, descendantName := range routingDescendantNames {
		descendantHandler := rubyRailsRouteHandlerKey(descendantName, actionName)
		if descendantHandler == "" {
			continue
		}
		if _, routed := routes.RoutedHandlers[descendantHandler]; routed {
			return routeLivenessOutcome{routeEvidence: RouteEvidenceRouted}
		}
	}
	return routeLivenessOutcome{
		downgrade:     true,
		reason:        ReasonRouteUnreachable,
		terminal:      "route_unreachable:" + handler,
		routeEvidence: RouteEvidenceUnrouted,
	}
}

// rubyRailsRouteHandlerKey builds the "ClassName.action" handler key, or ""
// when either half is blank. It matches the shape framework_routes.go's
// railsRouteHandler emits exactly, so a RubyRailsRouteFacts.RoutedHandlers set
// built from parser route_entries joins directly against a root's
// (ClassContext, ActionName) pair with no re-normalization.
func rubyRailsRouteHandlerKey(classContext, actionName string) string {
	classContext = strings.TrimSpace(classContext)
	actionName = strings.TrimSpace(actionName)
	if classContext == "" || actionName == "" {
		return ""
	}
	return classContext + "." + actionName
}

// newRubySubclassIndex builds a repo-wide map from a resolved qualified class
// key to every class that declares it as a base, resolved through the SAME
// ExactMatches/SuffixMatches candidate resolution rubycontroller.Decide uses
// for the superclass direction (#5494 P1 fix, PR #5742 codex review). Using
// the SAME trusted resolution the ancestry walk already relies on keeps this
// expansion exactly as precise in the "is this really a subclass" sense: an
// unrelated same-last-segment class never becomes a false subclass edge,
// because SuffixMatches requires a genuine segment-aligned suffix, not a
// name guess. A base ref that resolves ambiguously to more than one candidate
// (the same suffix-ambiguity the superclass walk already tolerates) adds
// EVERY candidate as a parent; that can only ever ADD a potential route match
// later (bias toward KEEP), never cause a wrong downgrade -- consistent with
// the "if you can't determine the subclass relationship, KEEP" requirement.
func newRubySubclassIndex(classes []RubyClassEntity, registry rubyRepoWideControllerRegistry) map[string][]string {
	children := make(map[string][]string)
	for _, class := range classes {
		qualified := rubyRegistryQualifiedName(class)
		if qualified == "" {
			continue
		}
		for _, base := range class.QualifiedBases {
			base = strings.TrimPrefix(strings.TrimSpace(base), "::")
			if base == "" {
				continue
			}
			for _, parent := range rubyUnionSortedKeys(registry.ExactMatches(base), registry.SuffixMatches(base)) {
				children[parent] = append(children[parent], qualified)
			}
		}
	}
	return children
}

// rubyUnionSortedKeys returns the deduplicated, sorted union of two class-key
// slices. Kept as a small local copy rather than importing rubycontroller's
// unexported equivalent (different package).
func rubyUnionSortedKeys(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, keys := range [][]string{a, b} {
		for _, key := range keys {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// rubyRoutingDescendantNames returns the SIMPLE (last-segment) names of every
// class transitively reachable from classKey through subclassIndex (exclusive
// of classKey itself), bounded by rubycontroller.MaxWalkDepth and cycle-safe
// -- mirroring the ancestry walk's own depth/cycle discipline so a
// pathological or generated inheritance chain cannot cause unbounded
// traversal. Simple names are what RubyRailsRouteFacts.RoutedHandlers keys on
// (parser route handlers are never namespace-qualified -- railsRouteHandler in
// internal/parser/ruby/framework_routes.go rejects any controller string
// containing "/"), so returning the last segment here lets
// evaluateRouteLiveness join directly with no re-normalization.
func rubyRoutingDescendantNames(classKey string, subclassIndex map[string][]string) []string {
	classKey = strings.TrimSpace(classKey)
	if classKey == "" || len(subclassIndex) == 0 {
		return nil
	}
	visited := map[string]struct{}{classKey: {}}
	names := make([]string, 0)
	queue := []string{classKey}
	for depth := 0; depth < rubycontroller.MaxWalkDepth && len(queue) > 0; depth++ {
		next := make([]string, 0)
		for _, key := range queue {
			for _, child := range subclassIndex[key] {
				if _, seen := visited[child]; seen {
					continue
				}
				visited[child] = struct{}{}
				names = append(names, rubyLastSegment(child))
				next = append(next, child)
			}
		}
		queue = next
	}
	return names
}
