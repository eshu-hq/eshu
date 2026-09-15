// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// buildEdgeRouteStatements removes every pre-upgrade propertyless RUNS_ON
// identity before issuing the deterministic keyed MERGE. Group-capable
// production executors commit the cleanup and replacement atomically. The
// deployment upgrade contract stops every old reducer before keyed writers
// start, so an old writer cannot recreate a propertyless edge after cleanup.
func buildEdgeRouteStatements(cypher string, rows []map[string]any, batchSize int) []Statement {
	statements := make([]Statement, 0, 2)
	if cypher == batchCanonicalRunsOnUpsertCypher {
		statements = append(
			statements,
			buildBatchedStatements(batchCanonicalRunsOnLegacyIdentityCleanupCypher, rows, batchSize)...,
		)
	}
	return append(statements, buildBatchedStatements(cypher, rows, batchSize)...)
}
