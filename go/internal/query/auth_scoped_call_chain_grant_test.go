// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// #5167 code-family batch 2b: two-tenant grant proofs for
// POST /api/v0/code/call-chain.
//
// The route's traversal bound was already written and already inert. A live run
// against the pinned NornicDB measured nornicDBCallChainOneHopRows returning
// every callee -- including one in another repository, with its real repo_id in
// the projection -- while $traversal_repo_ids named a single repository, because
// the predicate sat after two OPTIONAL MATCH clauses. Pushing a caller's grant
// into that clause would have been grant text that granted nothing.
const (
	callChainGrantedStart = "fn:call-chain-granted-start"
	callChainGrantedEnd   = "fn:call-chain-granted-end"
	callChainGrantedName  = "CallChainGrantedEnd"
	callChainUngrantedHop = "fn:call-chain-ungranted-hop"
	callChainUngrantedNam = "CallChainUngrantedHop"
)

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
	endpointPredicates, hopPredicates := callChainClausePredicates(cypher)
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
func callChainClausePredicates(cypher string) (endpoints []string, hops []string) {
	normalized := normalizeCypherWhitespace(cypher)
	split := strings.Index(normalized, "MATCH path = shortestPath")
	if split < 0 {
		return nil, nil
	}
	head, tail := normalized[:split], normalized[split:]
	if at := strings.Index(head, "WHERE "); at >= 0 {
		endpoints = storySplitPredicates(strings.TrimSpace(head[at+len("WHERE "):]))
	}
	marker := "WHERE all(node IN nodes(path) WHERE "
	at := strings.Index(tail, marker)
	if at < 0 {
		return endpoints, nil
	}
	block := tail[at+len(marker):]
	end := strings.Index(block, ") RETURN ")
	if end < 0 {
		return endpoints, nil
	}
	return endpoints, storySplitPredicates(strings.TrimSpace(block[:end]))
}

// callChainHopAdmits evaluates one hop predicate. The hop conditions are written
// on coalesce(node.repo_id, ”) rather than a bare property, so they need their
// own matcher rather than storyPredicateAdmits.
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

func callChainGrantEntities() []callChainGrantEntity {
	return []callChainGrantEntity{
		{
			uid: callChainGrantedStart, name: "CallChainGrantedStart", repoID: codeGrantGrantedRepo,
			calls: []string{callChainGrantedEnd, callChainUngrantedHop},
		},
		{uid: callChainGrantedEnd, name: callChainGrantedName, repoID: codeGrantGrantedRepo},
		{uid: callChainUngrantedHop, name: callChainUngrantedNam, repoID: codeGrantOtherRepo},
	}
}

func runCallChainRequest(
	t *testing.T,
	graph GraphQuery,
	body map[string]any,
	auth *AuthContext,
) *httptest.ResponseRecorder {
	t.Helper()
	return runCallChainRequestOn(t, GraphBackendNornicDB, graph, body, auth)
}

func runCallChainRequestOn(
	t *testing.T,
	backend GraphBackend,
	graph GraphQuery,
	body map[string]any,
	auth *AuthContext,
) *httptest.ResponseRecorder {
	t.Helper()
	handler := &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: backend,
		Neo4j:        graph,
		Content:      &storyGrantContentStore{entities: map[string]EntityContent{}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newCodeGrantRouteRequest(t, "/api/v0/code/call-chain", body, auth))
	return rec
}

func callChainRequestBody() map[string]any {
	return map[string]any{
		"start_entity_id": callChainGrantedStart,
		"end_entity_id":   callChainUngrantedHop,
		"max_depth":       3,
	}
}

// TestCallChainFiltersByRepositoryGrant proves a scoped caller cannot route a
// chain through, or terminate it on, an entity outside its grant.
func TestCallChainFiltersByRepositoryGrant(t *testing.T) {
	t.Parallel()

	graph := &callChainGrantGraph{entities: callChainGrantEntities()}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runCallChainRequest(t, graph, callChainRequestBody(), &auth)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leaked := range []string{callChainUngrantedNam, codeGrantOtherRepo} {
		if strings.Contains(body, leaked) {
			t.Fatalf("the scoped call chain leaked %q: %s", leaked, body)
		}
	}
}

