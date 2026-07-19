// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// grandfatheredLanguageParityReadSurfaces is the closed digest ledger for
// pre-existing specs/language-feature-parity-ledger.v1.yaml rows whose
// read_surfaces label could not be resolved to a live consumer at #5335
// landing time, mirroring go/internal/queryplan/grandfathered_non_hot.go's
// landing mechanism. It is empty today: every label every language row
// claims resolves through languageParityReadSurfaceBacking to a real MCP
// tool or Go symbol, so no row needed grandfathering. The mechanism is kept
// so a future ledger row that genuinely cannot be classified at landing has
// a typed escape hatch instead of a silently-skipped label.
//
// Key is "<language>:<label>" for the specific unresolved instance. Value is
// languageParityRowDigest(entry.ReadSurfaces) for that language's exact
// read_surfaces list -- editing the row (adding, removing, or reordering any
// read_surfaces entry) changes the digest and un-grandfathers it, forcing the
// label back through real resolution.
var grandfatheredLanguageParityReadSurfaces = map[string]string{}

// grandfatheredFactKindReadSurfaces is the closed digest ledger for
// pre-existing specs/fact-kind-registry.v1.yaml family read_surface values
// that could not be matched to a live served route at #5335 landing time. It
// is empty today: every one of the 17 distinct literal "METHOD /path"
// read_surface values matches a live route. Literal routes have no
// legitimate reason to be unresolvable -- unlike an abstract label, there is
// no alias indirection to get wrong -- so a new entry here should be treated
// as a bug report, not routine landing friction.
//
// Key is the family name. Value is sha256(family's read_surface string);
// editing read_surface un-grandfathers it.
var grandfatheredFactKindReadSurfaces = map[string]string{}
