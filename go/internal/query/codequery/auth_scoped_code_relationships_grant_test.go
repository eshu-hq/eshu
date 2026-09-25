// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// #5167: two-tenant grant proofs for the content-store half of
// POST /api/v0/code/relationships.
//
// The graph half is proven live (relationships_grant_live_test.go). This file
// covers what the handler does once the graph read returns no row -- which is
// exactly what the grant-bound metadata read returns for an anchor outside the
// caller's grant. relationshipsFromContent then resolved the entity from
// Postgres with no grant check and returned its name, path, repository and
// neighbours, so an ungranted entity_id read through the fallback what the
// graph read had just refused.
const (
	relGrantGrantedID   = "entity:rel-granted-helper"
	relGrantGrantedName = "RelGrantedHelper"
	relGrantOtherID     = "entity:rel-other-helper"
	relGrantOtherName   = "RelOtherHelper"
	relGrantSharedName  = "RelSharedName"
	relGrantSharedAID   = "entity:rel-shared-a"
	relGrantSharedBID   = "entity:rel-shared-b"
)

func relGrantEntities() []EntityContent {
	return []EntityContent{
		{EntityID: relGrantGrantedID, EntityName: relGrantGrantedName, EntityType: "Function", RepoID: codeGrantGrantedRepo, RelativePath: "internal/granted.go", Language: "go"},
		{EntityID: relGrantOtherID, EntityName: relGrantOtherName, EntityType: "Function", RepoID: codeGrantOtherRepo, RelativePath: "internal/other.go", Language: "go"},
		{EntityID: relGrantSharedAID, EntityName: relGrantSharedName, EntityType: "Function", RepoID: codeGrantGrantedRepo, RelativePath: "internal/shared.go", Language: "go"},
		{EntityID: relGrantSharedBID, EntityName: relGrantSharedName, EntityType: "Function", RepoID: codeGrantOtherRepo, RelativePath: "internal/shared.go", Language: "go"},
	}
}

type relGrantFixture struct {
	graph   *relGrantCountingGraph
	content *relGrantContentStore
	builder *relGrantContentBuilder
	handler *CodeHandler
}

func newRelGrantFixture(backend GraphBackend) relGrantFixture {
	graph := &relGrantCountingGraph{}
	store := &relGrantContentStore{entities: relGrantEntities()}
	builder := &relGrantContentBuilder{}
	return relGrantFixture{
		graph:   graph,
		content: store,
		builder: builder,
		handler: &CodeHandler{
			Profile:              ProfileLocalAuthoritative,
			GraphBackend:         backend,
			Neo4j:                graph,
			Content:              store,
			ContentRelationships: builder,
		},
	}
}

func (f relGrantFixture) serve(t *testing.T, body map[string]any, auth *AuthContext) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	f.handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newCodeGrantRouteRequest(t, "/api/v0/code/relationships", body, auth))
	return rec
}

func relGrantBackends() []GraphBackend {
	return []GraphBackend{GraphBackendNornicDB, GraphBackendNeo4j}
}

// TestCodeRelationshipsUngrantedEntityIDIsIndistinguishableFromUnknown: an
// entity_id outside the grant must answer exactly what an entity_id that does
// not exist answers, and must never reach the relationship builder.
func TestCodeRelationshipsUngrantedEntityIDIsIndistinguishableFromUnknown(t *testing.T) {
	t.Parallel()
	for _, backend := range relGrantBackends() {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			auth := testutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
			fixture := newRelGrantFixture(backend)
			ungranted := fixture.serve(t, map[string]any{"entity_id": relGrantOtherID}, &auth)
			unknown := newRelGrantFixture(backend).serve(t, map[string]any{"entity_id": "entity:does-not-exist"}, &auth)

			if got, want := ungranted.Code, http.StatusNotFound; got != want {
				t.Fatalf("ungranted entity_id status = %d, want %d; body = %s", got, want, ungranted.Body.String())
			}
			if ungranted.Body.String() != unknown.Body.String() {
				t.Fatalf("ungranted body differs from unknown body:\nungranted: %s\nunknown:   %s", ungranted.Body.String(), unknown.Body.String())
			}
			if n := fixture.builder.builtCount(); n != 0 {
				t.Fatalf("relationship builder ran %d time(s) for an ungranted anchor", n)
			}
		})
	}
}

// TestCodeRelationshipsUngrantedNameIsIndistinguishableFromUnknown: a name that
// exists only outside the grant, with no repo_id, used to resolve through the
// corpus-wide SearchEntitiesByNameAnyRepo read and return the other tenant's
// entity.
func TestCodeRelationshipsUngrantedNameIsIndistinguishableFromUnknown(t *testing.T) {
	t.Parallel()
	for _, backend := range relGrantBackends() {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			auth := testutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
			fixture := newRelGrantFixture(backend)
			ungranted := fixture.serve(t, map[string]any{"name": relGrantOtherName}, &auth)
			unknown := newRelGrantFixture(backend).serve(t, map[string]any{"name": "RelNoSuchSymbol"}, &auth)

			if got, want := ungranted.Code, http.StatusNotFound; got != want {
				t.Fatalf("ungranted name status = %d, want %d; body = %s", got, want, ungranted.Body.String())
			}
			if ungranted.Body.String() != unknown.Body.String() {
				t.Fatalf("ungranted body differs from unknown body:\nungranted: %s\nunknown:   %s", ungranted.Body.String(), unknown.Body.String())
			}
			if fixture.content.anyRepoReads != 0 {
				t.Fatalf("scoped caller issued %d corpus-wide name read(s)", fixture.content.anyRepoReads)
			}
			for _, repoID := range fixture.content.repoReads {
				if repoID != codeGrantGrantedRepo {
					t.Fatalf("scoped caller read repository %q outside its grant", repoID)
				}
			}
		})
	}
}

