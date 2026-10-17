// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "strings"

// edgelessNodeGuard is the exact adjacency guard an edgeless-node reap
// carries. It is matched as a literal fragment, not a pattern family: the
// exception below exists for this one shape.
const edgelessNodeGuard = "NOT EXISTS { MATCH (n)-[]-() }"

// isEdgelessNodeReapStatement reports whether stmt is the edgeless-node
// reap the semantic writer emits after its dedicated edge retract (#8359):
// a parameter-bounded MATCH on writer-owned nodes, guarded by
// edgelessNodeGuard, ending in a plain DELETE of the node alone.
//
// The generic retract check in writer.go refuses it twice over — NOT reads
// as an open-ended predicate and the guard term names no parameter — and
// that refusal is correct for the general case. This shape is the
// exception because its match set cannot escape the parameter-bound node
// set: every term outside the guard is a bound membership or equality
// (verified by re-running hasParameterBoundedPredicate with the guard
// stripped), and the guard only narrows within that set. A node that
// becomes edgeless between a failed attempt and its replay — because the
// edge's owner retracted it meanwhile — is exactly what the reap exists to
// delete, and a node that gains an edge drops out of the set instead of
// losing the foreign edge. The reap therefore converges on replay in both
// directions, which is the premise AllStatementsAreReplaySafe protects.
//
// The exception stays narrow on purpose: the guard fragment is literal,
// the delete target must be the matched node variable alone, and every
// other retract requirement (retract operation, leading MATCH, no
// CREATE/SET/UNWIND/CALL/FOREACH) still applies. A DETACH DELETE is
// refused — it would take a racing foreign edge with the node, which is
// the failure the reap shape exists to prevent.
func isEdgelessNodeReapStatement(stmt Statement) bool {
	if stmt.Operation != OperationCanonicalRetract {
		return false
	}
	if !leadingMatchPattern.MatchString(stmt.Cypher) {
		return false
	}
	if nonIdempotentClausePattern.MatchString(stmt.Cypher) {
		return false
	}
	if !strings.Contains(stmt.Cypher, edgelessNodeGuard) {
		return false
	}
	if strings.Contains(strings.ToUpper(stmt.Cypher), "DETACH") {
		return false
	}
	if !strings.HasSuffix(strings.TrimSpace(stmt.Cypher), "DELETE n") {
		return false
	}
	stripped := strings.ReplaceAll(stmt.Cypher, "AND "+edgelessNodeGuard, "")
	return hasParameterBoundedPredicate(stripped)
}
