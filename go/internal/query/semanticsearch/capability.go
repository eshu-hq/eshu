// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// Support returns this family's capability contract: the per-profile truth
// ceiling and the minimum profile the route requires.
//
// LocalLightweightMax is nil, which means unsupported rather than degraded —
// that profile has no curated search-document index at all, so the handler's
// profile gate must be able to refuse the route outright instead of serving a
// lower-truth answer from a corpus that does not exist.
//
// This is the ONLY declaration of these values. Root package query's
// baseCapabilityMatrix registers the result for production, and this package's
// main_test.go registers it again for tests that cannot link root (#6060). An
// earlier version copied the five fields into main_test.go under a comment
// saying to keep them in sync; nothing enforced that comment, and flipping
// LocalLightweightMax to non-nil and RequiredProfile down to local_lightweight
// left this package's tests passing against a capability profile production no
// longer enforced. One declaration removes the copy the comment was policing.
//
// It is a function, not an exported var, and every call allocates its own truth
// levels rather than pointing at package-level ones. CapabilitySupport carries
// its ceilings as pointers, so a shared var would hand every caller — including
// root's production registration — write access to the same three ints:
// `*semanticsearch.Support.ProductionMax = ...` from anywhere would move the
// registered ceiling at runtime, long after the gate that validated it ran.
// RegisterCapabilities copies the struct by value, which copies the pointers,
// not what they point at. queryauth makes the same call one package over for
// the same reason, keeping its data-class slice unexported behind a function so
// no caller can append to the shared backing array. No writer exists today; the
// point is that this file is the template every later family move in #6053
// copies, so the shape has to be right before it is cloned.
//
// The three ceilings get separate variables on purpose. Returning three
// pointers to one local would leave them aliased inside the returned struct,
// so writing through any one of them would silently move the other two.
//
// The values themselves are pinned independently by a root test
// (semantic_search_capability_support_test.go), so editing them here without
// intending to reddens that test rather than silently changing what the route
// serves.
func Support() querycontract.CapabilitySupport {
	localAuthoritativeMax := querycontract.TruthLevelDerived
	localFullStackMax := querycontract.TruthLevelDerived
	productionMax := querycontract.TruthLevelDerived
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &localAuthoritativeMax,
		LocalFullStackMax:     &localFullStackMax,
		ProductionMax:         &productionMax,
		RequiredProfile:       querycontract.ProfileLocalAuthoritative,
	}
}
