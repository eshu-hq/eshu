// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"strings"
)

// BuildBoundedRetractDrainCypher rewrites an unbounded full-refresh retract
// statement into a bounded drain step for NornicDB execution.
//
// The original Cypher must end with exactly "DETACH DELETE <drainVar>".
// The rewritten form inserts:
//
//	WITH <drainVar> LIMIT $<batchParam>
//	DETACH DELETE <drainVar>
//	RETURN count(<drainVar>) AS __drained
//
// The caller drives a loop that repeats this until __drained == 0, ensuring
// the full prior-generation subgraph is deleted without a single unbounded
// transaction. The WHERE clause and all MATCH anchors are preserved verbatim.
//
// This rewrite is intentionally NornicDB-only: it is applied at execution time
// by nornicDBPhaseGroupExecutor, never by the shared cypher builder. The shared
// builder marks eligible statements with Drain=true and DrainVar so the executor
// knows which statements to rewrite.
func BuildBoundedRetractDrainCypher(cypher, drainVar, batchParam string) (string, error) {
	if drainVar == "" {
		return "", fmt.Errorf("drainVar must not be empty")
	}
	if batchParam == "" {
		return "", fmt.Errorf("batchParam must not be empty")
	}

	// The original statement must end with "DETACH DELETE <drainVar>" on its
	// own line (possibly with trailing whitespace). Any other trailing verb
	// means we are operating on the wrong statement shape and must not silently
	// produce wrong Cypher.
	trailer := "DETACH DELETE " + drainVar
	trimmed := strings.TrimRight(cypher, " \t\r\n")
	if !strings.HasSuffix(trimmed, trailer) {
		return "", fmt.Errorf(
			"cypher must end with %q to be eligible for bounded drain rewrite; got trailing: %q",
			trailer,
			lastLine(trimmed),
		)
	}

	// Strip the trailing DETACH DELETE line and replace it with the bounded
	// drain form. We cut just before the matched suffix so we can insert the
	// LIMIT gate between the WHERE block and the DELETE.
	body := strings.TrimRight(trimmed[:len(trimmed)-len(trailer)], " \t\r\n")

	rewritten := body +
		"\nWITH " + drainVar + " LIMIT $" + batchParam +
		"\nDETACH DELETE " + drainVar +
		"\nRETURN count(" + drainVar + ") AS __drained"

	return rewritten, nil
}

// lastLine returns the last non-empty line of s, used for error messages.
func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return s
}
