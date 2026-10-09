// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachabilitystore

// LoadCodeReachabilityRubyClasses, LoadCodeReachabilityRoots,
// LoadCodeReachabilityRailsRouteFacts, and LoadCodeReachabilityEdges expose
// CodeReachabilityStore's private loader methods to the live tests, which
// live in package reachabilitystore_test (they also need the root postgres
// package's live-DB test helpers, so they must be an external test package).
// Each takes the store as its first argument (a method-expression alias),
// e.g. LoadCodeReachabilityRoots(store, ctx, repoID). Production code never
// needs these outside the store itself.
var (
	LoadCodeReachabilityRubyClasses     = (*CodeReachabilityStore).loadCodeReachabilityRubyClasses
	LoadCodeReachabilityRoots           = (*CodeReachabilityStore).loadCodeReachabilityRoots
	LoadCodeReachabilityRailsRouteFacts = (*CodeReachabilityStore).loadCodeReachabilityRailsRouteFacts
	LoadCodeReachabilityEdges           = (*CodeReachabilityStore).loadCodeReachabilityEdges
)

// ListPendingCodeReachabilityInputsSQL exposes the loader's candidate
// statement to the live EXPLAIN proof and the #7547 live gate proofs in
// store_route_liveness_live_test.go, so they run the exact production text,
// including its seeded mutations.
const ListPendingCodeReachabilityInputsSQL = listPendingCodeReachabilityInputsSQL
