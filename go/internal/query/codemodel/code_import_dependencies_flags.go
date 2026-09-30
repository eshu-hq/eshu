// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

// importCycleEdgeState is what an IMPORTS edge's flag properties say about
// whether the import can close a load-time cycle (#7346). The projector writes
// type_only, deferred, and inferred as explicit booleans on every edge (#7345),
// so a property that is missing or null means the edge was written before the
// flags existed. That is its own state, unknown, and never runtime: reading a
// null as false would count an import that may be type-only as proven runtime.
type importCycleEdgeState int

const (
	// edgeStateRuntime: all three flags were written and all are false.
	edgeStateRuntime importCycleEdgeState = iota
	// edgeStateUnknown: at least one flag property is missing or not a boolean.
	edgeStateUnknown
	// edgeStateInferred: the parser synthesized the import's source, so the edge
	// is a guess; a cycle it closes is ambiguous.
	edgeStateInferred
	// edgeStateTypeOnly: the import never runs. Excluded before the walk.
	edgeStateTypeOnly
	// edgeStateDeferred: the import runs at call time, not at load. Excluded
	// before the walk.
	edgeStateDeferred
)

// Public labels and flag states reported on a cycle and on each cycle edge.
const (
	// CycleLabelRuntime: every edge is a proven load-time import.
	CycleLabelRuntime = "runtime"
	// CycleLabelAmbiguous: at least one edge is inferred.
	CycleLabelAmbiguous = "ambiguous"
	// CycleLabelFlagsUnknown: no edge is inferred, and at least one predates the
	// import flags, so the cycle may include an import that never runs.
	CycleLabelFlagsUnknown = "flags_unknown"

	flagStateRuntime  = "runtime"
	flagStateInferred = "inferred"
	flagStateUnknown  = "unknown"
)

// CycleEdgeFlags counts the deduplicated import edges the cycle read saw, by
// flag class, before anchor filtering. It is disclosed as
// coverage.cycle_edge_flags so a client can tell how much of a result rests on
// unknown or inferred edges, and how many edges the flags removed.
type CycleEdgeFlags struct {
	// Considered is every deduplicated edge, before any exclusion.
	Considered int
	// TypeOnlyExcluded and DeferredExcluded are edges dropped before the walk.
	TypeOnlyExcluded int
	DeferredExcluded int
	// Inferred and Unknown are edges kept in the walk.
	Inferred int
	Unknown  int
}

// importCycleEdgeStateFromRow classifies one edge row from its three flag
// columns. The columns are projected raw, so a null arrives as a nil value. It
// deliberately does not use a lenient boolean helper: those turn nil into
// false, which would read a legacy edge as runtime.
func importCycleEdgeStateFromRow(row map[string]any) importCycleEdgeState {
	typeOnly, typeOnlyOK := row["type_only"].(bool)
	deferred, deferredOK := row["deferred"].(bool)
	inferred, inferredOK := row["inferred"].(bool)
	if !typeOnlyOK || !deferredOK || !inferredOK {
		return edgeStateUnknown
	}
	switch {
	case typeOnly:
		return edgeStateTypeOnly
	case deferred:
		return edgeStateDeferred
	case inferred:
		return edgeStateInferred
	default:
		return edgeStateRuntime
	}
}

// foldImportCycleEdgeState combines the states of two rows that share one edge
// key. The reader matches a Module by name while the writer keys it on
// (name, language), so two graph edges can reach the reader as one
// (file, module) pair. The edge stays as strong as its strongest proof: any
// runtime row wins, then unknown, then inferred, and only when every row is
// type-only or deferred is the edge excluded. It is a maximum over a total
// order, so the result does not depend on row order.
func foldImportCycleEdgeState(left, right importCycleEdgeState) importCycleEdgeState {
	if foldRank(left) >= foldRank(right) {
		return left
	}
	return right
}

func foldRank(state importCycleEdgeState) int {
	switch state {
	case edgeStateRuntime:
		return 5
	case edgeStateUnknown:
		return 4
	case edgeStateInferred:
		return 3
	case edgeStateDeferred:
		return 2
	default: // edgeStateTypeOnly
		return 1
	}
}

// hopRank orders edge states when several edges feed one collapsed file-to-file
// hop: runtime is the best proof of the hop, then inferred, then unknown.
func hopRank(state importCycleEdgeState) int {
	switch state {
	case edgeStateRuntime:
		return 3
	case edgeStateInferred:
		return 2
	default: // edgeStateUnknown; excluded states never reach a hop
		return 1
	}
}

// partitionImportCycleEdges drops the edges that cannot close a load-time cycle
// (type-only and deferred) and counts every class. Unknown edges stay: dropping
// them would hide real cycles, and exclusion needs proof.
func partitionImportCycleEdges(edges []importCycleEdge) ([]importCycleEdge, CycleEdgeFlags) {
	counts := CycleEdgeFlags{Considered: len(edges)}
	kept := make([]importCycleEdge, 0, len(edges))
	for _, edge := range edges {
		switch edge.state {
		case edgeStateTypeOnly:
			counts.TypeOnlyExcluded++
			continue
		case edgeStateDeferred:
			counts.DeferredExcluded++
			continue
		case edgeStateInferred:
			counts.Inferred++
		case edgeStateUnknown:
			counts.Unknown++
		case edgeStateRuntime:
			// Kept, and counted only in Considered.
		}
		kept = append(kept, edge)
	}
	return kept, counts
}

// importCycleFlagStateName is the public flag_state of a kept edge.
//
// Type-only and deferred edges are dropped before the walk, so they never reach a
// cycle; they return their own names so that a bug letting one through would show
// up in the output instead of reading as runtime.
func importCycleFlagStateName(state importCycleEdgeState) string {
	switch state {
	case edgeStateRuntime:
		return flagStateRuntime
	case edgeStateInferred:
		return flagStateInferred
	case edgeStateUnknown:
		return flagStateUnknown
	case edgeStateTypeOnly:
		return "type_only"
	case edgeStateDeferred:
		return "deferred"
	}
	return flagStateUnknown
}

// importCycleLabelFor labels a cycle by its weakest edge: ambiguous when any
// edge is inferred, else flags_unknown when any edge has no flag properties,
// else runtime.
func importCycleLabelFor(states []importCycleEdgeState) string {
	label := CycleLabelRuntime
	for _, state := range states {
		switch state {
		case edgeStateInferred:
			return CycleLabelAmbiguous
		case edgeStateUnknown:
			label = CycleLabelFlagsUnknown
		case edgeStateRuntime, edgeStateTypeOnly, edgeStateDeferred:
			// Neither weakens the label; the excluded states never reach a cycle.
		}
	}
	return label
}
