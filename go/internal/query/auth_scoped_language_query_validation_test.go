// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
	"testing"
)

// #5167 code-family batch 2a review round 2, finding 1.
//
// handleLanguageQuery validates entity_type by falling off the end of its
// dispatch chain: the four `if label, ok := <map>[req.EntityType]` branches all
// miss and the tail writes the documented 400. The empty-grant short-circuit
// added by the grant binding returns an empty 200 BEFORE that dispatch, so a
// scoped caller with no repository grants used to get a success page for an
// entity type the route does not support, while every other caller got the 400.
// Two callers disagreeing about whether a request is well-formed is a contract
// bug on its own, and the difference is also a signal about the caller's own
// grant state that the empty page was written to avoid leaking.
//
// The fix validates the entity type with the rest of the request -- after the
// profile capability gate, ahead of the selector's graph read and the grant
// short-circuit -- so request validity is answered the same way for every
// caller. These tests pin both callers.

// languageQueryUnsupportedEntityType is a value no entry of
// graphBackedEntityTypes, graphFirstContentBackedEntityTypes, or
// contentBackedEntityTypes carries, so every dispatch branch misses it.
const languageQueryUnsupportedEntityType = "bogus"

// newLanguageQueryValidationHandler builds a handler whose backends both record
// every call, so a test can prove a rejected request reached neither.
func newLanguageQueryValidationHandler() (*LanguageQueryHandler, *languageQueryPlainContentStore, *evaluatingRepositoryGraph) {
	store := &languageQueryPlainContentStore{}
	graph := &evaluatingRepositoryGraph{
		seeds:             languageQueryGraphSeeds("Function"),
		repositoryAlias:   "r",
		repositoryColumns: repositoryProjectedColumns(),
	}
	return &LanguageQueryHandler{
		Neo4j:   graph,
		Content: store,
		Profile: ProfileLocalAuthoritative,
	}, store, graph
}

// TestLanguageQueryRejectsUnsupportedEntityTypeForEveryCaller is the regression
// test: the grantless scoped caller and the unscoped one must get the same 400,
// and neither may reach a backend on the way to it.
func TestLanguageQueryRejectsUnsupportedEntityTypeForEveryCaller(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		auth *AuthContext
	}{
		{name: "scoped caller with no repository grants", auth: authContextPointer(codeGrantScopedAuthContext(nil))},
		{name: "scoped caller with a repository grant", auth: authContextPointer(codeGrantScopedAuthContext([]string{codeGrantGrantedRepo}))},
		{name: "unscoped caller", auth: nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			handler, store, graph := newLanguageQueryValidationHandler()
			rec := runLanguageQueryGrantRequest(
				t, handler,
				languageQueryGrantBody(languageQueryUnsupportedEntityType),
				testCase.auth,
			)

			if got, want := rec.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, "unsupported entity_type") {
				t.Fatalf("body does not carry the documented unsupported-entity message: %s", body)
			}
			if !strings.Contains(body, languageQueryUnsupportedEntityType) {
				t.Fatalf("body does not name the rejected entity type: %s", body)
			}
			if len(store.askedRepoIDs) != 0 {
				t.Fatalf("content store was queried with %#v for an invalid request", store.askedRepoIDs)
			}
			if len(graph.statements) != 0 {
				t.Fatalf("graph was read for an invalid request: %v", graph.statements)
			}
		})
	}
}

// TestLanguageQueryEmptyGrantStillAnswersASupportedEntityType pins the other
// half: moving the validation ahead of the short-circuit must not turn the
// grantless caller's normal empty page into an error.
func TestLanguageQueryEmptyGrantStillAnswersASupportedEntityType(t *testing.T) {
	t.Parallel()

	handler, store, graph := newLanguageQueryValidationHandler()
	auth := codeGrantScopedAuthContext(nil)
	rec := runLanguageQueryGrantRequest(t, handler, languageQueryGrantBody("function"), &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(store.askedRepoIDs) != 0 || len(graph.statements) != 0 {
		t.Fatalf("a grantless scoped caller reached a backend: content %#v, graph %v", store.askedRepoIDs, graph.statements)
	}
	data := decodeEnvelopeData(t, rec.Body.Bytes())
	rows, ok := data["results"].([]any)
	if !ok || len(rows) != 0 {
		t.Fatalf("results = %#v, want an empty JSON array", data["results"])
	}
}

// authContextPointer lifts a value-returning AuthContext helper into the
// pointer the route request builder takes, so a table case can carry nil for
// the unscoped caller.
func authContextPointer(auth AuthContext) *AuthContext {
	return &auth
}
