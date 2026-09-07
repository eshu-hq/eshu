// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package familyodu holds Ifá's family Odù fixtures: one file per
// materialized-edge family beside its guard, each exporting a constructor
// that returns a cataloged CatalogOdu. The fixtures moved here from
// go/internal/ifa when that package reached its directory file cap (#6594
// P1); keeping family fixtures in their own package restores the ratchet
// and leaves room for the remaining waived families to land beside their
// siblings instead of beside unrelated files.
//
// # What lives here and what does not
//
// This package owns the family scenario builders: the Odu and CatalogOdu
// envelope types (types.go), the shared wire-kind literals the git-content
// collector emits (fixturekinds.go), the cassette-envelope loader
// (cassette_envelopes.go), and one `*_family_odu.go` file per cataloged
// family. The per-family catalog registration (`*_family_catalog.go`),
// the catalog seed, coverage reconciliation, and canonicalization stay in
// the parent go/internal/ifa package, which re-exports this package's
// surface through family_fixture_compat.go so external callers observe no
// API break across the move.
//
// # Boundary
//
// This package must never import the parent ifa package: ifa imports
// familyodu, so the reverse edge is a cycle. Catalog files stay in the
// parent for the same reason catalog_seed.go calls the family constructors
// at registration time.
package familyodu
