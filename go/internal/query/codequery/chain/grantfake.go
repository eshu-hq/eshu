// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Grant fake for the #5167 batch-2b call-chain proofs: the seeded graph,
// the statement readers, and the predicate splitters in one importable home.
// Root grant tests and chain tests share it; a _test.go symbol cannot cross
// a package boundary, so a second copy would drift and keep passing.

// The graph fake behind the #5167 batch-2b call-chain proofs, split out of
// auth_scoped_call_chain_grant_test.go when that file passed 800 lines.
//
// It answers the four statement kinds the route issues -- the per-label
// identity lookup, the metadata anchor, the NornicDB one-hop expansion, and the
// Neo4j-compat shortestPath read -- and applies each statement's repository
// predicates according to the clause they are attached to, which is the whole
// question this batch exists to settle. A fake that applied every predicate to
// the driving rows would pass on statements the backend does not filter.

// GrantEntity is one seeded graph entity: its identity, the repository
// it belongs to, and the entities it calls.
type GrantEntity struct {
	UID    string
	Name   string
	RepoID string
	Calls  []string
}

// GrantGraph answers the three statement kinds the NornicDB call-chain
// path issues -- the per-label identity lookup, the metadata anchor, and the
// one-hop expansion -- and applies each statement's repository predicates
// according to the clause they are attached to, the same rule storyClauseGraph
// uses and the same rule the backend follows.
type GrantGraph struct {
	Entities   []GrantEntity
	Statements []string
	// parseFailures records a statement this fake could not read the way it
	// expects. Without it a marker that stops matching -- a reformatted builder,
	// a renamed clause -- would silently yield no predicates, and a fake that
	// applies no predicates admits every row. Tests assert this is empty, so a
	// parse miss is its own failure rather than a result that happens to be
	// right or wrong for an unrelated reason.
	ParseFailures []string
}

