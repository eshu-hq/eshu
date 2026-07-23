// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype"
)

type callChainRequest struct {
	Start         string `json:"start"`
	End           string `json:"end"`
	StartEntityID string `json:"start_entity_id"`
	EndEntityID   string `json:"end_entity_id"`
	RepoID        string `json:"repo_id"`
	CrossRepo     bool   `json:"cross_repo"`
	StartRepoID   string `json:"start_repo_id"`
	EndRepoID     string `json:"end_repo_id"`
	MaxDepth      int    `json:"max_depth"`
}

// codeCallChainAnchorLabelDisjunction is the label set the Neo4j-compat
// call-chain builder seeds its start/end anchors with. It mirrors the
// authoritative CALLS-source label set the canonical edge writer projects
// (see codeCallRetractSourceLabels in storage/cypher), so every node
// reachable as a call-chain endpoint still resolves while the planner seeds
// from a label/index scan instead of an all-node scan. The prior unlabeled
// `MATCH (start)` / `MATCH (end)` gave the Neo4j planner no label to anchor on,
// so the id/name predicate forced a full-graph scan (issue #3567). NornicDB has
// its own builder (buildNornicDBCallChainCypher) and is intentionally untouched.
const codeCallChainAnchorLabelDisjunction = "Function|Class|Struct|Interface|TypeAlias|File"

