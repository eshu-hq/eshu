// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachabilitystore

// LoadCodeReachabilityRubyClasses, LoadCodeReachabilityRoots, and
// LoadCodeReachabilityRailsRouteFacts expose CodeReachabilityStore's private
// loader methods to store_route_liveness_live_test.go, which lives in
// package reachabilitystore_test (it also needs the root postgres package's
// live-DB test helpers, so it must be an external test package). Each takes
// the store as its first argument (a method-expression alias), e.g.
// LoadCodeReachabilityRoots(store, ctx, repoID). Production code never needs
// these outside the store itself.
var (
	LoadCodeReachabilityRubyClasses     = (*CodeReachabilityStore).loadCodeReachabilityRubyClasses
	LoadCodeReachabilityRoots           = (*CodeReachabilityStore).loadCodeReachabilityRoots
	LoadCodeReachabilityRailsRouteFacts = (*CodeReachabilityStore).loadCodeReachabilityRailsRouteFacts
)