// rows holds the statement dispatch both exported methods answer from. Run
// documents the dispatch; this is where it lives. The indirection is
// load-bearing: the queryplan production query-callsite inventory flags any
// Run/RunSingle call expression, and routing RunSingle through Run would wear
// exactly the self-delegation shape a real graph read could hide behind.
func (g *GrantGraph) rows(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
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

func (g *GrantGraph) Run(
	ctx context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.Statements = append(g.Statements, cypher)
	return g.rows(ctx, cypher, params)
}

func (g *GrantGraph) RunSingle(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (map[string]any, error) {
	g.Statements = append(g.Statements, cypher)
	rows, err := g.rows(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// labelRows answers the per-label identity lookup, which carries no repository
// binding today: it returns only the label of an entity id the caller already
// named. The route tests below assert on the response body, so an unbound label
// lookup cannot make them pass.
func (g *GrantGraph) labelRows(params map[string]any) []map[string]any {
	entityID, _ := params["entity_id"].(string)
	for _, entity := range g.Entities {
		if entity.UID == entityID {
			return []map[string]any{{"uid": entity.UID, "id": entity.UID, "labels": []string{"Function"}}}
		}
	}
	return nil
}

func (g *GrantGraph) metadataRows(cypher string, params map[string]any) []map[string]any {
	anchoring, _ := StoryClausePredicates(cypher)
	name, _ := params["name"].(string)
	entityID, _ := params["entity_id"].(string)
	rows := make([]map[string]any, 0, 2)
	for _, entity := range g.Entities {
		if entityID != "" && entity.UID != entityID {
			continue
		}
		if name != "" && entity.Name != name {
			continue
		}
		if !callChainRepoAliasAdmits(anchoring, "repo", entity.RepoID, params) {
			continue
		}
		rows = append(rows, map[string]any{
			"id": entity.UID, "name": entity.Name, "labels": []string{"Function"},
			"file_path": "internal/a.go", "repo_id": entity.RepoID, "repo_name": entity.RepoID,
			"language": "go", "start_line": 1, "end_line": 9,
		})
	}
	return rows
}

func (g *GrantGraph) oneHopRows(cypher string, params map[string]any) []map[string]any {
	anchoring, stranded := StoryClausePredicates(cypher)
	sourceID, _ := params["source_id"].(string)
	rows := make([]map[string]any, 0, 2)
	for _, entity := range g.Entities {
		if entity.UID != sourceID {
			continue
		}
		for _, calleeUID := range entity.Calls {
			callee, ok := g.entity(calleeUID)
			if !ok {
				continue
			}
			seed := storyGrantSeed{repoByAlias: map[string]string{"target": callee.RepoID}}
			if !storySeedAdmits(seed, anchoring, params) {
				continue
			}
			row := map[string]any{
				"id": callee.UID, "name": callee.Name, "labels": []string{"Function"},
				"repo_id": callee.RepoID, "language": "go",
			}
			// A predicate stranded on an OPTIONAL MATCH nulls that pattern's
			// columns and keeps the row: the defect this batch measured.
			if !storySeedAdmits(seed, stranded, params) {
				row["repo_id"] = callee.RepoID
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
func (g *GrantGraph) shortestPathRows(cypher string, params map[string]any) []map[string]any {
	endpointPredicates, hopPredicates, parsed := ClausePredicates(cypher)
	if !parsed {
		g.ParseFailures = append(g.ParseFailures, cypher)
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
	seed := storyGrantSeed{repoByAlias: map[string]string{"start": start.RepoID, "end": end.RepoID}}
	if !storySeedAdmits(seed, endpointPredicates, params) {
		return nil
	}
	path := g.shortestPath(start.UID, end.UID)
	if len(path) == 0 {
		return nil
	}
	for _, node := range path {
		for _, predicate := range hopPredicates {
			if !callChainHopAdmits(predicate, node.RepoID, params) {
				return nil
			}
		}
	}
	chain := make([]any, 0, len(path))
	for _, node := range path {
		chain = append(chain, map[string]any{
			"id": node.UID, "name": node.Name, "labels": []string{"Function"},
			"language": "go", "docstring": "", "method_kind": "",
		})
	}
	return []map[string]any{{"chain": chain, "depth": len(path) - 1}}
}

func (g *GrantGraph) endpoint(params map[string]any, prefix string) (GrantEntity, bool) {
	if uid, _ := params[prefix+"_entity_id"].(string); uid != "" {
		return g.entity(uid)
	}
	name, _ := params[prefix].(string)
	for _, entity := range g.Entities {
		if entity.Name == name {
			return entity, true
		}
	}
	return GrantEntity{}, false
}

// shortestPath returns the node sequence of the shortest CALLS path, endpoints
// included, or nil when there is none.
func (g *GrantGraph) shortestPath(startUID, endUID string) []GrantEntity {
	type step struct {
		UID  string
		path []GrantEntity
	}
	start, ok := g.entity(startUID)
	if !ok {
		return nil
	}
	frontier := []step{{UID: startUID, path: []GrantEntity{start}}}
	seen := map[string]struct{}{startUID: {}}
	for depth := 0; depth < 10 && len(frontier) > 0; depth++ {
		next := make([]step, 0)
		for _, current := range frontier {
			entity, ok := g.entity(current.UID)
			if !ok {
				continue
			}
			for _, calleeUID := range entity.Calls {
				callee, ok := g.entity(calleeUID)
				if !ok {
					continue
				}
				path := append(append([]GrantEntity{}, current.path...), callee)
				if calleeUID == endUID {
					return path
				}
				if _, visited := seen[calleeUID]; visited {
					continue
				}
				seen[calleeUID] = struct{}{}
				next = append(next, step{UID: calleeUID, path: path})
			}
		}
		frontier = next
	}
	return nil
}

// ClausePredicates splits the compat statement at its shortestPath
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
func ClausePredicates(cypher string) (endpoints []string, hops []string, parsed bool) {
	normalized := querycontract.NormalizeCypherWhitespace(cypher)
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
		return querycontract.GraphParamContains(params, "allowed_repository_ids", repoID) ||
			querycontract.GraphParamContains(params, "allowed_scope_ids", repoID)
	case strings.Contains(predicate, "IN $traversal_repo_ids"):
		return querycontract.GraphParamContains(params, "traversal_repo_ids", repoID)
	case strings.Contains(predicate, "= $repo_id"):
		bound, _ := params["repo_id"].(string)
		return repoID == bound && repoID != ""
	default:
		return true
	}
}

func (g *GrantGraph) entity(uid string) (GrantEntity, bool) {
	for _, entity := range g.Entities {
		if entity.UID == uid {
			return entity, true
		}
	}
	return GrantEntity{}, false
}

// storyGrantSeed is one row the fake can return. repoByAlias says which
// repository each node alias in the statement resolves to. Copy of
// story_grant_clause_fake_test.go, which stays in codequery.
type storyGrantSeed struct {
	repoByAlias map[string]string
}

func storySeedAdmits(seed storyGrantSeed, predicates []string, params map[string]any) bool {
	for _, predicate := range predicates {
		if !storyPredicateAdmits(predicate, seed.repoByAlias, params) {
			return false
		}
	}
	return true
}

// StoryClausePredicates splits a statement's repository predicates by the clause
// they are attached to: the WHERE on the anchoring MATCH, and any WHERE that
// follows an OPTIONAL MATCH. Copy of story_grant_clause_fake_test.go.
func StoryClausePredicates(cypher string) (anchoring []string, stranded []string) {
	normalized := querycontract.NormalizeCypherWhitespace(cypher)
	seenOptional := false
	for _, token := range storyClauseTokens(normalized) {
		switch {
		case strings.HasPrefix(token, "OPTIONAL MATCH "):
			seenOptional = true
		case strings.HasPrefix(token, "WHERE "):
			predicates := storySplitPredicates(strings.TrimPrefix(token, "WHERE "))
			if seenOptional {
				stranded = append(stranded, predicates...)
				continue
			}
			anchoring = append(anchoring, predicates...)
		}
	}
	return anchoring, stranded
}

// storyClauseTokens cuts a normalized statement at every clause keyword.
// Copy of story_grant_clause_fake_test.go.
func storyClauseTokens(normalized string) []string {
	keywords := []string{"OPTIONAL MATCH ", "MATCH ", "WHERE ", "WITH ", "RETURN ", "ORDER BY ", "SKIP ", "LIMIT "}
	tokens := make([]string, 0, 8)
	for index := 0; index < len(normalized); {
		next, keyword := storyNextClause(normalized, index+1, keywords)
		if next < 0 {
			tokens = append(tokens, normalized[index:])
			break
		}
		tokens = append(tokens, normalized[index:next])
		index = next
		_ = keyword
	}
	return tokens
}

func storyNextClause(normalized string, from int, keywords []string) (int, string) {
	earliest, found := -1, ""
	for _, keyword := range keywords {
		at := strings.Index(normalized[from:], keyword)
		if at < 0 {
			continue
		}
		at += from
		// A MATCH that is the tail of OPTIONAL MATCH is not its own clause.
		if keyword == "MATCH " && at >= len("OPTIONAL ") &&
			strings.HasSuffix(normalized[:at], "OPTIONAL ") {
			continue
		}
		if earliest < 0 || at < earliest {
			earliest, found = at, keyword
		}
	}
	return earliest, found
}

// storySplitPredicates splits an AND-joined predicate list, keeping a
// parenthesised grant condition -- which contains its own OR -- in one piece.
// Copy of story_grant_clause_fake_test.go.
func storySplitPredicates(block string) []string {
	parts := strings.Split(block, " AND ")
	predicates := make([]string, 0, len(parts))
	for _, part := range parts {
		predicates = append(predicates, strings.TrimSpace(part))
	}
	return predicates
}

// storyPredicateAdmits evaluates one repository predicate against a seed. A
// predicate this fake does not recognise admits the row. Copy of
// story_grant_clause_fake_test.go.
func storyPredicateAdmits(predicate string, repoByAlias map[string]string, params map[string]any) bool {
	for alias, repoID := range repoByAlias {
		switch {
		case strings.Contains(predicate, alias+".repo_id IN $allowed_repository_ids"):
			return querycontract.GraphParamContains(params, "allowed_repository_ids", repoID) ||
				querycontract.GraphParamContains(params, "allowed_scope_ids", repoID)
		case strings.Contains(predicate, alias+".repo_id = $repo_id"):
			bound, _ := params["repo_id"].(string)
			return repoID == bound && repoID != ""
		case strings.Contains(predicate, alias+".repo_id, '') IN $traversal_repo_ids"):
			return querycontract.GraphParamContains(params, "traversal_repo_ids", repoID)
		}
	}
	return true
}

// callChainRepoAliasAdmits evaluates the predicates on a Repository alias, whose
// grant key is its own id rather than a repo_id property.
func callChainRepoAliasAdmits(predicates []string, alias, repoID string, params map[string]any) bool {
	for _, predicate := range predicates {
		switch {
		case strings.Contains(predicate, alias+".id IN $allowed_repository_ids"):
			if !querycontract.GraphParamContains(params, "allowed_repository_ids", repoID) &&
				!querycontract.GraphParamContains(params, "allowed_scope_ids", repoID) {
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