// TestCallChainBoundsEveryFrontierHop is the traversal half: the BFS applies the
// bound per frontier node, so an out-of-grant intermediate cannot join two
// granted endpoints.
func TestCallChainBoundsEveryFrontierHop(t *testing.T) {
	t.Parallel()

	graph := &callChainGrantGraph{entities: []callChainGrantEntity{
		{
			uid: callChainGrantedStart, name: "CallChainGrantedStart", repoID: codeGrantGrantedRepo,
			calls: []string{callChainUngrantedHop},
		},
		{
			uid: callChainUngrantedHop, name: callChainUngrantedNam, repoID: codeGrantOtherRepo,
			calls: []string{callChainGrantedEnd},
		},
		{uid: callChainGrantedEnd, name: callChainGrantedName, repoID: codeGrantGrantedRepo},
	}}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runCallChainRequest(t, graph, map[string]any{
		"start_entity_id": callChainGrantedStart,
		"end_entity_id":   callChainGrantedEnd,
		"max_depth":       3,
	}, &auth)
	body := rec.Body.String()
	if strings.Contains(body, callChainUngrantedNam) || strings.Contains(body, codeGrantOtherRepo) {
		t.Fatalf("the chain was routed through an out-of-grant hop: %s", body)
	}
	if !strings.Contains(body, `"chains":[]`) {
		t.Fatalf("a chain that only exists through an out-of-grant hop was still reported: %s", body)
	}
}

