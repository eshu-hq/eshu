// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package edgetype is the single central registry of Cypher graph relationship
// (edge) types used across Eshu's graph writers, reducer materialization, and
// query read paths.
//
// Each EdgeType constant's string value is the exact Cypher relationship type
// emitted on the wire and MUST stay byte-identical to the historical literal it
// replaces. Both the NornicDB and Neo4j backends consume the same raw Cypher
// relationship type, so renaming a constant value is a breaking graph-contract
// change that silently invalidates stored edges and every reader matching the
// old type.
//
// Cypher does not allow parameterizing a relationship type (`-[:$x]->` is
// illegal on both backends), so writers necessarily weave the type into a
// compile-time Cypher template. Writers that build Cypher with fmt.Sprintf
// reference these constants directly; the remaining inline literals are bound to
// this registry by TestNoUnregisteredEdgeLiteral, which fails CI if any
// production Cypher names an edge type absent from the registry. Use
// IsRegistered to check membership and All to enumerate the registered set.
// Adding a constant is additive only when its owning writer, reader, and
// retraction contracts move in lockstep.
//
// Out of scope: data-driven edge-type families synthesized from collector row
// data at runtime rather than named in source — the AWS_* and GCP_* cloud
// relationship families (built as "AWS_"+raw / "GCP_"+raw in the
// internal/storage/cypher cloud resource edge writers) and the observability
// coverage family. These are open sets that cannot be enumerated as constants,
// and the enforcement test skips them deliberately.
package edgetype
