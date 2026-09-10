// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// AnchorLabelDisjunction is the label set the Neo4j-compat call-chain
// builder seeds its start/end anchors with. It mirrors the authoritative
// CALLS-source label set the canonical edge writer projects (see
// codeCallRetractSourceLabels in storage/cypher), so every node reachable
// as a call-chain endpoint still resolves while the planner seeds from a
// label/index scan instead of an all-node scan. The prior unlabeled
// `MATCH (start)` / `MATCH (end)` gave the Neo4j planner no label to anchor on,
// so the id/name predicate forced a full-graph scan (issue #3567). NornicDB has
// its own builder (BuildNornicDBCallChainCypher) and is intentionally untouched.
const AnchorLabelDisjunction = "Function|Class|Struct|Interface|TypeAlias|File"

// BuildCallChainCypher renders the Neo4j-compat shortestPath call-chain
// read for req, binding the request's own repository scope and the
// caller's grant before the LIMIT.
func BuildCallChainCypher(
	req Request,
	backend querycontract.GraphBackend,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{}
	predicates := make([]string, 0, 6)

	if backend == querycontract.GraphBackendNornicDB {
		return BuildNornicDBCallChainCypher(req, access)
	}

	if strings.TrimSpace(req.StartEntityID) != "" {
		params["start_entity_id"] = strings.TrimSpace(req.StartEntityID)
		predicates = append(predicates, codemodel.GraphEntityIDPredicate("start", "$start_entity_id"))
	} else {
		params["start"] = strings.TrimSpace(req.Start)
		predicates = append(predicates, "start.name = $start")
	}

	if strings.TrimSpace(req.EndEntityID) != "" {
		params["end_entity_id"] = strings.TrimSpace(req.EndEntityID)
		predicates = append(predicates, codemodel.GraphEntityIDPredicate("end", "$end_entity_id"))
	} else {
		params["end"] = strings.TrimSpace(req.End)
		predicates = append(predicates, "end.name = $end")
	}

	if req.CrossRepo {
		params["start_repo_id"] = strings.TrimSpace(StartRepoID(&req))
		params["end_repo_id"] = strings.TrimSpace(EndRepoID(&req))
		params["traversal_repo_ids"] = AllowedTraversalRepoIDs(&req)
		predicates = append(predicates, "start.repo_id = $start_repo_id", "end.repo_id = $end_repo_id")
	} else if strings.TrimSpace(req.RepoID) != "" {
		params["repo_id"] = strings.TrimSpace(req.RepoID)
		predicates = append(predicates, "start.repo_id = $repo_id", "end.repo_id = $repo_id")
	}
	if access.Scoped() {
		params = access.GraphParams(params)
		predicates = append(predicates,
			access.GraphConditionOnProperty("start", "repo_id"),
			access.GraphConditionOnProperty("end", "repo_id"),
		)
	}

	var cypher strings.Builder
	cypher.WriteString("\n\t\tMATCH (start:" + AnchorLabelDisjunction + ")\n")
	cypher.WriteString("\t\tMATCH (end:" + AnchorLabelDisjunction + ")")
	if len(predicates) > 0 {
		cypher.WriteString("\n\t\tWHERE ")
		cypher.WriteString(strings.Join(predicates, " AND "))
	}
	cypher.WriteString("\n\t\tMATCH path = shortestPath(\n")
	cypher.WriteString("\t\t\t(start)-[:CALLS*1..")
	fmt.Fprint(&cypher, req.MaxDepth)
	cypher.WriteString("]->(end)\n")
	cypher.WriteString("\t\t)\n")
	if hops := PathHopPredicates(req, access); len(hops) > 0 {
		cypher.WriteString("\t\tWHERE all(node IN nodes(path) WHERE " + strings.Join(hops, " AND ") + ")\n")
	}
	if backend == querycontract.GraphBackendNornicDB {
		// NornicDB resolves this path correctly with raw nodes(path) results,
		// while its inline list projection returns null today.
		cypher.WriteString("\t\tRETURN nodes(path) as chain,\n")
	} else {
		cypher.WriteString("\t\tRETURN [node IN nodes(path) | {id: coalesce(node.id, node.uid), name: node.name, labels: labels(node), language: node.language, docstring: node.docstring, method_kind: node.method_kind}] as chain,\n")
	}
	cypher.WriteString("\t\t       length(path) as depth\n")
	cypher.WriteString("\t\tLIMIT 5\n\t")
	return cypher.String(), params
}

// PathHopPredicates returns the conditions every node on a returned
// call chain must satisfy, for the Neo4j-compat shortestPath read.
//
// Binding the two endpoints is not enough here. The projection returns EVERY
// node on the path -- id, name, labels, language, docstring, method_kind -- so a
// chain whose endpoints are both in grant can still carry an interior hop from a
// repository the caller was never granted. The endpoint predicates live in the
// anchoring WHERE; this is the only clause that reaches the hops between them.
//
// The caller's grant is a conjunct beside the request's own traversal bound
// rather than a replacement for it: cross_repo and repo_id already narrow the
// path to the selectors the caller named, and the grant narrows it to what the
// caller may read at all. A scoped caller who names neither -- which the route
// permits -- gets the grant conjunct alone, which is the case that was
// previously unbounded.
//
// This shape is deliberately NOT mirrored into BuildNornicDBCallChainCypher.
// A list-membership test inside all(node IN nodes(path) ...) is not evaluated on
// the pinned NornicDB build (see the path-predicate table in
// docs/public/reference/nornicdb-path-predicate-pitfalls.md), so writing it there would
// be grant text that grants nothing -- the exact defect this batch fixed. That
// lane bounds each hop as its Go-side traversal expands instead
// (nornicDBCallChainOneHopRows), and its shortestPath builder is unreachable
// from handleCallChain.
func PathHopPredicates(req Request, access querycontract.RepositoryAccessFilter) []string {
	predicates := make([]string, 0, 2)
	switch {
	case req.CrossRepo:
		predicates = append(predicates, "coalesce(node.repo_id, '') IN $traversal_repo_ids")
	case strings.TrimSpace(req.RepoID) != "":
		predicates = append(predicates, "coalesce(node.repo_id, '') = $repo_id")
	}
	if access.Scoped() {
		// Rendered by the grant contract rather than written out here, so a
		// change to how it renders moves this predicate with the endpoint ones
		// eight lines up instead of leaving it behind. The bare property is
		// right: a null repo_id makes the membership test null, all() over a
		// null yields null, and WHERE null drops the row -- so an unattributable
		// hop still fails closed without the coalesce the request's own bound
		// carries. Nor can a "" in the grant admit one: the id lists are cleaned
		// at the context boundary, so neither array can contain an empty value.
		predicates = append(predicates, access.GraphConditionOnProperty("node", "repo_id"))
	}
	return predicates
}