func (h *CodeHandler) handleCallChain(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "call_graph.call_chain_path") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"call-chain analysis requires authoritative graph mode",
			"unsupported_capability",
			"call_graph.call_chain_path",
			h.profile(),
			requiredProfile("call_graph.call_chain_path"),
		)
		return
	}

	var req callChainRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if strings.TrimSpace(req.StartEntityID) == "" && strings.TrimSpace(req.Start) == "" {
		WriteError(w, http.StatusBadRequest, "start or start_entity_id is required")
		return
	}
	if strings.TrimSpace(req.EndEntityID) == "" && strings.TrimSpace(req.End) == "" {
		WriteError(w, http.StatusBadRequest, "end or end_entity_id is required")
		return
	}
	if req.MaxDepth <= 0 {
		req.MaxDepth = 5
	}
	if req.MaxDepth > 10 {
		req.MaxDepth = 10
	}
	if err := req.validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, "call_graph.call_chain_path") {
		return
	}
	if req.CrossRepo {
		if !h.applyRepositorySelectorForCapability(w, r, &req.StartRepoID, "call_graph.call_chain_path") {
			return
		}
		if !h.applyRepositorySelectorForCapability(w, r, &req.EndRepoID, "call_graph.call_chain_path") {
			return
		}
	}
	if err := h.resolveCallChainEntityIDs(r.Context(), &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	var rows []map[string]any
	if h.graphBackend() == GraphBackendNornicDB {
		nornicRows, err := h.nornicDBCallChainRows(r.Context(), req)
		if err != nil {
			if WriteGraphReadError(w, r, err, "call_graph.call_chain_path") {
				return
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows = nornicRows
	} else {
		cypher, params := buildCallChainCypher(req, h.graphBackend())
		neoRows, err := h.Neo4j.Run(r.Context(), cypher, params)
		if err != nil {
			if WriteGraphReadError(w, r, err, "call_graph.call_chain_path") {
				return
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows = neoRows
	}

	chains := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		chain := attachCallChainNodeSemantics(normalizeCallChainNodes(row["chain"]))
		chains = append(chains, map[string]any{
			"chain": chain,
			"depth": IntVal(row, "depth"),
		})
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"start":           req.Start,
		"end":             req.End,
		"start_entity_id": req.StartEntityID,
		"end_entity_id":   req.EndEntityID,
		"repo_id":         req.RepoID,
		"cross_repo":      req.CrossRepo,
		"start_repo_id":   req.StartRepoID,
		"end_repo_id":     req.EndRepoID,
		"chains":          chains,
	}, BuildTruthEnvelope(h.profile(), "call_graph.call_chain_path", TruthBasisAuthoritativeGraph, "resolved from authoritative call graph traversal"))
}

func (r callChainRequest) validate() error {
	if !r.CrossRepo && (strings.TrimSpace(r.StartRepoID) != "" || strings.TrimSpace(r.EndRepoID) != "") {
		return fmt.Errorf("start_repo_id and end_repo_id require cross_repo")
	}
	if !r.CrossRepo {
		return nil
	}
	if strings.TrimSpace(callChainStartRepoID(&r)) == "" {
		return fmt.Errorf("cross_repo call-chain traversal requires start_repo_id or repo_id")
	}
	if strings.TrimSpace(callChainEndRepoID(&r)) == "" {
		return fmt.Errorf("cross_repo call-chain traversal requires end_repo_id or repo_id")
	}
	return nil
}

func buildCallChainCypher(req callChainRequest, backend GraphBackend) (string, map[string]any) {
	params := map[string]any{}
	predicates := make([]string, 0, 2)

	if backend == GraphBackendNornicDB {
		return buildNornicDBCallChainCypher(req)
	}

	if strings.TrimSpace(req.StartEntityID) != "" {
		params["start_entity_id"] = strings.TrimSpace(req.StartEntityID)
		predicates = append(predicates, graphEntityIDPredicate("start", "$start_entity_id"))
	} else {
		params["start"] = strings.TrimSpace(req.Start)
		predicates = append(predicates, "start.name = $start")
	}

	if strings.TrimSpace(req.EndEntityID) != "" {
		params["end_entity_id"] = strings.TrimSpace(req.EndEntityID)
		predicates = append(predicates, graphEntityIDPredicate("end", "$end_entity_id"))
	} else {
		params["end"] = strings.TrimSpace(req.End)
		predicates = append(predicates, "end.name = $end")
	}

	if req.CrossRepo {
		params["start_repo_id"] = strings.TrimSpace(callChainStartRepoID(&req))
		params["end_repo_id"] = strings.TrimSpace(callChainEndRepoID(&req))
		params["traversal_repo_ids"] = callChainAllowedTraversalRepoIDs(&req)
		predicates = append(predicates, "start.repo_id = $start_repo_id", "end.repo_id = $end_repo_id")
	} else if strings.TrimSpace(req.RepoID) != "" {
		params["repo_id"] = strings.TrimSpace(req.RepoID)
		predicates = append(predicates, "start.repo_id = $repo_id", "end.repo_id = $repo_id")
	}

	var cypher strings.Builder
	cypher.WriteString("\n\t\tMATCH (start:" + codeCallChainAnchorLabelDisjunction + ")\n")
	cypher.WriteString("\t\tMATCH (end:" + codeCallChainAnchorLabelDisjunction + ")")
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
	if backend == GraphBackendNornicDB {
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

func buildNornicDBCallChainCypher(req callChainRequest) (string, map[string]any) {
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

func normalizeCallChainNodes(raw any) []any {
	switch nodes := raw.(type) {
	case []map[string]any:
		normalized := make([]any, 0, len(nodes))
		for _, node := range nodes {
			normalized = append(normalized, normalizeCallChainNode(node))
		}
		return normalized
	case []any:
		normalized := make([]any, 0, len(nodes))
		for _, node := range nodes {
			normalized = append(normalized, normalizeCallChainNode(node))
		}
		return normalized
	case []dbtype.Node:
		normalized := make([]any, 0, len(nodes))
		for _, node := range nodes {
			normalized = append(normalized, normalizeCallChainNode(node))
		}
		return normalized
	default:
		return nil
	}
}

func normalizeCallChainNode(raw any) any {
	switch node := raw.(type) {
	case map[string]any:
		return cloneQueryAnyMap(node)
	case dbtype.Node:
		// The shared Bolt driver returns typed nodes for raw nodes(path)
		// results, so the handler normalizes them to the existing map shape.
		labels := make([]any, 0, len(node.Labels))
		for _, label := range node.Labels {
			labels = append(labels, label)
		}
		return map[string]any{
			"id":          graphNodeSemanticID(node.Props),
			"name":        fmt.Sprintf("%v", node.Props["name"]),
			"labels":      labels,
			"language":    node.Props["language"],
			"docstring":   node.Props["docstring"],
			"method_kind": node.Props["method_kind"],
		}
	default:
		return raw
	}
}

func (h *CodeHandler) resolveCallChainEntityIDs(ctx context.Context, req *callChainRequest) error {
	if h == nil || req == nil {
		return nil
	}
	var (
		startCandidates []EntityContent
		endCandidates   []EntityContent
		startErr        error
		endErr          error
	)
	if strings.TrimSpace(req.StartEntityID) == "" && strings.TrimSpace(req.Start) != "" {
		var err error
		startRepoID := callChainStartRepoID(req)
		startCandidates, err = resolveExactGraphEntityCandidates(ctx, h.Content, startRepoID, req.Start)
		if err != nil {
			return err
		}
		resolved, err := selectExactGraphEntityCandidate(startRepoID, req.Start, startCandidates)
		startErr = err
		if resolved != nil {
			req.StartEntityID = resolved.EntityID
		}
	}
	if strings.TrimSpace(req.EndEntityID) == "" && strings.TrimSpace(req.End) != "" {
		var err error
		endRepoID := callChainEndRepoID(req)
		endCandidates, err = resolveExactGraphEntityCandidates(ctx, h.Content, endRepoID, req.End)
		if err != nil {
			return err
		}
		resolved, err := selectExactGraphEntityCandidate(endRepoID, req.End, endCandidates)
		endErr = err
		if resolved != nil {
			req.EndEntityID = resolved.EntityID
		}
	}
	if startErr != nil || endErr != nil {
		resolved, err := h.resolveCallChainEntityIDsByReachability(ctx, req, startCandidates, endCandidates)
		if err != nil {
			return err
		}
		if resolved {
			return nil
		}
	}
	if startErr != nil {
		return startErr
	}
	if endErr != nil {
		return endErr
	}
	return nil
}

func callChainStartRepoID(req *callChainRequest) string {
	if req != nil && req.CrossRepo && strings.TrimSpace(req.StartRepoID) != "" {
		return req.StartRepoID
	}
	if req == nil {
		return ""
	}
	return req.RepoID
}

func callChainEndRepoID(req *callChainRequest) string {
	if req != nil && req.CrossRepo && strings.TrimSpace(req.EndRepoID) != "" {
		return req.EndRepoID
	}
	if req == nil {
		return ""
	}
	return req.RepoID
}

func callChainTraversalRepoIDs(req *callChainRequest) []string {
	if req == nil || !req.CrossRepo {
		return nil
	}
	repos := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, repoID := range []string{callChainStartRepoID(req), callChainEndRepoID(req)} {
		repoID = strings.TrimSpace(repoID)
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		repos = append(repos, repoID)
	}
	return repos
}

func callChainAllowedTraversalRepoIDs(req *callChainRequest) []string {
	if req == nil {
		return nil
	}
	if req.CrossRepo {
		return callChainTraversalRepoIDs(req)
	}
	if repoID := strings.TrimSpace(req.RepoID); repoID != "" {
		return []string{repoID}
	}
	return nil
}

func graphNodeSemanticID(props map[string]any) string {
	if props == nil {
		return ""
	}
	if id, ok := props["id"]; ok {
		if normalized := strings.TrimSpace(fmt.Sprintf("%v", id)); normalized != "" {
			return normalized
		}
	}
	if uid, ok := props["uid"]; ok {
		if normalized := strings.TrimSpace(fmt.Sprintf("%v", uid)); normalized != "" {
			return normalized
		}
	}
	return ""
}

func attachCallChainNodeSemantics(nodes []any) []any {
	if len(nodes) == 0 {
		return nodes
	}

	attached := make([]any, 0, len(nodes))
	for _, node := range nodes {
		nodeMap, ok := node.(map[string]any)
		if !ok {
			attached = append(attached, node)
			continue
		}

		normalized := cloneQueryAnyMap(nodeMap)
		if metadata := graphResultMetadata(normalized); len(metadata) > 0 {
			normalized["metadata"] = metadata
			attachSemanticSummary(normalized)
		}
		attached = append(attached, normalized)
	}

	return attached
}
