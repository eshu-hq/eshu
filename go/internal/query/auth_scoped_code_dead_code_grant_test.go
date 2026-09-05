// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// #5167 code-family batch 1, step 3: two-tenant grant proof for the three
// dead-code routes. All three reach exactly one candidate read --
// CodeHandler.deadCodeCandidateRows (code_dead_code_scan.go) -- and every
// probe downstream of it is keyed on entity ids that read already returned, so
// binding the grant there binds all three routes.
//
// The choke point has two backends and both are covered here:
//   - the content read model (ContentReader.DeadCodeCandidateRows), proved by
//     deadCodeGrantContentStore, whose filter mirrors the shipped SQL: an
//     explicit repo_id anchors the scan, a non-empty grant list restricts it,
//     an empty list does not restrict it at all.
//   - the graph fallback (buildDeadCodeGraphCypherForLabel), proved by
//     TestDeadCodeGraphCandidateScanBindsTheGrantInTheBuiltCypher, which
//     captures the Cypher the handler actually runs.
//
// dead-code/cross-repo keeps its consumer-side post-filter
// (filterCrossRepoDeadCodeEvidence); this covers its producer-side candidate
// scan, which is the read that had no grant of its own.
//
// This file covers the producer-side candidate scan the three routes share.
// dead-code/cross-repo's consumer-side reads -- the grant-bound evidence page
// and the ungranted-consumer probe -- are in
// auth_scoped_code_dead_code_cross_repo_grant_test.go.

type deadCodeGrantContentStore struct {
	fakeDeadCodeContentStore
	bound   []string
	queried bool
}

func (s *deadCodeGrantContentStore) DeadCodeCandidateRows(
	_ context.Context,
	q deadCodeCandidateQuery,
) ([]map[string]any, error) {
	s.queried = true
	s.bound = append([]string(nil), q.AllowedRepositoryIDs...)
	if q.Label != "Function" || q.Offset > 0 {
		return nil, nil
	}
	rows := make([]map[string]any, 0, 2)
	for _, repoID := range []string{codeGrantGrantedRepo, codeGrantOtherRepo} {
		if !codeContentGrantAdmits(repoID, q.RepoID, q.AllowedRepositoryIDs) {
			continue
		}
		rows = append(rows, map[string]any{
			"entity_id":  repoID + "#unusedHelper",
			"name":       "unusedHelper",
			"labels":     []any{"Function"},
			"file_path":  "internal/legacy/helper.go",
			"repo_id":    repoID,
			"repo_name":  repoID,
			"language":   "go",
			"start_line": 4,
			"end_line":   9,
		})
	}
	return rows, nil
}

type deadCodeGrantRoute struct {
	name string
	path string
	body map[string]any
}

// deadCodeGrantRoutes lists the three routes on the deadCodeCandidateRows
// choke point. cross-repo requires a producer repo_id, so its grant proof is
// the sharper "caller names a repository they were not granted" shape rather
// than the corpus-wide one the other two use.
func deadCodeGrantRoutes() []deadCodeGrantRoute {
	return []deadCodeGrantRoute{
		{name: "find_dead_code", path: "/api/v0/code/dead-code", body: map[string]any{"language": "go"}},
		{name: "investigate_dead_code", path: "/api/v0/code/dead-code/investigate", body: map[string]any{"language": "go"}},
	}
}

func TestDeadCodeRoutesFilterByRepositoryGrant(t *testing.T) {
	t.Parallel()

	for _, route := range deadCodeGrantRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			store := &deadCodeGrantContentStore{}
			handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)

			auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
			req := newCodeGrantRouteRequest(t, route.path, route.body, &auth)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if got := store.bound; !slices.Equal(got, []string{codeGrantGrantedRepo}) {
				t.Fatalf("AllowedRepositoryIDs = %#v, want [%q] bound into the candidate scan", got, codeGrantGrantedRepo)
			}
			body := rec.Body.String()
			if !strings.Contains(body, codeGrantGrantedRepo) {
				t.Fatalf("response missing the granted repository %q: %s", codeGrantGrantedRepo, body)
			}
			if strings.Contains(body, codeGrantOtherRepo) {
				t.Fatalf("response leaked the out-of-grant repository %q: %s", codeGrantOtherRepo, body)
			}
		})
	}
}

func TestDeadCodeRoutesEmptyGrantSkipsTheCandidateScan(t *testing.T) {
	t.Parallel()

	routes := append(deadCodeGrantRoutes(), deadCodeGrantRoute{
		name: "find_cross_repo_dead_code",
		path: "/api/v0/code/dead-code/cross-repo",
		body: map[string]any{"repo_id": codeGrantGrantedRepo, "language": "go"},
	})
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			store := &deadCodeGrantContentStore{}
			handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)

			auth := codeGrantScopedAuthContext(nil)
			req := newCodeGrantRouteRequest(t, route.path, route.body, &auth)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			// cross-repo requires a producer repo_id, so a grantless caller is
			// refused by the selector before the scan; the other two reach the
			// scan and must short-circuit inside it. Either way the store must
			// never be read.
			if store.queried {
				t.Fatalf("candidate store was queried (status %d), want no read at all -- an empty scoped grant must skip the candidate scan, not scan then filter to empty", rec.Code)
			}
			body := rec.Body.String()
			if strings.Contains(body, codeGrantOtherRepo) {
				t.Fatalf("response leaked the out-of-grant repository for an empty-grant caller: %s", body)
			}
		})
	}
}

