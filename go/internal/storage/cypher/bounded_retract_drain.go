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
// For bare-label retracts the NornicDB phase-group executor first runs the
// read from BuildBoundedRetractProbeCypher and runs this drain only when the
// probe finds a node, because on NornicDB v1.3.3 a DETACH DELETE over a
// bare-label scan with property predicates costs a whole-store scan even when
// nothing matches (#6822).
//
// This rewrite is intentionally NornicDB-only: it is applied at execution time
// by nornicDBPhaseGroupExecutor, never by the shared cypher builder. The shared
// builder marks eligible statements with Drain=true and DrainVar so the executor
// knows which statements to rewrite.
func BuildBoundedRetractDrainCypher(cypher, drainVar, batchParam string) (string, error) {
	// The original statement must end with "DETACH DELETE <drainVar>" on its
	// own line; any other trailing verb means the wrong statement shape. The
	// body is the statement without that line, so the LIMIT gate can be
	// inserted between the WHERE block and the DELETE.
	if batchParam == "" {
		return "", fmt.Errorf("batchParam must not be empty")
	}
	body, err := boundedRetractBody(cypher, drainVar)
	if err != nil {
		return "", err
	}

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

// BuildBoundedRetractProbeCypher returns a read that reports whether a
// Drain-marked bare-label retract still matches any node (#6822). It returns
// ok=false, with no error, for relationship-anchored statements, whose
// single-statement drain is already bounded by the anchor.
//
// The probe keeps the MATCH and WHERE verbatim and ends with
//
//	WITH <drainVar> ORDER BY elementId(<drainVar>) LIMIT 1
//	RETURN elementId(<drainVar>) AS __id
//
// On NornicDB v1.3.3 a DETACH DELETE over a bare-label scan with property
// predicates costs a whole-store scan even when nothing matches, while this
// read stays bounded. The executor runs the probe first and only runs the
// single-statement drain from BuildBoundedRetractDrainCypher when the probe
// finds a node. The delete itself stays in that one statement, so its WHERE
// clause is rechecked atomically and a node another attempt refreshed to the
// current generation is never deleted. Keep the WITH ... LIMIT gate: a bare
// "RETURN ... LIMIT" over the same predicate costs a store-proportional scan.
func BuildBoundedRetractProbeCypher(cypher, drainVar string) (string, bool, error) {
	body, err := boundedRetractBody(cypher, drainVar)
	if err != nil {
		return "", false, err
	}
	if isRelationshipAnchored(body) {
		return "", false, nil
	}
	return body +
		"\nWITH " + drainVar + " ORDER BY elementId(" + drainVar + ") LIMIT 1" +
		"\nRETURN elementId(" + drainVar + ") AS __id", true, nil
}

// boundedRetractBody validates a Drain-marked retract and returns it without
// its trailing "DETACH DELETE <drainVar>" line.
func boundedRetractBody(cypher, drainVar string) (string, error) {
	if drainVar == "" {
		return "", fmt.Errorf("drainVar must not be empty")
	}
	trailer := "DETACH DELETE " + drainVar
	trimmed := strings.TrimRight(cypher, " \t\r\n")
	if !strings.HasSuffix(trimmed, trailer) {
		return "", fmt.Errorf(
			"cypher must end with %q to be eligible for bounded drain rewrite; got trailing: %q",
			trailer,
			lastLine(trimmed),
		)
	}
	return strings.TrimRight(trimmed[:len(trimmed)-len(trailer)], " \t\r\n"), nil
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
