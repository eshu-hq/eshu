// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"regexp"
	"strings"
	"testing"
)

// queryplanVarLengthPattern matches the length quantifier inside a
// relationship pattern in an expansion operator's Details, e.g. the
// "*2..6" in "(a)-[:CALLS*2..6]->(b)". Groups: lower, "..", upper.
var queryplanVarLengthPattern = regexp.MustCompile(`\[[^\]]*\*\s*(\d*)\s*(\.\.)?\s*(\d*)\s*\]`)

// queryplanBoundedRepeatPattern matches a quantified-path-pattern repeat with
// a numeric upper bound, e.g. "{1, 3}".
var queryplanBoundedRepeatPattern = regexp.MustCompile(`\{\s*\d*\s*,\s*\d+\s*\}`)

// queryplanUnboundedVarLength reports whether one plan operator expands a
// variable-length path with no upper bound. Only expansion operators count;
// Projection and friends print path expressions with a bare star even for
// bounded patterns. A star with no "..", or with ".." and no upper number,
// is unbounded; an exact length ("*3") or any upper bound is bounded.
func queryplanUnboundedVarLength(operator, details string) bool {
	name, _, _ := strings.Cut(operator, "@")
	switch {
	case strings.HasPrefix(name, "Repeat"):
		return !queryplanBoundedRepeatPattern.MatchString(details)
	case strings.Contains(name, "VarLengthExpand"),
		strings.Contains(name, "VarExpand"),
		strings.Contains(name, "ShortestPath"):
	default:
		return false
	}
	for _, match := range queryplanVarLengthPattern.FindAllStringSubmatch(details, -1) {
		lower, dots, upper := match[1], match[2], match[3]
		if dots == "" && lower == "" {
			return true
		}
		if dots != "" && upper == "" {
			return true
		}
	}
	return false
}

// The details strings below were recorded from PROFILE on the pinned
// queryplan image (Neo4j 2026.05.0) and EXPLAIN on the compose pin (Neo4j
// 2026.08.1) for #7335. Neo4j has no operator named UnboundedExpand: the
// bound is visible only in the expansion operator's Details argument.
func TestQueryplanUnboundedVarLength(t *testing.T) {
	tests := []struct {
		name      string
		operator  string
		details   string
		unbounded bool
	}{
		{"star", "VarLengthExpand(All)@neo4j", "(a)-[:CALLS*]->(b)", true},
		{"star with variable", "VarLengthExpand(All)@neo4j", "(a)-[r:CALLS*]->()", true},
		{"incoming star", "VarLengthExpand(All)@neo4j", "(a)<-[:CALLS*]-(b)", true},
		{"lower bound only", "VarLengthExpand(All)@neo4j", "(a)-[:CALLS*2..]->(b)", true},
		{
			"quantified path with filter", "VarLengthExpand(All)@neo4j",
			"p = (a)-[anon_5:CALLS*]->(b) WHERE all(anon_10 IN relationships(p) WHERE NOT endNode(anon_10).uid = $autostring_0)", true,
		},
		{"upper bound", "VarLengthExpand(All)@neo4j", "(a)-[:CALLS*..4]->(b)", false},
		{"exact length", "VarLengthExpand(All)@neo4j", "(b)<-[:CALLS*3]-(a)", false},
		{"lower and upper", "VarLengthExpand(Pruning)@neo4j", "(a)-[:CALLS*2..6]->(b)", false},
		{"unbounded shortest path", "ShortestPath@neo4j", "p = (a)-[anon_0:CALLS*]->(b)", true},
		{"bounded shortest path", "ShortestPath@neo4j", "p = (a)-[anon_0:CALLS*..8]->(b)", false},
		{"stateful shortest path", "StatefulShortestPath(All)@neo4j", "(a)-[anon_7:CALLS*]->(b)", true},
		// Projection renders path expressions with a bare star even for a
		// bounded {1,3} pattern. It is not an expansion and must be ignored.
		{"projection path expression", "Projection@neo4j", "length((a)-[anon_7*]->(b)) AS `length(p)`", false},
		{"single hop expand", "Expand(All)@neo4j", "(a)-[anon_0:CALLS]->(b)", false},
		// Repeat(Trail) details were not observed on either pin, so the
		// check fails closed: a Repeat is unbounded unless it shows {n, m}.
		{"repeat without upper", "Repeat(Trail)@neo4j", "(a) ((x)-[:CALLS]->(y)){1, *} (b)", true},
		{"repeat with upper", "Repeat(Trail)@neo4j", "(a) ((x)-[:CALLS]->(y)){1, 3} (b)", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := queryplanUnboundedVarLength(tt.operator, tt.details); got != tt.unbounded {
				t.Fatalf("queryplanUnboundedVarLength(%q, %q) = %v, want %v", tt.operator, tt.details, got, tt.unbounded)
			}
		})
	}
}