// BuildNornicDBCallChainCypher is the NornicDB dialect of the shortestPath
// call-chain read.
//
// It is not on the live NornicDB path: handleCallChain sends a NornicDB backend
// to nornicDBCallChainRows above, and only a non-NornicDB backend reaches
// BuildCallChainCypher. That matters, because #5167 batch 2b ran this exact
// statement against the pinned build and it does not parse there --
// "shortestPath: could not resolve start variable" -- so the pre-bound-endpoint
// shape docs/public/reference/nornicdb-path-predicate-pitfalls.md records as safe was
// measured on an older build and is not safe on the current pin. It carries the
// grant on both endpoints like every other builder in the family, and the parse
// failure is tracked in docs/internal/evidence/5167-code-family-batch-2b.md.
//
// It does NOT carry the path-wide grant conjunct its Neo4j sibling gained
// (PathHopPredicates), because a list-membership test inside
// all(node IN nodes(path) ...) is not evaluated on the pinned build -- writing
// it here would be grant text that grants nothing. So a scoped caller reaching
// this builder would get bounded endpoints and unbounded interior hops. Nothing
// can: handleCallChain routes a NornicDB backend to nornicDBCallChainRows, whose
// per-hop bound is the shape that works on this backend, and the statement below
// does not parse there in any case. If this builder is ever made reachable, the
// interior has to be bounded in Go from the raw nodes(path) projection.
func BuildNornicDBCallChainCypher(
	req Request,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{}
	predicates := make([]string, 0, 4)

	startPattern := "(start"
	if strings.TrimSpace(req.StartEntityID) != "" {
		params["start_entity_id"] = strings.TrimSpace(req.StartEntityID)
		startPattern += " {uid: $start_entity_id}"
	} else {
		params["start"] = strings.TrimSpace(req.Start)
		startPattern += " {name: $start}"
	}
	startPattern += ")"

	endPattern := "(end"
	if strings.TrimSpace(req.EndEntityID) != "" {
		params["end_entity_id"] = strings.TrimSpace(req.EndEntityID)
		endPattern += " {uid: $end_entity_id}"
	} else {
		params["end"] = strings.TrimSpace(req.End)
		endPattern += " {name: $end}"
	}
	endPattern += ")"

	if req.CrossRepo {
		params["start_repo_id"] = strings.TrimSpace(StartRepoID(&req))
		params["end_repo_id"] = strings.TrimSpace(EndRepoID(&req))
		params["traversal_repo_ids"] = AllowedTraversalRepoIDs(&req)
		predicates = append(predicates, "start.repo_id = $start_repo_id", "end.repo_id = $end_repo_id")
	} else if strings.TrimSpace(req.RepoID) != "" {
		params["repo_id"] = strings.TrimSpace(req.RepoID)
		predicates = append(predicates, "start.repo_id = $repo_id", "end.repo_id = $repo_id")
	}
	if access.Scoped() {
		params = access.GraphParams(params)
		predicates = append(predicates,
			access.GraphConditionOnProperty("start", "repo_id"),
			access.GraphConditionOnProperty("end", "repo_id"),
		)
	}

	var cypher strings.Builder
	cypher.WriteString("\n\t\tMATCH ")
	cypher.WriteString(startPattern)
	cypher.WriteString("\n\t\tMATCH ")
	cypher.WriteString(endPattern)
	if len(predicates) > 0 {
		cypher.WriteString("\n\t\tWHERE ")
		cypher.WriteString(strings.Join(predicates, " AND "))
	}
	cypher.WriteString("\n\t\tMATCH path = shortestPath(\n")
	cypher.WriteString("\t\t\t(start)-[:CALLS*1..")
	fmt.Fprint(&cypher, req.MaxDepth)
	cypher.WriteString("]->(end)\n")
	cypher.WriteString("\t\t)\n")
	if req.CrossRepo {
		cypher.WriteString("\t\tWHERE all(node IN nodes(path) WHERE coalesce(node.repo_id, '') IN $traversal_repo_ids)\n")
	} else if strings.TrimSpace(req.RepoID) != "" {
		cypher.WriteString("\t\tWHERE all(node IN nodes(path) WHERE coalesce(node.repo_id, '') = $repo_id)\n")
	}
	// NornicDB returns typed Bolt nodes for raw nodes(path); the handler
	// normalizes them to Eshu's existing call-chain response shape.
	cypher.WriteString("\t\tRETURN nodes(path) as chain,\n")
	cypher.WriteString("\t\t       length(path) as depth\n")
	cypher.WriteString("\t\tLIMIT 5\n\t")
	return cypher.String(), params
}
