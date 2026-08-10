// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "errors"

// errLanguageQueryGraphOnlyEntityUnavailable reports that the requested
// entity type has no content-store equivalent (graphLabelToContentEntityType
// returns "" only for Repository, Directory, and File) and the configured
// GraphQuery reader has no live graph backend to serve it -- either nil, or
// non-nil but undriven (NewNeo4jReader(nil, database), which wiring.go
// constructs unconditionally even in local_lightweight/ESHU_DISABLE_NEO4J
// mode; see Neo4jReader.GraphConfigured, #5761 F1). This is a capability-
// shaped failure, not a transient graph-read failure -- no retry or backend
// recovery changes the answer for these three labels under a graphless
// profile -- so handleLanguageQuery maps it to 501 unsupported_capability
// instead of routing it through WriteGraphReadError's 503/504 bounded-retry
// vocabulary.
var errLanguageQueryGraphOnlyEntityUnavailable = errors.New("language query entity type requires a graph backend")

// graphConfiguredReader is the optional interface a GraphQuery implementation
// may satisfy to report whether it actually has a live driver or session
// factory wired, distinct from being merely non-nil (*Neo4jReader implements
// this; see neo4j.go). Declared as an optional interface, rather than adding
// the method to the GraphQuery port itself, so existing GraphQuery test fakes
// -- which construct a working or explicitly-nil reader and never model
// "non-nil but undriven" -- do not need to grow the method to keep compiling.
type graphConfiguredReader interface {
	GraphConfigured() bool
}

// languageQueryGraphConfigured reports whether reader is both non-nil and,
// when it opts into the graphConfiguredReader interface, actually configured
// with a live driver or session factory. A reader that does not implement
// the interface is treated as configured whenever it is non-nil, preserving
// existing test-fake behavior (#5761 F1).
func languageQueryGraphConfigured(reader GraphQuery) bool {
	return graphQueryConfigured(reader)
}

// graphQueryConfigured reports whether reader is both non-nil and backed by
// a live driver or test session factory when it exposes that distinction.
func graphQueryConfigured(reader GraphQuery) bool {
	if reader == nil {
		return false
	}
	if configurable, ok := reader.(graphConfiguredReader); ok {
		return configurable.GraphConfigured()
	}
	return true
}
