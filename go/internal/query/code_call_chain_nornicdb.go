// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
)

type nornicDBCallChainPath struct {
	nodeID string
	label  string
	chain  []map[string]any
}

func (h *CodeHandler) nornicDBCallChainRows(ctx context.Context, req callChainRequest) ([]map[string]any, error) {
	start, err := h.nornicDBRelationshipMetadataRow(ctx, req.StartEntityID, req.Start, callChainStartRepoID(&req))
	if err != nil || start == nil {
		return nil, err
	}
	end, err := h.nornicDBRelationshipMetadataRow(ctx, req.EndEntityID, req.End, callChainEndRepoID(&req))
	if err != nil || end == nil {
		return nil, err
	}

	endID := StringVal(end, "id")
	frontier := []nornicDBCallChainPath{{
		nodeID: StringVal(start, "id"),
		label:  nornicDBPrimaryEntityLabel(start),
		chain:  []map[string]any{nornicDBCallChainNode(start)},
	}}
	seen := map[string]struct{}{StringVal(start, "id"): {}}
	rows := make([]map[string]any, 0, 1)

	// Keep NornicDB traversal breadth-first so the first returned rows are the
	// shortest paths, and stop once the response cap is satisfied.
	for depth := 1; depth <= req.MaxDepth && len(frontier) > 0 && len(rows) < 5; depth++ {
		next := make([]nornicDBCallChainPath, 0)
		for _, path := range frontier {
			targets, err := h.nornicDBCallChainOneHopRows(ctx, path.nodeID, path.label, callChainAllowedTraversalRepoIDs(&req))
			if err != nil {
				return nil, err
			}
			for _, target := range targets {
				targetID := StringVal(target, "id")
				if targetID == "" {
					continue
				}
				chain := append(cloneCallChainNodeSlice(path.chain), nornicDBCallChainNode(target))
				if targetID == endID {
					rows = append(rows, map[string]any{
						"chain": chain,
						"depth": depth,
					})
					if len(rows) >= 5 {
						break
					}
					continue
				}
				if _, ok := seen[targetID]; ok {
					continue
				}
				seen[targetID] = struct{}{}
				next = append(next, nornicDBCallChainPath{
					nodeID: targetID,
					label:  nornicDBPrimaryEntityLabel(target),
					chain:  chain,
				})
			}
		}
		frontier = next
	}
	return rows, nil
}

func (h *CodeHandler) nornicDBCallChainOneHopRows(
	ctx context.Context,
	sourceID string,
	sourceLabel string,
	allowedRepoIDs []string,
) ([]map[string]any, error) {
	sourcePattern := nornicDBNodePattern("source", sourceLabel, "$source_id")
	params := map[string]any{"source_id": sourceID}
	access := codeGrantAccessFilter(ctx)
	predicates := make([]string, 0, 2)
	if len(allowedRepoIDs) > 0 {
		params["traversal_repo_ids"] = allowedRepoIDs
		predicates = append(predicates, "coalesce(target.repo_id, '') IN $traversal_repo_ids")
	}
	if access.Scoped() {
		params = access.GraphParams(params)
		predicates = append(predicates, access.GraphConditionOnProperty("target", "repo_id"))
	}
	// Both predicates sit in the anchoring MATCH's own WHERE, on the target
	// node's own repo_id. They used to follow the two OPTIONAL MATCH clauses
	// below, where a WHERE constrains the optional pattern rather than the
	// driving row set: #5167 batch 2b measured the shipped statement returning
	// every callee with its real repository id while $traversal_repo_ids named
	// one repository. The coalesce(target.repo_id, targetRepo.id, '') fallback
	// cannot survive the move because targetRepo is not bound yet, and it should
	// not: a target the graph cannot attribute to a repository now fails the
	// predicate and is dropped.
	repoPredicate := ""
	if len(predicates) > 0 {
		repoPredicate = `
		WHERE ` + strings.Join(predicates, " AND ")
	}
	rows, err := h.Neo4j.Run(ctx, `
		MATCH `+sourcePattern+`-[:CALLS]->(target)`+repoPredicate+`
		OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)
		OPTIONAL MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
		RETURN coalesce(target.id, target.uid) as id,
		       target.name as name,
		       labels(target) as labels,
		       coalesce(target.repo_id, targetRepo.id) as repo_id,
		       coalesce(target.language, target.lang) as language,
		       target.docstring as docstring,
		       target.method_kind as method_kind
	`, params)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func nornicDBCallChainNode(row map[string]any) map[string]any {
	node := map[string]any{
		"id":          StringVal(row, "id"),
		"name":        StringVal(row, "name"),
		"labels":      StringSliceVal(row, "labels"),
		"language":    row["language"],
		"docstring":   row["docstring"],
		"method_kind": row["method_kind"],
	}
	for _, key := range []string{"decorators", "async", "class_context", "impl_context", "semantic_kind"} {
		if value, ok := row[key]; ok {
			node[key] = value
		}
	}
	return node
}

func cloneCallChainNodeSlice(nodes []map[string]any) []map[string]any {
	cloned := make([]map[string]any, 0, len(nodes)+1)
	for _, node := range nodes {
		cloned = append(cloned, cloneQueryAnyMap(node))
	}
	return cloned
}

// buildNornicDBCallChainCypher is the NornicDB dialect of the shortestPath
// call-chain read.
//
// It is not on the live NornicDB path: handleCallChain sends a NornicDB backend
// to nornicDBCallChainRows above, and only a non-NornicDB backend reaches
// buildCallChainCypher. That matters, because #5167 batch 2b ran this exact
// statement against the pinned build and it does not parse there --
// "shortestPath: could not resolve start variable" -- so the pre-bound-endpoint
// shape docs/public/reference/nornicdb-query-pitfalls.md records as safe was
// measured on an older build and is not safe on the current pin. It carries the
// grant like every other builder in the family so a future caller cannot reach
// it unbound, and the parse failure is tracked in
// docs/internal/evidence/5167-code-family-batch-2b.md.
func buildNornicDBCallChainCypher(
	req callChainRequest,
	access repositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{}
	predicates := make([]string, 0, 2)

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
		params["start_repo_id"] = strings.TrimSpace(callChainStartRepoID(&req))
		params["end_repo_id"] = strings.TrimSpace(callChainEndRepoID(&req))
		params["traversal_repo_ids"] = callChainAllowedTraversalRepoIDs(&req)
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
