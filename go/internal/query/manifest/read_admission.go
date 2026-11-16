// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package manifest

import (
	"context"
	"time"
)

// ManifestDependenciesReadAdmissionContractVersion versions the #9669 G2
// admission-contract shape for the repository manifest dependency read
// (GET /api/v0/repository/manifest-dependencies). Canonical reads persist
// through explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned manifest store
// satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this version
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
const ManifestDependenciesReadAdmissionContractVersion = "v0"

// ManifestDependenciesReadAdmission is the versioned admission-contract port
// the API handler will depend on instead of the ManifestDependencyStore
// handle directly. It re-declares exactly the one read the manifest
// dependency route admits through — the bounded page read through
// ListRepositoryManifestDependencyFacts, lookahead bound and refused signal
// included — so the boundary is held honest by the compile-time
// mirror/satisfier assertions below plus the frozen method-set contract
// test; it will back the eshu-api extraction handoff for this family. The
// seam is read-only on purpose: the one declared method is a bounded page
// read and the port carries no mutation surface, so a dest consumer
// implementing only this contract can serve manifest dependency reads but
// cannot acquire a write it did not bargain for through it. It lives in
// package manifest because the page and row types are package-local.
//
// G2-wave consumer: the G2 satellite leaf terminating this family's #9669
// MUST-TERMINATE rows. The satellite records this pin in a follow-up; a
// satellite PR consuming an unminted pin is invalid. Arbiter review of this
// pin precedes satellite termination of the manifest-dependencies leg.
//
// In scope: the repository manifest dependency read. The route admits its
// read through exactly this pin:
//
//   - GET /api/v0/repository/manifest-dependencies
//
// Narrow by construction, recorded not silently assumed: the method
// re-declares the staying-port signature whole in this
// manifest-dependencies-scoped shape instead of embedding
// ManifestDependencyStore or aliasing the store, so no other family can cite
// this pin — a blanket or shared-port pin is forbidden, and the widened
// shape needs arbiter review before satellite consumption. This is its own
// pin over the manifest dependency store. Citing the repository-story,
// MultiCloud drift, TerraformDrift, or IaC pins here is void: each family
// stands on its own re-declaration, and this pin adopts only the read named
// above.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own standing
// until a deliberate pin covers it):
//
//   - LoadContextSection stays unpinned here by construction: the bounded
//     repository-context section consumes the same store but is a different
//     read the list route never issues.
//   - The row mapping, ecosystem and scope normalization, and freshness
//     rendering are store construction, not port shape: there is no method
//     set to freeze.
//   - The grant checks, the fixed failure messages, the cursor and limit
//     bounds, the include-dev default, and QueryProfile are handler
//     behavior, not port shape: there is no method set to freeze.
//   - No constructor, selector, or wider store surface: the satellite
//     satisfies this port with its own wiring; this pin carries the read
//     shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type ManifestDependenciesReadAdmission interface {
	ListRepositoryManifestDependencyFacts(
		ctx context.Context,
		scopeID string,
		cursorObservedAt *time.Time,
		cursorFactID string,
		fetchLimit int,
		includeDev bool,
	) (ManifestDependencyPage, bool, error)
}

// The mirror assertion keeps the manifest-dependencies-scoped re-declaration
// honest in one direction: removal, rename, or re-signing of the mirrored
// method on the staying handler-facing port fails compilation here. A new
// method on that source port does not widen this pin — adoption takes
// deliberate re-declaration plus a frozen-list update. It pins no shared
// port — the assertion names ManifestDependencyStore only as the shape this
// pin mirrors, and no other family may cite this pin as coverage.
var _ ManifestDependenciesReadAdmission = interface {
	ManifestDependencyStore
}(nil)

// The satisfier assertion names the production adapter the wiring serves
// today: *PostgresManifestDependencyStore implements the pinned read, so the
// pin is satisfiable with the minimal shape, no wider store surface.
var _ ManifestDependenciesReadAdmission = (*PostgresManifestDependencyStore)(nil)
