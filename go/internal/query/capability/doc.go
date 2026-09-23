// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package capability serves the capability catalog: the read surface that
// answers which capabilities Eshu supports and at what truth ceiling for a
// given profile.
//
// Handler mounts GET /api/v0/capabilities and the MCP get_capability_catalog
// tool. It answers from the live registry that query/contract populates
// through init-time registration. lookup.go imports query/contract blank to
// link those registrations in; the rows themselves are read back through
// querycontract.CompatibilityCapabilityMatrix, so no contract symbol is
// named here.
//
// This package deliberately does NOT own capability registration. Every
// capability row, the register function that records it, and the support
// matrix all live in query/contract; see that package's registry.go. The
// split is by direction of dependency, not by name: a file called
// capability_matrix.go belongs to contract because it registers, while
// root's capability_keys.go stays in query because root's own routes name
// those ids.
//
// CatalogKey is the one capability id this package and the query root both
// name, so it is declared here and imported by root rather than duplicated.
package capability
