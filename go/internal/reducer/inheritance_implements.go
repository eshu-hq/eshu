// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// implementerEntityTypes are the entity types that may implement an interface.
// Interface implementation is an explicit-keyword relationship; Go's structural
// implements is intentionally out of this slice (gap analysis #2228).
var implementerEntityTypes = map[string]struct{}{
	"Class":  {},
	"Struct": {},
	"Enum":   {},
}

// interfaceLikeEntityTypes are the entity types that can be the target of an
// IMPLEMENTS edge.
var interfaceLikeEntityTypes = map[string]struct{}{
	"Interface": {},
	"Protocol":  {},
}

// inheritancePayloadImplementedInterfaces reads the parser-emitted
// implemented_interfaces metadata that flows through the content-entity snapshot
// (issue #2229). It mirrors inheritancePayloadBases.
func inheritancePayloadImplementedInterfaces(payload map[string]any) []string {
	return semanticPayloadMetadataStringSlice(payload, "implemented_interfaces")
}
