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
	MaxDepth      int    `json:"max_depth"`
}

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
	if !h.applyRepositorySelector(w, r, &req.RepoID) {
		return
	}
	if err := h.resolveCallChainEntityIDs(r.Context(), &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	var rows []map[string]any
	if h.graphBackend() == GraphBackendNornicDB {
		nornicRows, err := h.nornicDBCallChainRows(r.Context(), req)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows = nornicRows
	} else {
		cypher, params := buildCallChainCypher(req, h.graphBackend())
		neoRows, err := h.Neo4j.Run(r.Context(), cypher, params)
		if err != nil {
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
		"chains":          chains,
	}, BuildTruthEnvelope(h.profile(), "call_graph.call_chain_path", TruthBasisAuthoritativeGraph, "resolved from authoritative call graph traversal"))
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

	if strings.TrimSpace(req.RepoID) != "" {
		params["repo_id"] = strings.TrimSpace(req.RepoID)
		predicates = append(predicates, "start.repo_id = $repo_id", "end.repo_id = $repo_id")
	}

	var cypher strings.Builder
	cypher.WriteString("\n\t\tMATCH (start)\n")
	cypher.WriteString("\t\tMATCH (end)")
	if len(predicates) > 0 {
		cypher.WriteString("\n\t\tWHERE ")
		cypher.WriteString(strings.Join(predicates, " AND "))
	}
	cypher.WriteString("\n\t\tMATCH path = shortestPath(\n")
	cypher.WriteString("\t\t\t(start)-[:CALLS*1..")
	fmt.Fprint(&cypher, req.MaxDepth)
	cypher.WriteString("]->(end)\n")
	cypher.WriteString("\t\t)\n")
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

	if strings.TrimSpace(req.RepoID) != "" {
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
		startCandidates, err = resolveExactGraphEntityCandidates(ctx, h.Content, req.RepoID, req.Start)
		if err != nil {
			return err
		}
		resolved, err := selectExactGraphEntityCandidate(req.RepoID, req.Start, startCandidates)
		startErr = err
		if resolved != nil {
			req.StartEntityID = resolved.EntityID
		}
	}
	if strings.TrimSpace(req.EndEntityID) == "" && strings.TrimSpace(req.End) != "" {
		var err error
		endCandidates, err = resolveExactGraphEntityCandidates(ctx, h.Content, req.RepoID, req.End)
		if err != nil {
			return err
		}
		resolved, err := selectExactGraphEntityCandidate(req.RepoID, req.End, endCandidates)
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
