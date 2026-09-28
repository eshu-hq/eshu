// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// entityContextFallbackAnchor is the unlabeled pre-#7006 anchor. It matches an
// id on any label that carries an id property, so it is the exact-answer
// fallback after every indexed read misses, on both dialects.
const entityContextFallbackAnchor = "(e) WHERE e.id = $entity_id"

// neo4jContextAnchor is neo4jEntityContextAnchor's text, built once: the label
// sets never change at runtime and a constant text is what lets Neo4j plan it
// once and reuse the plan for every request.
var neo4jContextAnchor = buildNeo4jEntityContextAnchor()

// neo4jEntityContextAnchor returns the single Neo4j anchor clause that replaces
// the per-label loop (issue #7380). A cold Neo4j plans every distinct Cypher
// text a request sends, and the loop sent up to 16 (32 across the unscoped and
// scoped shapes) under one 10 s budget. This is one `CALL () { ... UNION ... }`
// (the form codemodel.Neo4jEntityIDAnchor uses, #7057; the empty scope clause
// needs the documented Neo4j 5.23 floor) that plans as one
// NodeUniqueIndexSeek per label:
//
//   - a uid branch over every EntityContextAnchorLabels entry with a schema uid
//     uniqueness constraint (graph.HasUIDUniquenessConstraint), anchored
//     `{uid: $entity_id}` with the id equality kept in a WHERE so it matches
//     only what the old `e.id = $entity_id` read matched (a File carries a uid
//     and no id and must not match), exactly as entityContextAnchors does;
//   - an id branch over the remaining entries that have a schema id uniqueness
//     constraint (graph.HasIDUniquenessConstraint: Repository, Workload,
//     WorkloadInstance).
//
// Both sets are derived from the schema tables, not hand-listed. A label with
// neither constraint (Directory, keyed by path; the canonical writer never
// sets Directory.id) has no index to seek, so it is left to the unlabeled
// fallback rather than paying a label scan on every request.
//
// The inline `{uid: $x}` map is what makes Neo4j seek: a plain
// `MATCH (e:A|B) WHERE e.id = $x` disjunction plans as UnionNodeByLabelsScan
// (docs/internal/evidence/7057-relationship-uid-anchor.md). The label
// disjunction is unsafe on NornicDB (#7006), so only the Neo4j dialect uses
// this clause.
//
// The loop stopped at the first label, in EntityContextAnchorLabels order,
// that had a row, and RunSingle takes the first row. A union has no such
// order, so each candidate is ranked by the position of its first label in
// that order and only the best-ranked one is kept (ORDER BY anchor_rank
// LIMIT 1). An id shared by two labels therefore still resolves to the label
// the loop tried first, deterministically. It also bounds the tail
// (the OPTIONAL MATCHes and the aggregation) to one anchor node.
//
// Measured cost (quiet 16-CPU amd64 host, load1 below 8 for every reported
// round; docs/internal/evidence/7380-entity-context-single-anchor.md): a miss
// or a late-label hit is about 6x to 10x faster with a warm JVM and a cleared
// plan cache (1209 to 190 ms and 1136 to 118 ms median) and about 2.2x after a
// restart (3169 to 1415 ms). A hit on the first label (Function) is slower
// while the anchor's plan is not cached: +41 ms paired mean (SD 9, n=10) with
// a warm JVM and a cleared plan cache, about +296 ms (SD 18, n=10) as the
// first query after a restart. With the plan cached the two are equal (12.1
// against 13.6 ms median). Neo4j keeps a plan until its statistics diverge
// (dbms.cypher.statistics_divergence_threshold, checked at most every
// dbms.cypher.min_replan_interval), so at most one request per caller shape
// pays that per plan epoch, and only label position 1 loses: a hit on the
// second label is already faster. With a warm JVM the extra cost vanishes once
// plans are cached, which points at planning the larger union; the
// planning-versus-first-execution split after a restart is not proven.
func neo4jEntityContextAnchor() string { return neo4jContextAnchor }

func buildNeo4jEntityContextAnchor() string {
	var uidLabels, idLabels, rankOrder []string
	for _, label := range EntityContextAnchorLabels {
		switch {
		case graph.HasUIDUniquenessConstraint(label):
			uidLabels = append(uidLabels, label)
			rankOrder = append(rankOrder, label)
		case graph.HasIDUniquenessConstraint(label):
			idLabels = append(idLabels, label)
			rankOrder = append(rankOrder, label)
		}
	}
	var branches []string
	if len(uidLabels) > 0 {
		branches = append(branches,
			"MATCH (e:"+strings.Join(uidLabels, "|")+" {uid: $entity_id}) WHERE e.id = $entity_id\n"+
				"\t\t\tRETURN e")
	}
	if len(idLabels) > 0 {
		branches = append(branches,
			"MATCH (e:"+strings.Join(idLabels, "|")+" {id: $entity_id})\n"+
				"\t\t\tRETURN e")
	}
	if len(branches) == 0 {
		return ""
	}
	quoted := make([]string, len(rankOrder))
	for i, label := range rankOrder {
		quoted[i] = strconv.Quote(label)
	}
	return "CALL () {\n\t\t\t" + strings.Join(branches, "\n\t\t\tUNION\n\t\t\t") + "\n\t\t}\n" +
		"\t\tWITH e, head([i IN range(0, " + strconv.Itoa(len(rankOrder)-1) + ") WHERE [" +
		strings.Join(quoted, ", ") + "][i] IN labels(e) | i]) AS anchor_rank\n" +
		"\t\tORDER BY anchor_rank\n" +
		"\t\tLIMIT 1"
}

// entityContextStatements returns GetEntityContext's anchor statements in try
// order for the request's graph backend and caller shape. The loop stops at the
// first statement that returns a row.
//
// On Neo4j it is two statements: the indexed CALL () anchor, then the
// unlabeled fallback (#7380). On NornicDB, and for the zero value, it is the
// per-label loop from entityContextAnchors, because a label disjunction or a
// many-branch UNION is unreliable there (#7006).
func (h *Handler) entityContextStatements(access querycontract.RepositoryAccessFilter) []string {
	if h.GraphBackend == querycontract.GraphBackendNeo4j {
		if anchor := neo4jEntityContextAnchor(); anchor != "" {
			return []string{
				entityContextStatement(anchor, access),
				entityContextCypher(entityContextFallbackAnchor, access),
			}
		}
	}
	anchors := entityContextAnchors()
	statements := make([]string, len(anchors))
	for i, anchor := range anchors {
		statements[i] = entityContextCypher(anchor, access)
	}
	return statements
}
