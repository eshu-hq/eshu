// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package interproc solves interprocedural, cross-repo taint by reachability over
// a port graph: a node is a value position (a function's parameter, return, or a
// named slot for a captured closure variable or an object field), and an edge is
// a value flow. Cross-function call bindings (caller argument to callee
// parameter, callee return to caller result) and cross-repo edges are ordinary
// edges, so the same engine handles single-function, interprocedural, and
// cross-repo flows.
//
// Modeling closures and object fields as named-slot ports closes the two largest
// false-negative classes of summary-only taint engines: a value captured by a
// closure or stored in a field and read back flows through its named port like
// any other value.
//
// Taint propagates with the kind-set sanitizer model: a value carries a set of
// neutralized sink kinds, intersected where paths merge, and a sink of kind K is
// reported unless K is neutralized on every path reaching it. A sink may be a
// correlated cloud fact, which terminates a code-to-cloud reachability path.
// Findings carry confidence and provenance and are evidence, never canonical
// truth.
//
// The solver is a pure function of its Program. SolvePartitioned splits the
// program into weakly-connected components — the conflict key — and solves them
// concurrently; because taint cannot cross a component boundary and each
// component has its own state, this is race-free and needs no serialization, and
// its result is identical to the serial Solve.
package interproc
