// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

// The graph fake behind the #5167 batch-2b call-chain proofs, split out of
// auth_scoped_call_chain_grant_test.go when that file passed 800 lines.
//
// It answers the four statement kinds the route issues -- the per-label
// identity lookup, the metadata anchor, the NornicDB one-hop expansion, and the
// Neo4j-compat shortestPath read -- and applies each statement's repository
// predicates according to the clause they are attached to, which is the whole
// question this batch exists to settle. A fake that applied every predicate to
// the driving rows would pass on statements the backend does not filter.

// callChainGrantEntity is one seeded graph entity: its identity, the repository
// it belongs to, and the entities it calls.
type callChainGrantEntity struct {
	uid    string
	name   string
	repoID string
	calls  []string
}

// callChainGrantGraph answers the three statement kinds the NornicDB call-chain
// path issues -- the per-label identity lookup, the metadata anchor, and the
// one-hop expansion -- and applies each statement's repository predicates
// according to the clause they are attached to, the same rule storyClauseGraph
// uses and the same rule the backend follows.
type callChainGrantGraph struct {
	entities   []callChainGrantEntity
	statements []string
	// parseFailures records a statement this fake could not read the way it
	// expects. Without it a marker that stops matching -- a reformatted builder,
	// a renamed clause -- would silently yield no predicates, and a fake that
	// applies no predicates admits every row. Tests assert this is empty, so a
	// parse miss is its own failure rather than a result that happens to be
	// right or wrong for an unrelated reason.
	parseFailures []string
}

func (g *callChainGrantGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.statements = append(g.statements, cypher)
	switch {
	case strings.Contains(cypher, "CALL {"):
		return g.labelRows(params), nil
	case strings.Contains(cypher, "shortestPath("):
		return g.shortestPathRows(cypher, params), nil
	case strings.Contains(cypher, "<-[:CONTAINS]-(f:File)"):
		return g.metadataRows(cypher, params), nil
	case strings.Contains(cypher, "-[:CALLS]->(target)"):
		return g.oneHopRows(cypher, params), nil
	default:
		return nil, nil
	}
}

