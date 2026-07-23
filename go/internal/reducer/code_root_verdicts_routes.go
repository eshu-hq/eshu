// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

const (
	// ReasonRouteUnreachable marks a #5494 downgrade: the root's ancestry is a
	// genuine, confirmed Rails controller (rubycontroller.Decide kept it), but
	// the repo's exact-only Rails route facts positively prove no route
	// targets this action, and the repo has zero detected dynamic/unresolved
	// route registrations anywhere (RubyRailsRouteFacts.HasUnmodeledRoutes is
	// false), so the exact route surface is provably complete. This is the
	// ONLY route-based downgrade reason; every other route outcome keeps.
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
// surface is exact-only (HasUnmodeledRoutes is false) AND was actually
// observed (HasAnyRouteEvidence is true) AND no route_entries handler matches
// this action -- the one case where the parser's exact route facts can be
// trusted as a COMPLETE negative. Every other outcome keeps: no data observed,
// an unmodeled/dynamic route present anywhere in the repo, or a positive
// handler match. This mirrors the #5376 ancestry walk's keep-biased shape --
// a downgrade requires positive, provably-complete evidence, never an absence.
func evaluateRouteLiveness(classContext, actionName string, routes RubyRailsRouteFacts) routeLivenessOutcome {
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
