// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

// GraphConfiguredReader is the optional interface a GraphQuery implementation
// may satisfy to report whether it actually has a live driver or session
// factory wired, distinct from being merely non-nil (*Neo4jReader implements
// this; see package query's neo4j.go). Declared as an optional interface,
// rather than adding the method to the GraphQuery port itself, so existing
// GraphQuery test fakes -- which construct a working or explicitly-nil reader
// and never model "non-nil but undriven" -- do not need to grow the method to
// keep compiling.
type GraphConfiguredReader interface {
	GraphConfigured() bool
}

// GraphConfigured reports whether reader is both non-nil and, when it opts
// into the GraphConfiguredReader interface, actually configured with a live
// driver or session factory. A reader that does not implement the interface
// is treated as configured whenever it is non-nil, preserving existing
// test-fake behavior (#5761 F1).
//
// Single home for the predicate root package query historically spelled
// languageQueryGraphConfigured and the supplychain hub spelled
// supplyChainGraphConfigured: the two were behavior-identical copies held in
// sync by comment, and a future edit to either side alone would silently
// change scoped-graph gating for one family only (#6542 review). Callers in
// both packages use this function; do not reintroduce family-local copies.
func GraphConfigured(reader GraphQuery) bool {
	if reader == nil {
		return false
	}
	if configurable, ok := reader.(GraphConfiguredReader); ok {
		return configurable.GraphConfigured()
	}
	return true
}
