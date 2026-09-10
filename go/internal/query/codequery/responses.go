// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype"
)

// This file shapes call-chain responses: row normalization, semantic
// enrichment, and the HTTP writer. It sits beside callers.go (which owns
// the *CodeHandler methods) so neither file approaches the file cap.
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

func cloneCallChainNodeSlice(nodes []map[string]any) []map[string]any {
	cloned := make([]map[string]any, 0, len(nodes)+1)
	for _, node := range nodes {
		cloned = append(cloned, cloneQueryAnyMap(node))
	}
	return cloned
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
		if metadata := querycontract.GraphResultMetadata(normalized); len(metadata) > 0 {
			normalized["metadata"] = metadata
			entitysemantics.AttachSemanticSummary(normalized)
		}
		attached = append(attached, normalized)
	}

	return attached
}

// writeCallChainResponse renders the route's one success shape. A grantless
// scoped caller gets it with an empty chain list, which is the same answer as
// "no chain found" -- so an empty grant cannot be told apart from an absent
// route, and the route never reaches a backend to produce it.
func writeCallChainResponse(
	w http.ResponseWriter,
	r *http.Request,
	h *CodeHandler,
	req callChainRequest,
	rows []map[string]any,
) {
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

func formatReachableCallChainCandidatePairs(pairs []callChainCandidatePair) string {
	items := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		items = append(items, fmt.Sprintf("%s -> %s (depth %d)", pair.startID, pair.endID, pair.depth))
	}
	return strings.Join(items, ", ")
}

func formatCallChainCandidateIDs(candidates []EntityContent) string {
	items := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if id := strings.TrimSpace(candidate.EntityID); id != "" {
			items = append(items, id)
		}
	}
	return strings.Join(items, ", ")
}
