// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// TerraformStateResolversConfigured reports whether the #5443
// MATCHES_STATE ownership and config-match resolvers are wired on this
// writer. Both resolvers are optional at construction (see
// WithTerraformStateOwnershipResolver and WithTerraformStateConfigMatchResolver),
// which makes a missing resolver a silent degradation rather than a build or
// startup error: a nil ownership resolver means OwningRepoID never populates
// and terraformStateMatchesConfigEdgeStatements filters every row out, so no
// MATCHES_STATE edge is ever written and nothing signals that at runtime.
// cmd/* wiring-level tests type-assert their constructed projector.CanonicalWriter
// to *CanonicalNodeWriter and call this accessor to prove the deployed
// construction path actually attaches both resolvers, not just that the
// isolated adapter types behave correctly in unit tests.
func (w *CanonicalNodeWriter) TerraformStateResolversConfigured() (ownership, configMatch bool) {
	if w == nil {
		return false, false
	}
	return w.tfStateOwnershipResolver != nil, w.tfStateConfigMatchResolver != nil
}
