// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
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

// TestCallChainEmptyGrantNamingARepositoryReachesNoBackend is the other caller
// shape, the same split the story route has. A grantless caller that names a
// repository is refused by the repository selector before the empty-grant gate
// is reached, so it gets a 400 rather than the route's own empty answer. No
// backend is read either way, which is the property both halves pin.
func TestCallChainEmptyGrantNamingARepositoryReachesNoBackend(t *testing.T) {
	t.Parallel()

	graph := &callChainGrantGraph{entities: callChainGrantEntities()}
	auth := codeGrantScopedAuthContext(nil)
	body := callChainRequestBody()
	body["repo_id"] = codeGrantGrantedRepo
	rec := runCallChainRequest(t, graph, body, &auth)
	if got, want := rec.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(graph.statements) != 0 {
		t.Fatalf("a grantless scoped caller reached the graph: %v", graph.statements)
	}
	if leaked := rec.Body.String(); strings.Contains(leaked, callChainUngrantedHop) {
		t.Fatalf("the refusal named an out-of-grant entity: %s", leaked)
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
			if len(graph.parseFailures) != 0 {
				t.Fatalf("the fake could not read the shipped statement, so it filtered nothing:\n%s",
					strings.Join(graph.parseFailures, "\n---\n"))
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
	if len(graph.parseFailures) != 0 {
		t.Fatalf("the fake could not read the shipped statement:\n%s",
			strings.Join(graph.parseFailures, "\n---\n"))
	}
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
	if len(graph.parseFailures) != 0 {
		t.Fatalf("the fake could not read the shipped statement:\n%s",
			strings.Join(graph.parseFailures, "\n---\n"))
	}
	if !strings.Contains(rec.Body.String(), callChainUngrantedNam) {
		t.Fatalf("the shared-key compat chain lost its cross-repository hop: %s", rec.Body.String())
	}
	for _, statement := range graph.statements {
		if strings.Contains(statement, "$allowed_repository_ids") {
			t.Fatalf("an unscoped caller rendered a grant array:\n%s", statement)
		}
	}
}
