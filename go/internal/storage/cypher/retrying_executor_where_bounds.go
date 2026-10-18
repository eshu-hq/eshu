// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"regexp"
	"strings"
)

var whereClausePattern = regexp.MustCompile(`(?is)\bWHERE\b(.*?)(?:\bDETACH\s+DELETE\b|\bDELETE\b|\bREMOVE\b|\bRETURN\b|\z)`)

// predicateSeparatorPattern splits a WHERE body into terms. OR as well as AND,
// because an OR term WIDENS the set and would otherwise pass as one term
// holding a $param. MATCH and WHERE too: whereClausePattern's non-greedy body
// runs to the first write clause and so SWALLOWS a second `MATCH ... WHERE`,
// and splitting makes that second predicate its own term.
var predicateSeparatorPattern = regexp.MustCompile(`(?i)\b(AND|OR|MATCH|WHERE)\b`)

// everyConjunctIsBounded requires every term of every WHERE to name a parameter.
//
// A predicate can name a parameter and still read mutable graph state:
// `WHERE n.repo_id IN $repo_ids AND n.stale` passes the membership and
// open-ended checks in hasParameterBoundedPredicate. The delete stays inside the
// key space $repo_ids enumerates -- smaller blast radius than the complement
// case -- but within it the set still moves: a concurrent writer flipping
// n.stale between the failed attempt and the replay puts a node in range the
// first attempt never saw. Same broken premise.
//
// A literal comparison is NOT bounding: `n.stale = true` reads the same mutable
// property as the bare `n.stale`. Only a $param names a value the caller fixed
// for the statement. Every retract here today is a conjunction of bound terms.
func everyConjunctIsBounded(cypher string) bool {
	clauses := whereClausePattern.FindAllStringSubmatch(cypher, -1)
	if len(clauses) == 0 {
		// No WHERE to verify: a $param elsewhere says nothing about the set.
		return false
	}
	for _, where := range clauses {
		for _, term := range predicateSeparatorPattern.Split(where[1], -1) {
			if strings.TrimSpace(term) == "" {
				continue
			}
			if !strings.Contains(term, "$") {
				return false
			}
		}
	}
	return true
}
