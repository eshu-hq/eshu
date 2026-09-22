// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package chain declares the supply-chain fact families: OCI registry
// evidence (oci_registry.go), language package-registry evidence
// (package_registry.go), SBOM documents and in-toto/SLSA attestations
// (sbom_attestation.go), vulnerability intelligence from upstream advisory
// sources (vulnerability_intelligence.go), and operator-declared or
// provider-dismissed vulnerability suppressions (vulnerability_suppression.go).
//
// The families moved out of go/internal/facts in issue #6776 with no
// identifier renamed: none of them stuttered against the package name, so
// every exported name is unchanged from its facts-root declaration and
// facts.X and chain.X are the same constant. The facts root's
// compat_supply_chain.go aliases them for callers that still spell them
// facts.X.
//
// Each family file declares that family's fact-kind constants, its
// schema-version constants, a <Family>FactKinds accessor returning the
// kinds in collector emission order, and a <Family>SchemaVersion lookup
// returning the version core accepts for one kind. The accessors return
// copies: mutating a returned slice cannot perturb the package's ordering,
// and a lookup for a kind the family does not own reports false rather than
// an empty version. go/internal/facts/schema_version.go's
// schemaVersionFamilies table dispatches SchemaVersion,
// ClassifySchemaVersion, and ValidateSchemaVersion through these accessors,
// and it panics at init if two families claim one kind.
//
// This is a leaf declaration package: constants, ordering slices, and
// lookup maps only. It performs no I/O, holds no collector or reducer
// logic, and admits nothing. Suppression in particular is declarative here
// — VulnerabilitySuppressionSourcePolicy and its siblings name where a
// suppression came from, while the authority decision that acts on it lives
// in the query and reducer surfaces.
//
// The package must never import go/internal/facts: the root imports the
// nested families, so the reverse edge is an import cycle.
package chain
