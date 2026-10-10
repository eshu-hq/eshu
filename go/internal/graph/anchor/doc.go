// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package anchor defines which graph nodes the Neo4j entity-context anchor can
// reach, and the checks that keep that set closed (#7212).
//
// The Neo4j anchor statement of GET /api/v0/entities/{id}/context seeks a node
// through a uid-constrained label with uid equal to id, or through an
// id-constrained label by id alone. A node with an id that sits on no such
// label is invisible to it. The unlabeled fallback statement used to find such
// a node with an AllNodesScan; keeping that scan off the miss path depends on
// no writer producing the node in the first place.
//
// Two parts hold that invariant:
//
//   - [CensusCypher] counts, over the whole graph, the id-bearing nodes the
//     anchor cannot reach. [Classify] is the reference definition the Cypher
//     must agree with; [EvaluateCensus] turns a [Census] into a gate verdict.
//     This count is the authority. It runs as the required graph/anchor_census
//     check on the Neo4j legs after the golden-corpus replay, and as the
//     reducer's gauge on each deployment.
//   - [CheckWriters] is a heuristic pre-filter over Cypher text, recorded or
//     literal. It reports a node-id write whose node carries no label in
//     [Labels], for the shapes its test rows cover, which the package README
//     names beside the test functions. It is not a full Cypher parser. The
//     README also lists its known blind spots (not exhaustive); the census is
//     the backstop for them.
//
// The package has no runtime state and emits no telemetry of its own.
package anchor
