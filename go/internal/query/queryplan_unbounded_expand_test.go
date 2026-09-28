// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// queryplanStarQuantifierPattern matches the length quantifier inside a
// relationship pattern in an expansion operator's Details, e.g. the
// "*2..6" in "(a)-[:CALLS*2..6]->(b)". Groups: lower, "..", upper.
var queryplanStarQuantifierPattern = regexp.MustCompile(`\[[^\]]*\*\s*(\d*)\s*(\.\.)?\s*(\d*)\s*\]`)

// queryplanBraceQuantifierPattern matches a quantified-path-pattern
// quantifier, e.g. the "{1, }" in "SHORTEST 2 (a) ((x)-[r]->(y)){1, } (b)".
// Groups: lower, upper.
var queryplanBraceQuantifierPattern = regexp.MustCompile(`\{\s*(\d*)\s*,\s*(\d*)\s*\}`)

// queryplanRelationshipPattern matches any relationship pattern in Details.
// One with no star is a fixed single hop: "*1..1" and "*1" print as
// "(a)-[rels]->(b)".
var queryplanRelationshipPattern = regexp.MustCompile(`\[[^\]]*\]`)

// queryplanUnboundedUpper is the planner's stand-in for "no upper bound": a
// pruning BFS over an unbounded pattern prints "*..2147483647" (Int32 max).
const queryplanUnboundedUpper = 2147483647

// queryplanUnboundedVarLength reports whether one plan operator expands a
// variable-length path with no upper bound. Only expansion operators count;
// Projection and friends print path expressions with a bare star even for
// bounded patterns. An expansion is unbounded when any quantifier in its
// details has no upper bound or an upper bound at Int32 max. A relationship
// pattern with no star is a fixed single hop and bounded. Details with no
// relationship pattern and no quantifier at all fail closed.
func queryplanUnboundedVarLength(operator, details string) bool {
	name, _, _ := strings.Cut(operator, "@")
	if !strings.HasPrefix(name, "Repeat") &&
		!strings.Contains(name, "VarLengthExpand") &&
		!strings.Contains(name, "VarExpand") &&
		!strings.Contains(name, "ShortestPath") {
		return false
	}
	found := false
	for _, match := range queryplanStarQuantifierPattern.FindAllStringSubmatch(details, -1) {
		found = true
		lower, dots, upper := match[1], match[2], match[3]
		if dots == "" && lower == "" {
			return true
		}
		if dots != "" && queryplanUpperUnbounded(upper) {
			return true
		}
	}
	for _, match := range queryplanBraceQuantifierPattern.FindAllStringSubmatch(details, -1) {
		found = true
		if queryplanUpperUnbounded(match[2]) {
			return true
		}
	}
	return !found && !queryplanRelationshipPattern.MatchString(details)
}

// queryplanUpperUnbounded reports whether an upper-bound token means "no
// upper bound": empty, unparseable, or at least Int32 max.
func queryplanUpperUnbounded(upper string) bool {
	if upper == "" {
		return true
	}
	value, err := strconv.ParseInt(upper, 10, 64)
	return err != nil || value >= queryplanUnboundedUpper
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
		// RETURN DISTINCT over an unbounded pattern plans a pruning BFS whose
		// upper bound is Int32 max: bounded in syntax, unbounded in fact.
		{"pruning BFS int32 max", "VarLengthExpand(Pruning,BFS,All)@neo4j", "(a)-[:CALLS*..2147483647]->(b)", true},
		{"unbounded shortest path", "ShortestPath@neo4j", "p = (a)-[anon_0:CALLS*]->(b)", true},
		{"bounded shortest path", "ShortestPath@neo4j", "p = (a)-[anon_0:CALLS*..8]->(b)", false},
		{"stateful shortest path", "StatefulShortestPath(All)@neo4j", "(a)-[anon_7:CALLS*]->(b)", true},
		// SHORTEST k over a quantified path pattern prints a brace quantifier
		// and no star.
		{
			"stateful shortest quantified", "StatefulShortestPath(Into, Trail)@neo4j",
			"SHORTEST 2 (a) ((`x`)-[`r`]->(`y`)){1, } (b)", true,
		},
		{
			"stateful shortest quantified from zero", "StatefulShortestPath(All, Trail)@neo4j",
			"SHORTEST 1 (a) ((`x`)-[`r`]->(`y`)){0, } (b)", true,
		},
		{
			"stateful shortest quantified bounded", "StatefulShortestPath(Into, Trail)@neo4j",
			"SHORTEST 2 (a) ((`x`)-[`r`]->(`y`)){1, 3} (b)", false,
		},
		{"repeat without upper", "Repeat(Trail)@neo4j", "(a) ((x)-[:CALLS]->(y)){1, } (b)", true},
		{"repeat with upper", "Repeat(Trail)@neo4j", "(a) ((x)-[:CALLS]->(y)){1, 3} (b)", false},
		// "*1..1" and "*1" print with no quantifier: a fixed single hop, which
		// is bounded. Production resource-path reads at depth 1 plan this way.
		{"fixed single hop", "VarLengthExpand(All)@neo4j", "(repo)-[rels]->(resource)", false},
		{"from zero to one", "VarLengthExpand(All)@neo4j", "(a)-[rels*0..1]->()", false},
		// Details with no relationship pattern and no quantifier cannot be
		// read, so the expansion fails closed.
		{"unreadable expansion details", "VarLengthExpand(All)@neo4j", "anon_3", true},
		// Projection renders path expressions with a bare star even for a
		// bounded {1,3} pattern. It is not an expansion and must be ignored.
		{"projection path expression", "Projection@neo4j", "length((a)-[anon_7*]->(b)) AS `length(p)`", false},
		{"single hop expand", "Expand(All)@neo4j", "(a)-[anon_0:CALLS]->(b)", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := queryplanUnboundedVarLength(tt.operator, tt.details); got != tt.unbounded {
				t.Fatalf("queryplanUnboundedVarLength(%q, %q) = %v, want %v", tt.operator, tt.details, got, tt.unbounded)
			}
		})
	}
}
