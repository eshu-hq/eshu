// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

// This file holds the bound-non-key verdict: the analyzer's positive answer
// that a statement only modifies already-matched nodes through SET assignments
// of properties no schema index covers (#11161). Such a statement can create
// no index key, oversized or otherwise, so the guard has nothing to measure
// and the sweep has nothing to flag.
//
// The verdict is explicit and plan-level. It is not a indexWritePlan.vars
// entry: vars keeps its documented meaning (only variables that write an
// indexed property), so the guard's zero-work fast path and per-row cost do
// not change for any statement.

// matchPatternProps returns the property names a MATCH node pattern's inline
// map names, or nil when the map is missing or its braces do not balance. A
// pattern it cannot read yields fewer properties, which only makes the verdict
// stricter.
func matchPatternProps(cypher string, openAt int) []string {
	if openAt < 0 {
		return nil
	}
	closeAt := matchingBrace(cypher, openAt)
	if closeAt < 0 {
		return nil
	}
	var props []string
	for _, entry := range splitTopLevel(cypher[openAt+1 : closeAt]) {
		if em := cypherMapEntryKey.FindStringSubmatch(entry); em != nil {
			props = append(props, em[1])
		}
	}
	return props
}

// classifyBoundNonKeyOnly reports whether every variable the statement writes
// is a node that a plain MATCH located by a full schema key and that the
// statement only modifies with simple var.prop = expr assignments of
// properties that appear in no schema key, in any position, for any of its
// labels. It is schema-derived: a future index over a stamped property
// disqualifies the statement without a code change here.
//
// It is false, so the statement keeps today's finding, when any written
// variable is also created by MERGE or CREATE, is bound by OPTIONAL MATCH or
// any non-MATCH clause, receives a map assignment (= or +=), a SET label, or an
// item the parse could not read, names no complete schema key in its MATCH, or
// carries no schema label. At least one such written variable must exist.
// The caller separately rejects a statement that SETs a variable no node
// pattern bound, and one with any unanalyzed write: an opaque SET item marks
// its variable opaque but not written, so this function alone cannot see it.
func classifyBoundNonKeyOnly(vars map[string]*indexWriteVar, order []string) bool {
	byLabel := SchemaIndexKeysByLabel()
	found := false
	for _, name := range order {
		v := vars[name]
		if !v.written {
			continue
		}
		if !v.matched || v.created || v.otherBound || v.opaque || v.setOther ||
			len(v.merges) > 0 || len(v.setProps) == 0 {
			return false
		}
		var keys [][]string
		keyed := false
		for _, label := range v.labels {
			for _, key := range byLabel[label] {
				keys = append(keys, key)
				if containsAll(v.matchProps, key) {
					keyed = true
				}
			}
		}
		if !keyed {
			return false
		}
		for _, prop := range v.setProps {
			if isKeyProperty(keys, prop) {
				return false
			}
		}
		found = true
	}
	return found
}

// containsAll reports whether values holds every element of want.
func containsAll(values, want []string) bool {
	for _, w := range want {
		if !containsString(values, w) {
			return false
		}
	}
	return true
}
