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
// Two checks hold that invariant:
//
//   - [CheckWriters] reads recorded or literal Cypher and requires every
//     statement that writes a node id to name at least one label in [Labels].
//     It is fail-closed: an unlabeled variable, a label expression it cannot
//     reduce, an id key in a pattern it cannot place, or a dynamic
//     `SET n += <map>` on an uncovered label whose bound parameters do not
//     prove the map has no id key, is a finding.
//   - [CensusCypher] counts, over the whole graph, the id-bearing nodes the
//     anchor cannot reach. [Classify] is the reference definition the Cypher
//     must agree with; [EvaluateCensus] turns a [Census] into a gate verdict.
//
// The golden-corpus gate runs both against a replay, and the reducer runs the
// census on an interval as an operator signal. The package has no runtime
// state and emits no telemetry of its own.
package anchor