func (g *callChainGrantGraph) RunSingle(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// labelRows answers the per-label identity lookup, which carries no repository
// binding today: it returns only the label of an entity id the caller already
// named. The route tests below assert on the response body, so an unbound label
// lookup cannot make them pass.
func (g *callChainGrantGraph) labelRows(params map[string]any) []map[string]any {
	entityID, _ := params["entity_id"].(string)
	for _, entity := range g.entities {
		if entity.uid == entityID {
			return []map[string]any{{"uid": entity.uid, "id": entity.uid, "labels": []string{"Function"}}}
		}
	}
	return nil
}

func (g *callChainGrantGraph) metadataRows(cypher string, params map[string]any) []map[string]any {
	anchoring, _ := storyClausePredicates(cypher)
	name, _ := params["name"].(string)
	entityID, _ := params["entity_id"].(string)
	rows := make([]map[string]any, 0, 2)
	for _, entity := range g.entities {
		if entityID != "" && entity.uid != entityID {
			continue
		}
		if name != "" && entity.name != name {
			continue
		}
		if !callChainRepoAliasAdmits(anchoring, "repo", entity.repoID, params) {
			continue
		}
		rows = append(rows, map[string]any{
			"id": entity.uid, "name": entity.name, "labels": []string{"Function"},
			"file_path": "internal/a.go", "repo_id": entity.repoID, "repo_name": entity.repoID,
			"language": "go", "start_line": 1, "end_line": 9,
		})
	}
	return rows
}

func (g *callChainGrantGraph) oneHopRows(cypher string, params map[string]any) []map[string]any {
	anchoring, stranded := storyClausePredicates(cypher)
	sourceID, _ := params["source_id"].(string)
	rows := make([]map[string]any, 0, 2)
	for _, entity := range g.entities {
		if entity.uid != sourceID {
			continue
		}
		for _, calleeUID := range entity.calls {
			callee, ok := g.entity(calleeUID)
			if !ok {
				continue
			}
			seed := storyGrantSeed{repoByAlias: map[string]string{"target": callee.repoID}}
			if !storySeedAdmits(seed, anchoring, params) {
				continue
			}
			row := map[string]any{
				"id": callee.uid, "name": callee.name, "labels": []string{"Function"},
				"repo_id": callee.repoID, "language": "go",
			}
			// A predicate stranded on an OPTIONAL MATCH nulls that pattern's
			// columns and keeps the row: the defect this batch measured.
			if !storySeedAdmits(seed, stranded, params) {
				row["repo_id"] = callee.repoID
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// shortestPathRows answers the Neo4j-compat statement. It walks the seeded
// calls breadth-first for the shortest start-to-end path, then applies the two
// clauses separately, because they reach different things: the WHERE before
// `MATCH path = shortestPath` constrains the endpoints, and the WHERE after it
// -- the all(node IN nodes(path) ...) predicate -- is the only one that reaches
// the hops in between. A statement that binds only the endpoints returns the
// interior hop, which is what this fake exists to catch.
func (g *callChainGrantGraph) shortestPathRows(cypher string, params map[string]any) []map[string]any {
	endpointPredicates, hopPredicates, parsed := callChainClausePredicates(cypher)
	if !parsed {
		g.parseFailures = append(g.parseFailures, cypher)
		return nil
	}
	start, ok := g.endpoint(params, "start")
	if !ok {
		return nil
	}
	end, ok := g.endpoint(params, "end")
	if !ok {
		return nil
	}
	seed := storyGrantSeed{repoByAlias: map[string]string{"start": start.repoID, "end": end.repoID}}
	if !storySeedAdmits(seed, endpointPredicates, params) {
		return nil
	}
	path := g.shortestPath(start.uid, end.uid)
	if len(path) == 0 {
		return nil
	}
	for _, node := range path {
		for _, predicate := range hopPredicates {
			if !callChainHopAdmits(predicate, node.repoID, params) {
				return nil
			}
		}
	}
	chain := make([]any, 0, len(path))
	for _, node := range path {
		chain = append(chain, map[string]any{
			"id": node.uid, "name": node.name, "labels": []string{"Function"},
			"language": "go", "docstring": "", "method_kind": "",
		})
	}
	return []map[string]any{{"chain": chain, "depth": len(path) - 1}}
}

func (g *callChainGrantGraph) endpoint(params map[string]any, prefix string) (callChainGrantEntity, bool) {
	if uid, _ := params[prefix+"_entity_id"].(string); uid != "" {
		return g.entity(uid)
	}
	name, _ := params[prefix].(string)
	for _, entity := range g.entities {
		if entity.name == name {
			return entity, true
		}
	}
	return callChainGrantEntity{}, false
}

// shortestPath returns the node sequence of the shortest CALLS path, endpoints
// included, or nil when there is none.
func (g *callChainGrantGraph) shortestPath(startUID, endUID string) []callChainGrantEntity {
	type step struct {
		uid  string
		path []callChainGrantEntity
	}
	start, ok := g.entity(startUID)
	if !ok {
		return nil
	}
	frontier := []step{{uid: startUID, path: []callChainGrantEntity{start}}}
	seen := map[string]struct{}{startUID: {}}
	for depth := 0; depth < 10 && len(frontier) > 0; depth++ {
		next := make([]step, 0)
		for _, current := range frontier {
			entity, ok := g.entity(current.uid)
			if !ok {
				continue
			}
			for _, calleeUID := range entity.calls {
				callee, ok := g.entity(calleeUID)
				if !ok {
					continue
				}
				path := append(append([]callChainGrantEntity{}, current.path...), callee)
				if calleeUID == endUID {
					return path
				}
				if _, visited := seen[calleeUID]; visited {
					continue
				}
				seen[calleeUID] = struct{}{}
				next = append(next, step{uid: calleeUID, path: path})
			}
		}
		frontier = next
	}
	return nil
}

// callChainClausePredicates splits the compat statement at its shortestPath
// clause: endpoint predicates before, hop predicates after (unwrapped from the
// all(...) they are written inside).
//
// parsed reports whether the statement had the shape this reader expects. An
// absent all(...) clause is NOT a parse failure -- a caller with no grant and no
// repository selector legitimately renders none -- but a statement with no
// shortestPath clause at all, or an all(...) whose block does not terminate, is.
// The distinction matters because an unrecognised statement yields no predicates,
// and a fake that applies no predicates admits every row: exactly the false green
// this batch exists to prevent.
func callChainClausePredicates(cypher string) (endpoints []string, hops []string, parsed bool) {
	normalized := normalizeCypherWhitespace(cypher)
	split := strings.Index(normalized, "MATCH path = shortestPath")
	if split < 0 {
		return nil, nil, false
	}
	head, tail := normalized[:split], normalized[split:]
	if at := strings.Index(head, "WHERE "); at >= 0 {
		endpoints = storySplitPredicates(strings.TrimSpace(head[at+len("WHERE "):]))
	}
	marker := "WHERE all(node IN nodes(path) WHERE "
	at := strings.Index(tail, marker)
	if at < 0 {
		return endpoints, nil, true
	}
	block := tail[at+len(marker):]
	end := strings.Index(block, ") RETURN ")
	if end < 0 {
		return endpoints, nil, false
	}
	return endpoints, storySplitPredicates(strings.TrimSpace(block[:end])), true
}

// callChainHopAdmits evaluates one hop predicate. The request's own hop bounds
// coalesce node.repo_id against the empty string, the grant's reads the bare
// property, and neither matches storyPredicateAdmits' per-alias keys, so they
// need their own matcher.
//
// The Cypher literal is spelled out rather than quoted because gofmt reformats
// doc comments and turns a pair of single quotes into a typographic quote pair.
func callChainHopAdmits(predicate, repoID string, params map[string]any) bool {
	switch {
	case strings.Contains(predicate, "IN $allowed_repository_ids"):
		return graphParamContains(params, "allowed_repository_ids", repoID) ||
			graphParamContains(params, "allowed_scope_ids", repoID)
	case strings.Contains(predicate, "IN $traversal_repo_ids"):
		return graphParamContains(params, "traversal_repo_ids", repoID)
	case strings.Contains(predicate, "= $repo_id"):
		bound, _ := params["repo_id"].(string)
		return repoID == bound && repoID != ""
	default:
		return true
	}
}

func (g *callChainGrantGraph) entity(uid string) (callChainGrantEntity, bool) {
	for _, entity := range g.entities {
		if entity.uid == uid {
			return entity, true
		}
	}
	return callChainGrantEntity{}, false
}

// callChainRepoAliasAdmits evaluates the predicates on a Repository alias, whose
// grant key is its own id rather than a repo_id property.
func callChainRepoAliasAdmits(predicates []string, alias, repoID string, params map[string]any) bool {
	for _, predicate := range predicates {
		switch {
		case strings.Contains(predicate, alias+".id IN $allowed_repository_ids"):
			if !graphParamContains(params, "allowed_repository_ids", repoID) &&
				!graphParamContains(params, "allowed_scope_ids", repoID) {
				return false
			}
		case strings.Contains(predicate, alias+".id = $repo_id"):
			bound, _ := params["repo_id"].(string)
			if repoID != bound || repoID == "" {
				return false
			}
		}
	}
	return true
}
