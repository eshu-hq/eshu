// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"regexp"
	"strings"
)

// UnanchoredRetractTraversal names one relationship pattern in a retract or
// refresh statement whose selective, index-backed side sits on the RIGHT of the
// arrow while an unbound labelled node sits on the LEFT.
//
// Direction matters on NornicDB. A pattern written as
// `(:Directory)-[r:CONTAINS]->(f)` puts the unbound `(:Directory)` first, and
// the executor cold-plans it as a full Directory label scan with per-directory
// CONTAINS fanout re-joined to `f`. That is work proportional to the whole
// graph's Directory population on every input row, no matter how few rows the
// statement carries — which is why a 25-path retract chunk that took
// milliseconds against an empty graph exceeded a two-minute budget once the
// corpus was populated (issue #4207). Writing the same relationship anchored on
// the bound variable, `(f)<-[r:CONTAINS]-(:Directory)`, walks `f`'s own
// adjacency and matches the identical edges. The same rewrite was applied to
// the workload catalog queries for issues #3466 and #1731.
type UnanchoredRetractTraversal struct {
	// Anchor is the index-backed variable stranded on the right of the arrow.
	Anchor string
	// Pattern is the offending relationship pattern text.
	Pattern string
}

var (
	// matchClausePattern isolates MATCH / OPTIONAL MATCH clauses. MERGE and
	// CREATE patterns are excluded: their endpoints are already bound by a
	// preceding MATCH, so arrow order carries no scan cost there.
	matchClausePattern = regexp.MustCompile(`(?m)^\s*(?:OPTIONAL\s+)?MATCH\s+(.*)$`)

	// propertyAnchoredNodePattern matches a node that pins a property map and
	// binds a variable, e.g. `(f:File {path: file_path})`.
	propertyAnchoredNodePattern = regexp.MustCompile(`\((\w+):\w+\s*\{[^}]*\}\)`)

	// relationshipPattern splits `(left)-[rel:TYPE]->(right)` into its two node
	// texts. Only the left-to-right arrow form can strand an anchor on the
	// right; `<-` and undirected forms already read from the left-hand node.
	relationshipPattern = regexp.MustCompile(`(\([^()]*\))-\[[^\]]*\]->(\([^()]*\))`)

	// nodeVariablePattern extracts the binding variable from a node pattern.
	nodeVariablePattern = regexp.MustCompile(`^\((\w+)`)
)

// FindUnanchoredRetractTraversals reports every MATCH relationship pattern in
// cypher that expands left-to-right from an unbound labelled node into a node
// the statement anchors on an indexed property.
//
// Both the positive assertion over the production retract statements and the
// negative assertion over a known-bad statement call this function, so the
// regression test cannot pass against a re-implementation of the rule.
func FindUnanchoredRetractTraversals(cypher string) []UnanchoredRetractTraversal {
	anchored := make(map[string]struct{})
	for _, match := range propertyAnchoredNodePattern.FindAllStringSubmatch(cypher, -1) {
		anchored[match[1]] = struct{}{}
	}

	var findings []UnanchoredRetractTraversal
	for _, clause := range matchClausePattern.FindAllStringSubmatch(cypher, -1) {
		for _, rel := range relationshipPattern.FindAllStringSubmatch(clause[1], -1) {
			left, right := rel[1], rel[2]
			if nodeIsAnchored(left, anchored) {
				continue
			}
			if !nodeIsAnchored(right, anchored) {
				continue
			}
			findings = append(findings, UnanchoredRetractTraversal{
				Anchor:  nodeVariable(right),
				Pattern: strings.TrimSpace(rel[0]),
			})
		}
	}
	return findings
}

// nodeIsAnchored reports whether a node pattern can seed an index lookup:
// either it pins a property map inline, or it binds a variable that some other
// clause in the same statement pinned.
func nodeIsAnchored(node string, anchored map[string]struct{}) bool {
	if strings.Contains(node, "{") {
		return true
	}
	variable := nodeVariable(node)
	if variable == "" {
		return false
	}
	_, ok := anchored[variable]
	return ok
}

func nodeVariable(node string) string {
	match := nodeVariablePattern.FindStringSubmatch(node)
	if match == nil {
		return ""
	}
	return match[1]
}
