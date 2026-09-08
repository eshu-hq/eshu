// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package querygraphrows shapes graph-driver result rows and Cypher
// projection fragments that a handler-family subpackage needs but that
// cannot live in querycontract.
//
// GraphPathNodeProps, RouteToCallerEntityFromChain, and their shared
// lastChainNodeProps helper decode a nodes(path) projection into a plain
// property map, branching on the neo4j-go-driver's neo4j.Node type.
// querycontract/AGENTS.md forbids importing a graph driver there -- it must
// stay dependency-neutral so any package can import it -- so this seam exists
// as the driver-aware home a handler family reaches instead. The package
// itself becomes a named exception to internal/query's query-no-graph-driver
// depguard rule (go/.golangci.yml), the same way
// internal/query/impact/exposure_path_mapping.go already is.
//
// GraphSemanticMetadataProjection lives here too, for a different reason: it
// returns a bare Cypher RETURN-clause fragment (a column projection list, no
// MATCH/RETURN of its own), and querycontract/AGENTS.md's Cypher-fragment
// carve-out is narrow and deliberate -- only the authorization seam
// (RepositoryAccessFilter's predicate builders and the inline-map grant
// primitives) may emit query text there. A field-projection fragment is not
// part of that seam, so it stays out of querycontract and rides along here
// instead of gaining a leaf of its own.
package querygraphrows
