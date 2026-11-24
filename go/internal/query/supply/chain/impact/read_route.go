// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// ReadRoute names the read model one impact call is answered from (#10880 PR-B2a,
// arbiter ruling V). One function, routeFor, decides it; every store method that
// issues the read and every method that reports its freshness takes the route from
// it, so a call can never read one model and disclose another's freshness.
type ReadRoute string

const (
	// RouteLegacy reads the active finding facts at read time. It serves only
	// calls made with the winners gate off.
	RouteLegacy ReadRoute = "legacy"
	// RouteWinners reads the maintained canonical winners table: an unscoped
	// call with no existence filter.
	RouteWinners ReadRoute = "winners"
	// RouteWinnersSupplement reads the winners table plus the per-support
	// supplement, in one statement: an unscoped profile=precise call. It reports
	// the winners watermark and also requires the supports readiness predicate.
	RouteWinnersSupplement ReadRoute = "winners_supplement"
	// RouteSupportsGrant reads the per-support table driven from the scope or
	// repository grant. Current by construction once the readiness predicate holds.
	RouteSupportsGrant ReadRoute = "supports_grant"
	// RouteSupportsImage reads the per-support table driven from the image_ref
	// partial index: an unscoped call with an image reference. Current by
	// construction once the readiness predicate holds.
	RouteSupportsImage ReadRoute = "supports_image"
	// RouteSupportsFindingID is the explain by public finding id of an ungranted
	// caller: the per-support table probed by its finding id index. Only explain
	// calls take it.
	RouteSupportsFindingID ReadRoute = "supports_finding_id"
	// RouteSupportsSelector is the explain selector or identity lookup of an
	// ungranted caller without an image reference: the per-support table read whole,
	// driven by the selector columns' btrees (#10880 PR-C1 f). Only explain calls
	// take it.
	RouteSupportsSelector ReadRoute = "supports_selector"
)

// routeFor decides the read model of one call. With the winners gate on, no call
// reads the legacy facts. A grant wins over every other filter (the winners row is
// one pick across scopes and cannot answer a grant); an image reference comes next
// (it is a per-support column, so the winners row cannot answer it); precise is the
// last existence filter (a winner that passes it is the answer for its key, and the
// supplement recovers the keys whose winner fails it); everything else reads the
// winners table unchanged.
func routeFor(gate, granted bool, imageRef, profile string) ReadRoute {
	switch {
	case !gate:
		return RouteLegacy
	case granted:
		return RouteSupportsGrant
	case imageRef != "":
		return RouteSupportsImage
	case profile == ProfilePrecise:
		return RouteWinnersSupplement
	default:
		return RouteWinners
	}
}

// readsSupports reports whether the route reads the per-support table and so must
// pass the supports readiness predicate first.
func (r ReadRoute) readsSupports() bool {
	return r == RouteSupportsGrant || r == RouteSupportsImage || r == RouteWinnersSupplement
}

// servesWinners reports whether the route reads the winners table, so its answer is
// bounded by the winners watermark.
func (r ReadRoute) servesWinners() bool {
	return r == RouteWinners || r == RouteWinnersSupplement
}

// route returns the read model one list call is answered from.
func (f FindingFilter) route(gate bool) ReadRoute {
	return routeFor(gate, len(f.AllowedRepositoryIDs) > 0 || len(f.AllowedScopeIDs) > 0, f.ImageRef, f.DetectionProfile)
}

// route returns the read model one aggregate call is answered from.
func (f AggregateFilter) route(gate bool) ReadRoute {
	return routeFor(gate, len(f.AllowedRepositoryIDs) > 0 || len(f.AllowedScopeIDs) > 0, f.ImageRef, f.DetectionProfile)
}
