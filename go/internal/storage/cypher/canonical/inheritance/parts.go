// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inheritance

// Part is one leaf-local retract statement part: the static Cypher template,
// its parameters, and the routing flag the parent cypher package maps onto
// Statement{Operation}. Part carries no Statement so this leaf never imports
// its parent (the root retract wrappers are the callers).
//
// The inheritance retract fans out to one part per child label (see
// InheritanceRetractParts): Drain-marking does not apply here — these parts
// run through the established retract dispatch, not the mixed
// structural-edges phase group.
type Part struct {
	// Cypher is the static template to execute.
	Cypher string
	// Parameters are the statement parameters.
	Parameters map[string]any
	// IsRetract is always true for this family's parts (parent maps to
	// OperationCanonicalRetract); it is carried explicitly so the root
	// mapping stays uniform with the sibling leaves.
	IsRetract bool
}