func TestCrossRepoDeadCodeProducerScanCarriesTheGrant(t *testing.T) {
	t.Parallel()

	store := &deadCodeGrantContentStore{}
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	req := newCodeGrantRouteRequest(
		t,
		"/api/v0/code/dead-code/cross-repo",
		map[string]any{"repo_id": codeGrantGrantedRepo, "language": "go"},
		&auth,
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if !store.queried {
		t.Fatal("candidate store was never queried; the granted producer scan must still run")
	}
	if strings.Contains(rec.Body.String(), codeGrantOtherRepo) {
		t.Fatalf("response leaked the out-of-grant repository: %s", rec.Body.String())
	}
}

func TestDeadCodeRoutesSharedKeyScanIsUnchanged(t *testing.T) {
	t.Parallel()

	for _, route := range deadCodeGrantRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			store := &deadCodeGrantContentStore{}
			handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := newCodeGrantRouteRequest(t, route.path, route.body, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if len(store.bound) != 0 {
				t.Fatalf("AllowedRepositoryIDs = %#v, want empty for an unscoped caller -- the shared-key scan must stay unrestricted", store.bound)
			}
			body := rec.Body.String()
			if !strings.Contains(body, codeGrantGrantedRepo) || !strings.Contains(body, codeGrantOtherRepo) {
				t.Fatalf("unscoped response lost rows: %s", body)
			}
		})
	}
}

// TestDeadCodeGraphCandidateScanBindsTheGrantInTheBuiltCypher is the guard for
// the choke point's other backend. The handler tests above all drive the
// content read model, so the graph builder could lose its predicate and stay
// green. This captures the Cypher the handler actually hands the graph reader
// and asserts both halves of a working grant: the predicate text and the
// parameters it references.
func TestDeadCodeGraphCandidateScanBindsTheGrantInTheBuiltCypher(t *testing.T) {
	t.Parallel()

	var (
		captured string
		params   map[string]any
	)
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, gotParams map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "MATCH (e:") && captured == "" {
					captured = cypher
					params = gotParams
				}
				return nil, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	req := newCodeGrantRouteRequest(t, "/api/v0/code/dead-code", map[string]any{"language": "go"}, &auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if captured == "" {
		t.Fatal("no candidate Cypher was captured; the graph fallback did not run")
	}
	want := "(r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids)"
	if !strings.Contains(captured, want) {
		t.Fatalf("candidate Cypher is missing %q; a scoped caller's grant is resolved but never applied:\n%s", want, captured)
	}
	if got, ok := params["allowed_repository_ids"].([]string); !ok || !slices.Equal(got, []string{codeGrantGrantedRepo}) {
		t.Fatalf("params[allowed_repository_ids] = %#v, want [%q]; the predicate references a parameter that is never bound", params["allowed_repository_ids"], codeGrantGrantedRepo)
	}
	if _, ok := params["allowed_scope_ids"]; !ok {
		t.Fatalf("params = %#v, want an allowed_scope_ids binding for the predicate's second disjunct", params)
	}
}

// TestDeadCodeCandidateRowsBindTheGrantInTheShippedSQL is the content-backend
// half of the guard TestDeadCodeGraphCandidateScanBindsTheGrantInTheBuiltCypher
// provides for the graph backend. The handler tests drive a fake store, so the
// SQL string is never built: delete the grant clause from
// ContentReader.DeadCodeCandidateRows and every one of them still passes. This
// drives the real reader against a recording driver and reads the statement it
// actually sent.
func TestDeadCodeCandidateRowsBindTheGrantInTheShippedSQL(t *testing.T) {
	t.Parallel()

	t.Run("scoped", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
			columns: []string{
				"entity_id", "entity_name", "entity_type", "repo_id", "relative_path",
				"language", "start_line", "end_line", "metadata",
			},
		}})
		reader := NewContentReader(db)
		if _, err := reader.DeadCodeCandidateRows(context.Background(), deadCodeCandidateQuery{
			Label:                "Function",
			Limit:                10,
			AllowedRepositoryIDs: []string{codeGrantGrantedRepo},
		}); err != nil {
			t.Fatalf("DeadCodeCandidateRows() error = %v, want nil", err)
		}
		if len(recorder.queries) != 1 {
			t.Fatalf("query count = %d, want 1", len(recorder.queries))
		}
		if want := "AND repo_id = ANY($4)"; !strings.Contains(recorder.queries[0], want) {
			t.Fatalf("candidate SQL is missing %q; a scoped caller's grant is bound but never applied:\n%s", want, recorder.queries[0])
		}
		// The grant argument must land at $4, ahead of LIMIT/OFFSET, or the
		// placeholders the statement renders point at the wrong values.
		bound := fmt.Sprintf("%s", recorder.args[0][3])
		if !strings.Contains(bound, codeGrantGrantedRepo) {
			t.Fatalf("grant argument = %q, want the encoded Postgres array carrying %q", bound, codeGrantGrantedRepo)
		}
	})

	t.Run("unscoped", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
			columns: []string{
				"entity_id", "entity_name", "entity_type", "repo_id", "relative_path",
				"language", "start_line", "end_line", "metadata",
			},
		}})
		reader := NewContentReader(db)
		if _, err := reader.DeadCodeCandidateRows(context.Background(), deadCodeCandidateQuery{
			Label: "Function",
			Limit: 10,
		}); err != nil {
			t.Fatalf("DeadCodeCandidateRows() error = %v, want nil", err)
		}
		if strings.Contains(recorder.queries[0], "ANY(") {
			t.Fatalf("unscoped candidate SQL gained a grant clause:\n%s", recorder.queries[0])
		}
		if got, want := len(recorder.args[0]), 5; got != want {
			t.Fatalf("argument count = %d, want %d for an unscoped scan", got, want)
		}
	})
}
