// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "strings"

// SanitizeStatementParameters returns a copy of params with every diagnostic
// metadata key (any key prefixed with "_", such as StatementMetadataPhaseKey)
// removed, so only real Cypher parameters reach the driver.
//
// The canonical write path tags statements with `_eshu_*` metadata
// (annotateCanonicalWritePhases and the per-writer phase taggers) for grouping,
// ordering, and diagnostics. Those keys MUST be stripped before execution: they
// are not referenced by any Cypher template, and passing them to the backend as
// live parameters is both wasteful and, on NornicDB, actively harmful — an
// unreferenced parameter on a grouped `DETACH DELETE` can cause the delete to
// silently no-op. Every backend executor that sends a Statement to a driver
// MUST route its parameters through this function first.
//
// When params carries no diagnostic key the input map is returned unchanged to
// avoid an allocation on the hot path; callers must therefore treat the result
// as read-only.
func SanitizeStatementParameters(params map[string]any) map[string]any {
	if len(params) == 0 {
		return params
	}

	hasDiagnostics := false
	for key := range params {
		if strings.HasPrefix(key, "_") {
			hasDiagnostics = true
			break
		}
	}
	if !hasDiagnostics {
		return params
	}

	sanitized := make(map[string]any, len(params))
	for key, value := range params {
		if strings.HasPrefix(key, "_") {
			continue
		}
		sanitized[key] = value
	}
	return sanitized
}

// SanitizeStatement returns stmt with its parameters stripped of diagnostic
// metadata keys via SanitizeStatementParameters. The Cypher and Operation are
// unchanged. Use it on every statement immediately before handing it to a Bolt
// driver session.
func SanitizeStatement(stmt Statement) Statement {
	stmt.Parameters = SanitizeStatementParameters(stmt.Parameters)
	return stmt
}

// SanitizeStatements returns a new slice in which every statement has been run
// through SanitizeStatement, preserving order. The input slice is not mutated.
func SanitizeStatements(stmts []Statement) []Statement {
	sanitized := make([]Statement, len(stmts))
	for i, stmt := range stmts {
		sanitized[i] = SanitizeStatement(stmt)
	}
	return sanitized
}

// StatementsAllUseOperation reports whether stmts is non-empty and every
// statement carries the given Operation. Backend phase-group executors use it to
// detect an all-retract phase, which must run as sequential auto-commit
// statements rather than one managed transaction (NornicDB silently drops a
// grouped multi-statement `DETACH DELETE`).
func StatementsAllUseOperation(stmts []Statement, operation Operation) bool {
	if len(stmts) == 0 {
		return false
	}
	for _, stmt := range stmts {
		if stmt.Operation != operation {
			return false
		}
	}
	return true
}
