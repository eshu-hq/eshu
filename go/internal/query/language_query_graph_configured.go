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

// Graph-configured checks live in querycontract.GraphConfigured, the single
// home for the predicate this file historically spelled
// languageQueryGraphConfigured (mirrored once more by the supplychain hub;
// #6542 review). A family-local copy drifted by comment alone, so both
// callers use the shared leaf; do not reintroduce copies here.