// TestCodeRelationshipsNameResolvesTheGrantedCopy: a name held by both tenants
// resolves to the granted copy for a scoped caller. The corpus-wide read saw
// two matches and answered not-found, so the leak fix is also an accuracy fix.
func TestCodeRelationshipsNameResolvesTheGrantedCopy(t *testing.T) {
	t.Parallel()
	for _, backend := range relGrantBackends() {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			auth := testutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
			rec := newRelGrantFixture(backend).serve(t, map[string]any{"name": relGrantSharedName}, &auth)
			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, relGrantSharedAID) {
				t.Fatalf("the granted copy %q is missing: %s", relGrantSharedAID, body)
			}
			for _, leaked := range []string{relGrantSharedBID, codeGrantOtherRepo} {
				if strings.Contains(body, leaked) {
					t.Fatalf("the scoped read leaked %q: %s", leaked, body)
				}
			}
		})
	}
}

// TestCodeRelationshipsEmptyGrantReadsNothing: a scoped caller with no grant
// reaches neither backend and answers what an unknown entity answers.
func TestCodeRelationshipsEmptyGrantReadsNothing(t *testing.T) {
	t.Parallel()
	for _, backend := range relGrantBackends() {
		for _, body := range []map[string]any{
			{"entity_id": relGrantGrantedID},
			{"name": relGrantGrantedName},
			{"entity_id": relGrantGrantedID, "relationship_type": "CALLS", "direction": "outgoing", "transitive": true},
		} {
			t.Run(string(backend), func(t *testing.T) {
				t.Parallel()
				empty := testutil.CodeGrantScopedAuthContext(nil)
				fixture := newRelGrantFixture(backend)
				rec := fixture.serve(t, body, &empty)
				if got, want := rec.Code, http.StatusNotFound; got != want {
					t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
				}
				if n := fixture.graph.count(); n != 0 {
					t.Fatalf("empty grant issued %d graph read(s)", n)
				}
				if n := fixture.content.reads; n != 0 {
					t.Fatalf("empty grant issued %d content read(s)", n)
				}
				granted := testutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
				unknownBody := map[string]any{}
				for k, v := range body {
					unknownBody[k] = v
				}
				if _, ok := unknownBody["entity_id"]; ok {
					unknownBody["entity_id"] = "entity:does-not-exist"
				} else {
					unknownBody["name"] = "RelNoSuchSymbol"
				}
				unknown := newRelGrantFixture(backend).serve(t, unknownBody, &granted)
				if rec.Body.String() != unknown.Body.String() {
					t.Fatalf("empty-grant body differs from unknown body:\nempty:   %s\nunknown: %s", rec.Body.String(), unknown.Body.String())
				}
			})
		}
	}
}

// TestCodeRelationshipsSharedKeyContentFallbackIsUnchanged pins the other
// direction: an unscoped caller still resolves either tenant's entity through
// the fallback, and a shared name stays ambiguous (not found) corpus-wide.
func TestCodeRelationshipsSharedKeyContentFallbackIsUnchanged(t *testing.T) {
	t.Parallel()
	for _, backend := range relGrantBackends() {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			for _, id := range []string{relGrantGrantedID, relGrantOtherID} {
				rec := newRelGrantFixture(backend).serve(t, map[string]any{"entity_id": id}, nil)
				if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), id) {
					t.Fatalf("shared-key entity_id %q: status = %d body = %s", id, rec.Code, rec.Body.String())
				}
			}
			rec := newRelGrantFixture(backend).serve(t, map[string]any{"name": relGrantOtherName}, nil)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), relGrantOtherID) {
				t.Fatalf("shared-key name: status = %d body = %s", rec.Code, rec.Body.String())
			}
			rec = newRelGrantFixture(backend).serve(t, map[string]any{"name": relGrantSharedName}, nil)
			if got, want := rec.Code, http.StatusNotFound; got != want {
				t.Fatalf("shared-key ambiguous name status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

// TestCodeRelationshipsUngrantedRepoSelectorIsRejected: a repo_id outside the
// grant is refused by the selector with 400 before any backend read.
func TestCodeRelationshipsUngrantedRepoSelectorIsRejected(t *testing.T) {
	t.Parallel()
	for _, backend := range relGrantBackends() {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			auth := testutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
			fixture := newRelGrantFixture(backend)
			rec := fixture.serve(t, map[string]any{"name": relGrantOtherName, "repo_id": codeGrantOtherRepo}, &auth)
			if got, want := rec.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), relGrantOtherID) {
				t.Fatalf("rejection leaked the entity: %s", rec.Body.String())
			}
			if n := fixture.graph.count() + fixture.content.reads; n != 0 {
				t.Fatalf("ungranted selector issued %d backend read(s)", n)
			}
		})
	}
}
