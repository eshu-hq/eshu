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
// The rewritten form inserts a LIMIT gate and RETURN clause:
//
//	WITH <drainVar> LIMIT $<batchParam>          (relationship-anchored MATCH)
//	  — or —
//	WITH <drainVar> ORDER BY elementId(<drainVar>) LIMIT $<batchParam>  (bare-label MATCH)
//	DETACH DELETE <drainVar>
//	RETURN count(<drainVar>) AS __drained
//
// NornicDB v1.1.9 requires different WITH clauses depending on the MATCH shape:
//   - Relationship-anchored queries (containing ")-[" in the MATCH line) must use
//     bare WITH <var> LIMIT; adding ORDER BY causes __drained=0 (no deletes).
//   - Bare-label queries (no relationship pattern) must use ORDER BY elementId(<var>)
//     before LIMIT; without ORDER BY, __drained=0 (no deletes).
//
// The shape is detected by scanning for ")-[" in the first MATCH line of the body.
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

	// Choose the WITH clause based on the MATCH shape.
	// Relationship-anchored queries contain ")-[" (e.g. (r)-[:REL]->(f)); bare-label
	// queries do not. NornicDB v1.1.9 treats these differently under WITH ... LIMIT:
	// anchored queries work without ORDER BY, bare-label queries require it.
	var withClause string
	if isRelationshipAnchored(body) {
		withClause = "WITH " + drainVar + " LIMIT $" + batchParam
	} else {
		withClause = "WITH " + drainVar + " ORDER BY elementId(" + drainVar + ") LIMIT $" + batchParam
	}

	rewritten := body +
		"\n" + withClause +
		"\nDETACH DELETE " + drainVar +
		"\nRETURN count(" + drainVar + ") AS __drained"

	return rewritten, nil
}

// isRelationshipAnchored reports whether the Cypher body contains a relationship
// pattern in its MATCH clause (i.e. ")-["), indicating a relationship-anchored
// query as opposed to a bare-label scan.
func isRelationshipAnchored(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		upper := strings.ToUpper(strings.TrimSpace(line))
		if strings.HasPrefix(upper, "MATCH") {
			return strings.Contains(line, ")-[")
		}
	}
	return false
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
