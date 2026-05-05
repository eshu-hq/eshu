package query

import "context"

type nornicDBCallChainPath struct {
	nodeID string
	label  string
	chain  []map[string]any
}

func (h *CodeHandler) nornicDBCallChainRows(ctx context.Context, req callChainRequest) ([]map[string]any, error) {
	start, err := h.nornicDBRelationshipMetadataRow(ctx, req.StartEntityID, req.Start, req.RepoID)
	if err != nil || start == nil {
		return nil, err
	}
	end, err := h.nornicDBRelationshipMetadataRow(ctx, req.EndEntityID, req.End, req.RepoID)
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
			targets, err := h.nornicDBCallChainOneHopRows(ctx, path.nodeID, path.label)
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

func (h *CodeHandler) nornicDBCallChainOneHopRows(ctx context.Context, sourceID string, sourceLabel string) ([]map[string]any, error) {
	sourcePattern := nornicDBNodePattern("source", sourceLabel, "$source_id")
	rows, err := h.Neo4j.Run(ctx, `
		MATCH `+sourcePattern+`-[:CALLS]->(target)
		RETURN coalesce(target.id, target.uid) as id,
		       target.name as name,
		       labels(target) as labels,
		       coalesce(target.language, target.lang) as language,
		       target.docstring as docstring,
		       target.method_kind as method_kind
	`, map[string]any{"source_id": sourceID})
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