// TestCallChainEmptyGrantReachesNoBackend proves a grantless scoped caller is
// refused in front of the graph and gets the route's own empty answer.
func TestCallChainEmptyGrantReachesNoBackend(t *testing.T) {
	t.Parallel()

	graph := &callChainGrantGraph{entities: callChainGrantEntities()}
	auth := codeGrantScopedAuthContext(nil)
	rec := runCallChainRequest(t, graph, callChainRequestBody(), &auth)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(graph.statements) != 0 {
		t.Fatalf("a grantless scoped caller reached the graph: %v", graph.statements)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"chains":[]`) {
		t.Fatalf("empty-grant call chain is not the route's own empty answer: %s", body)
	}
}

// TestCallChainSharedKeyReadIsUnchanged pins the other direction.
func TestCallChainSharedKeyReadIsUnchanged(t *testing.T) {
	t.Parallel()

	graph := &callChainGrantGraph{entities: callChainGrantEntities()}
	rec := runCallChainRequest(t, graph, callChainRequestBody(), nil)
	body := rec.Body.String()
	if !strings.Contains(body, callChainUngrantedNam) {
		t.Fatalf("the shared-key call chain lost the cross-repository hop: %s", body)
	}
	for _, statement := range graph.statements {
		if strings.Contains(statement, "$allowed_repository_ids") {
			t.Fatalf("an unscoped caller rendered a grant array:\n%s", statement)
		}
	}
}

// TestCallChainResolvesAScopeOnlyGrantToItsRepository covers a grant that names
// the git ingestion scope rather than the repository it owns.
func TestCallChainResolvesAScopeOnlyGrantToItsRepository(t *testing.T) {
	t.Parallel()

	graph := &callChainGrantGraph{entities: callChainGrantEntities()}
	auth := AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-a",
		WorkspaceID:     "workspace-a",
		AllowedScopeIDs: []string{"git-repository-scope:" + codeGrantGrantedRepo},
	}
	rec := runCallChainRequest(t, graph, map[string]any{
		"start_entity_id": callChainGrantedStart,
		"end_entity_id":   callChainGrantedEnd,
		"max_depth":       3,
	}, &auth)
	body := rec.Body.String()
	if !strings.Contains(body, callChainGrantedName) {
		t.Fatalf("a scope-only grant did not reach the repository it owns: %s", body)
	}
	if strings.Contains(body, codeGrantOtherRepo) {
		t.Fatalf("scope-only call chain leaked %q: %s", codeGrantOtherRepo, body)
	}
}

// TestCallChainOneHopBindsTheGrantInTheAnchoringMatch is the shipped-text pin.
func TestCallChainOneHopBindsTheGrantInTheAnchoringMatch(t *testing.T) {
	t.Parallel()

	graph := &callChainGrantGraph{entities: callChainGrantEntities()}
	handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
	access := repositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}}
	ctx := ContextWithAuthContext(t.Context(), codeGrantScopedAuthContext([]string{codeGrantGrantedRepo}))
	if _, err := handler.nornicDBCallChainOneHopRows(
		ctx, callChainGrantedStart, "Function", []string{codeGrantGrantedRepo},
	); err != nil {
		t.Fatalf("nornicDBCallChainOneHopRows() error = %v", err)
	}
	if len(graph.statements) != 1 {
		t.Fatalf("statements = %d, want 1", len(graph.statements))
	}
	anchoring, stranded := storyClausePredicates(graph.statements[0])
	for _, want := range []string{
		"coalesce(target.repo_id, '') IN $traversal_repo_ids",
		access.GraphConditionOnProperty("target", "repo_id"),
	} {
		if !containsPredicate(anchoring, want) {
			t.Fatalf("the anchoring MATCH does not carry %q:\n%s", want, graph.statements[0])
		}
		if containsPredicate(stranded, want) {
			t.Fatalf("%q sits after an OPTIONAL MATCH, where it filters nothing:\n%s", want, graph.statements[0])
		}
	}
}

func containsPredicate(predicates []string, want string) bool {
	for _, predicate := range predicates {
		if strings.Contains(predicate, want) {
			return true
		}
	}
	return false
}

// TestExactGraphEntityCandidatesRefuseAnUngrantedRepository covers the
// defense-in-depth check directly, because no route reaches it with an
// out-of-grant repository today: every caller resolves repo_id through the
// selector first. The check exists for the path that stops doing that, and its
// rows become an ambiguity error that names entity ids.
func TestExactGraphEntityCandidatesRefuseAnUngrantedRepository(t *testing.T) {
	t.Parallel()

	content := &storyGrantContentStore{byName: []EntityContent{
		{
			EntityID: storyUngrantedEntity, EntityName: callChainUngrantedNam,
			EntityType: "Function", RepoID: codeGrantOtherRepo,
		},
	}}
	ctx := ContextWithAuthContext(t.Context(), codeGrantScopedAuthContext([]string{codeGrantGrantedRepo}))
	rows, err := resolveExactGraphEntityCandidates(ctx, content, codeGrantOtherRepo, callChainUngrantedNam)
	if err != nil {
		t.Fatalf("resolveExactGraphEntityCandidates() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("candidates = %#v, want none for a repository outside the grant", rows)
	}
	if len(content.askedRepo) != 0 {
		t.Fatalf("the content store was read for an out-of-grant repository: %v", content.askedRepo)
	}
	if _, err := resolveExactGraphEntityCandidates(ctx, content, codeGrantGrantedRepo, callChainUngrantedNam); err != nil {
		t.Fatalf("resolveExactGraphEntityCandidates() error = %v", err)
	}
	if len(content.askedRepo) != 1 {
		t.Fatalf("a granted repository was not read: %v", content.askedRepo)
	}
}

// TestRelationshipMetadataAnchorBindsTheGrant covers the shared metadata lookup
// directly. It is defense in depth on call-chain -- the one-hop read already
// drops an out-of-grant hop, so no call-chain response changes when this
// predicate is removed -- but it is the only repository binding on the
// statement that resolves an endpoint by name, and it is shared with
// POST /api/v0/code/relationships. A route-level assertion cannot judge it, so
// the statement is judged instead.
func TestRelationshipMetadataAnchorBindsTheGrant(t *testing.T) {
	t.Parallel()

	access := repositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}}
	predicate, params := nornicDBRelationshipMetadataPredicate("CallChainGrantedStart", "", access)
	cypher := nornicDBRelationshipMetadataCypher(predicate, "Function", "uid")
	anchoring, stranded := storyClausePredicates(cypher)
	condition := access.GraphCondition("repo")
	if !containsPredicate(anchoring, condition) {
		t.Fatalf("the metadata anchor does not bind the grant:\n%s", cypher)
	}
	if containsPredicate(stranded, condition) {
		t.Fatalf("the metadata grant sits after an OPTIONAL MATCH:\n%s", cypher)
	}
	if !graphParamContains(params, "allowed_repository_ids", codeGrantGrantedRepo) {
		t.Fatalf("params do not bind the grant array: %#v", params)
	}

	unscoped, unscopedParams := nornicDBRelationshipMetadataPredicate("CallChainGrantedStart", "", repositoryAccessFilter{AllScopes: true})
	if strings.Contains(nornicDBRelationshipMetadataCypher(unscoped, "Function", "uid"), "$allowed_repository_ids") {
		t.Fatalf("an unscoped caller rendered a grant condition")
	}
	if _, ok := unscopedParams["allowed_repository_ids"]; ok {
		t.Fatalf("an unscoped caller bound a grant array: %#v", unscopedParams)
	}
}

// callChainBridgedEntities is the graph the interior-hop proofs need: two
// in-repository endpoints whose only route crosses the other repository.
func callChainBridgedEntities() []callChainGrantEntity {
	return []callChainGrantEntity{
		{
			uid: callChainGrantedStart, name: "CallChainGrantedStart", repoID: codeGrantGrantedRepo,
			calls: []string{callChainUngrantedHop},
		},
		{
			uid: callChainUngrantedHop, name: callChainUngrantedNam, repoID: codeGrantOtherRepo,
			calls: []string{callChainGrantedEnd},
		},
		{uid: callChainGrantedEnd, name: callChainGrantedName, repoID: codeGrantGrantedRepo},
	}
}

// TestCallChainNeo4jLaneBoundsInteriorHops is the Neo4j-compat counterpart of
// TestCallChainBoundsEveryFrontierHop.
//
// The two lanes reach the same guarantee by different shapes, and the compat one
// is the one that had no coverage: its statement binds the two endpoints in the
// anchoring WHERE and returns EVERY node on the path, so a chain whose endpoints
// are both in grant could still ship an interior hop's id, name, language and
// docstring from a repository the token was never granted.
func TestCallChainNeo4jLaneBoundsInteriorHops(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{
			name: "no_repository_anchor",
			body: map[string]any{
				"start_entity_id": callChainGrantedStart,
				"end_entity_id":   callChainGrantedEnd,
				"max_depth":       3,
			},
		},
		{
			name: "repo_id_anchor",
			body: map[string]any{
				"start_entity_id": callChainGrantedStart,
				"end_entity_id":   callChainGrantedEnd,
				"repo_id":         codeGrantGrantedRepo,
				"max_depth":       3,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			graph := &callChainGrantGraph{entities: callChainBridgedEntities()}
			auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
			rec := runCallChainRequestOn(t, GraphBackendNeo4j, graph, tc.body, &auth)
			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			body := rec.Body.String()
			if strings.Contains(body, callChainUngrantedNam) || strings.Contains(body, callChainUngrantedHop) {
				t.Fatalf("the compat chain shipped an out-of-grant interior hop: %s", body)
			}
			if !strings.Contains(body, `"chains":[]`) {
				t.Fatalf("a chain that only exists through an out-of-grant hop was still reported: %s", body)
			}
		})
	}
}

// TestCallChainNeo4jLaneKeepsAnInGrantChain is the other direction: the same
// statement must still return a chain whose every hop is in grant, so the fix
// narrows rather than empties.
func TestCallChainNeo4jLaneKeepsAnInGrantChain(t *testing.T) {
	t.Parallel()

	graph := &callChainGrantGraph{entities: []callChainGrantEntity{
		{
			uid: callChainGrantedStart, name: "CallChainGrantedStart", repoID: codeGrantGrantedRepo,
			calls: []string{callChainGrantedEnd},
		},
		{uid: callChainGrantedEnd, name: callChainGrantedName, repoID: codeGrantGrantedRepo},
	}}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runCallChainRequestOn(t, GraphBackendNeo4j, graph, map[string]any{
		"start_entity_id": callChainGrantedStart,
		"end_entity_id":   callChainGrantedEnd,
		"max_depth":       3,
	}, &auth)
	if !strings.Contains(rec.Body.String(), callChainGrantedName) {
		t.Fatalf("the compat chain lost an in-grant hop: %s", rec.Body.String())
	}
}

// TestCallChainNeo4jLaneSharedKeyReadIsUnchanged pins that the compat statement
// renders no grant for an unscoped caller and still returns the chain.
func TestCallChainNeo4jLaneSharedKeyReadIsUnchanged(t *testing.T) {
	t.Parallel()

	graph := &callChainGrantGraph{entities: callChainBridgedEntities()}
	rec := runCallChainRequestOn(t, GraphBackendNeo4j, graph, map[string]any{
		"start_entity_id": callChainGrantedStart,
		"end_entity_id":   callChainGrantedEnd,
		"max_depth":       3,
	}, nil)
	if !strings.Contains(rec.Body.String(), callChainUngrantedNam) {
		t.Fatalf("the shared-key compat chain lost its cross-repository hop: %s", rec.Body.String())
	}
	for _, statement := range graph.statements {
		if strings.Contains(statement, "$allowed_repository_ids") {
			t.Fatalf("an unscoped caller rendered a grant array:\n%s", statement)
		}
	}
}

// TestShortestPathCallChainBuildersBindTheGrant is the shipped-text pin for both
// shortestPath builders, with a SCOPED filter -- the caller class every other
// call site of these two builders omits.
func TestShortestPathCallChainBuildersBindTheGrant(t *testing.T) {
	t.Parallel()

	access := repositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}}
	req := callChainRequest{
		StartEntityID: callChainGrantedStart,
		EndEntityID:   callChainGrantedEnd,
		MaxDepth:      3,
	}

	t.Run("neo4j_compat_binds_endpoints_and_every_hop", func(t *testing.T) {
		t.Parallel()
		cypher, params := buildCallChainCypher(req, GraphBackendNeo4j, access)
		endpoints, hops := callChainClausePredicates(cypher)
		for _, want := range []string{
			access.GraphConditionOnProperty("start", "repo_id"),
			access.GraphConditionOnProperty("end", "repo_id"),
		} {
			if !containsPredicate(endpoints, want) {
				t.Fatalf("the anchoring WHERE does not carry %q:\n%s", want, cypher)
			}
		}
		if !containsPredicate(hops, "IN $allowed_repository_ids") {
			t.Fatalf("no hop predicate binds the grant, so an interior hop is unbounded:\n%s", cypher)
		}
		if !graphParamContains(params, "allowed_repository_ids", codeGrantGrantedRepo) {
			t.Fatalf("params do not bind the grant array: %#v", params)
		}
	})

	t.Run("nornicdb_binds_endpoints", func(t *testing.T) {
		t.Parallel()
		cypher, params := buildNornicDBCallChainCypher(req, access)
		endpoints, hops := callChainClausePredicates(cypher)
		for _, want := range []string{
			access.GraphConditionOnProperty("start", "repo_id"),
			access.GraphConditionOnProperty("end", "repo_id"),
		} {
			if !containsPredicate(endpoints, want) {
				t.Fatalf("the anchoring WHERE does not carry %q:\n%s", want, cypher)
			}
		}
		// Deliberately no hop predicate: a list-membership test inside
		// all(node IN nodes(path) ...) is not evaluated on the pinned NornicDB
		// build, so writing one here would be grant text that grants nothing.
		// The live NornicDB path bounds each hop as its traversal expands.
		if containsPredicate(hops, "IN $allowed_repository_ids") {
			t.Fatalf("the NornicDB builder gained a hop predicate the backend does not evaluate:\n%s", cypher)
		}
		if !graphParamContains(params, "allowed_repository_ids", codeGrantGrantedRepo) {
			t.Fatalf("params do not bind the grant array: %#v", params)
		}
	})

	t.Run("unscoped_carries_no_grant", func(t *testing.T) {
		t.Parallel()
		for name, cypher := range map[string]string{
			"neo4j_compat": firstOf(buildCallChainCypher(req, GraphBackendNeo4j, repositoryAccessFilter{AllScopes: true})),
			"nornicdb":     firstOf(buildNornicDBCallChainCypher(req, repositoryAccessFilter{AllScopes: true})),
		} {
			if strings.Contains(cypher, "$allowed_repository_ids") || strings.Contains(cypher, "$allowed_scope_ids") {
				t.Fatalf("%s rendered a grant for an unscoped caller:\n%s", name, cypher)
			}
		}
	})
}
