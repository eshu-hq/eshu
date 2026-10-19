// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package node

// Part is one leaf-local edge statement part: the static Cypher template,
// its parameters, and the two routing flags the parent cypher package maps
// onto Statement{Operation, Drain}. Part carries no Statement so this leaf
// never imports its parent (the root family wrappers are the callers).
//
// Mirrors the canonical/flux leaf's Part type: the four structural families
// whose resolution lives here (atlantis, gitlab, helm-template-value,
// kustomize) share the same retract-before-upsert shape with Drain-marked
// retracts, so they share one part shape and one root mapping.
type Part struct {
	// Cypher is the static template to execute.
	Cypher string
	// Parameters are the statement parameters.
	Parameters map[string]any
	// IsRetract marks a stale-edge retraction (parent maps to
	// OperationCanonicalRetract); false marks an upsert.
	IsRetract bool
	// Drain marks a retract the NornicDB phase-group executor must run as
	// a standalone autocommit statement (parent maps to Statement.Drain).
	Drain bool
}
