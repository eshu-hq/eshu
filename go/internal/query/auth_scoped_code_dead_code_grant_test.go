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

// codeGrantConsumerRepo is a second repository inside the caller's grant, so
// the cross-repo consumer tests can tell "dropped because ungranted" apart
// from "dropped because it is the producer".
const codeGrantConsumerRepo = "repo://tenant-a/consumer-service"

// crossRepoDeadCodeGrantStore answers both reads POST
// /api/v0/code/dead-code/cross-repo makes: the producer candidate scan and the
// consumer-evidence lookup. The evidence half mirrors both statements the
// shipped reader runs -- the grant-bound page, which excludes the consumers the
// caller may not see, and the ungranted signal read, which returns every
// cross-repo consumer -- so a handler that stops passing the grant gets the
// other tenant's consumer back in the page.
type crossRepoDeadCodeGrantStore struct {
	deadCodeGrantContentStore
	boundConsumerGrant []string
	signalRead         bool
}

func (s *crossRepoDeadCodeGrantStore) CrossRepoDeadCodeConsumerEvidence(
	_ context.Context,
	producerRepoID string,
	entityIDs []string,
	reads crossRepoDeadCodeConsumerReads,
) (map[string][]crossRepoDeadCodeEvidence, map[string][]crossRepoDeadCodeEvidence, error) {
	s.boundConsumerGrant = append([]string(nil), reads.PageRepositoryIDs...)
	s.signalRead = reads.Signal
	evidence := make(map[string][]crossRepoDeadCodeEvidence, len(entityIDs))
	signal := make(map[string][]crossRepoDeadCodeEvidence, len(entityIDs))
	for _, entityID := range entityIDs {
		for _, consumerRepoID := range []string{codeGrantConsumerRepo, codeGrantOtherRepo} {
			if consumerRepoID == producerRepoID {
				continue
			}
			row := crossRepoDeadCodeGrantConsumerRow(consumerRepoID, entityID)
			if reads.Signal {
				signal[entityID] = append(signal[entityID], row)
			}
			if len(reads.PageRepositoryIDs) > 0 && !slices.Contains(reads.PageRepositoryIDs, consumerRepoID) {
				continue
			}
			evidence[entityID] = append(evidence[entityID], row)
		}
	}
	return evidence, signal, nil
}

func crossRepoDeadCodeGrantConsumerRow(consumerRepoID string, entityID string) crossRepoDeadCodeEvidence {
	return crossRepoDeadCodeEvidence{
		ConsumerRepoID:   consumerRepoID,
		ConsumerRepoName: consumerRepoID,
		ConsumerEntityID: consumerRepoID + "#caller",
		RelationshipType: "CALLS",
		EvidenceFamily:   "direct_code",
		Citation:         "code_reachability_rows:g1/" + consumerRepoID + "/caller/" + entityID,
		Confidence:       0.95,
		ConfidenceLabel:  "high",
		ResolutionMethod: "bounded_lookup",
		Depth:            1,
		GenerationID:     "g1",
		GenerationStatus: "active",
	}
}

func runCrossRepoDeadCodeGrantRequest(
	t *testing.T,
	store *crossRepoDeadCodeGrantStore,
	auth *AuthContext,
) *httptest.ResponseRecorder {
	t.Helper()

	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := map[string]any{"repo_id": codeGrantGrantedRepo, "language": "go"}
	req := newCodeGrantRouteRequest(t, "/api/v0/code/dead-code/cross-repo", body, auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestCrossRepoDeadCodeConsumerEvidenceIsGrantBound closes the last read on
// this route that reached Postgres with no grant. The consumer rows were
// fetched for every tenant and dropped in Go after the LIMIT, so a page could
// be filled with another tenant's consumers and the granted ones pushed off it.
func TestCrossRepoDeadCodeConsumerEvidenceIsGrantBound(t *testing.T) {
	t.Parallel()

	store := &crossRepoDeadCodeGrantStore{}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo, codeGrantConsumerRepo})
	rec := runCrossRepoDeadCodeGrantRequest(t, store, &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if !slices.Equal(store.boundConsumerGrant, []string{codeGrantConsumerRepo, codeGrantGrantedRepo}) {
		t.Fatalf("consumer-evidence grant = %#v, want both granted repositories in sorted order", store.boundConsumerGrant)
	}
	body := rec.Body.String()
	if !strings.Contains(body, codeGrantConsumerRepo) {
		t.Fatalf("response lost the granted consumer %q: %s", codeGrantConsumerRepo, body)
	}
	if strings.Contains(body, codeGrantOtherRepo) {
		t.Fatalf("response leaked the out-of-grant consumer %q: %s", codeGrantOtherRepo, body)
	}
}

// TestCrossRepoDeadCodeKeepsTheHiddenConsumerSignal is the other half. Filtering
// the ungranted consumers out in SQL must not turn a symbol that has one into
// dead code: the reader reports how many it excluded, and the handler still
// answers unknown_needs_evidence with permission_hidden_consumer.
func TestCrossRepoDeadCodeKeepsTheHiddenConsumerSignal(t *testing.T) {
	t.Parallel()

	store := &crossRepoDeadCodeGrantStore{}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runCrossRepoDeadCodeGrantRequest(t, store, &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, codeGrantOtherRepo) {
		t.Fatalf("response leaked the out-of-grant consumer %q: %s", codeGrantOtherRepo, body)
	}
	if !strings.Contains(body, "permission_hidden_consumer") {
		t.Fatalf("a candidate whose only consumer is out of grant must stay unknown_needs_evidence: %s", body)
	}
	if !strings.Contains(body, `"hidden_consumer_evidence_count":2`) {
		t.Fatalf("hidden consumer count is missing from the answer; both consumers are outside this grant: %s", body)
	}
	if strings.Contains(body, `"classification":"dead"`) {
		t.Fatalf("a symbol with an out-of-grant consumer was marked dead: %s", body)
	}
}

// TestCrossRepoDeadCodeConsumerEvidenceBindsTheGrantInTheShippedSQL drives the
// real reader against a recording driver, because the handler tests above
// drive a fake store and never build the statement.
func TestCrossRepoDeadCodeConsumerEvidenceBindsTheGrantInTheShippedSQL(t *testing.T) {
	t.Parallel()

	t.Run("scoped", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
			{columns: crossRepoDeadCodeEvidenceColumns()},
			{columns: []string{"entity_id", "hidden_count"}},
		})
		reader := NewContentReader(db)
		if _, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
			context.Background(),
			codeGrantGrantedRepo,
			[]string{"entity-1"},
			crossRepoDeadCodeConsumerReads{PageRepositoryIDs: []string{codeGrantConsumerRepo}, Signal: true},
		); err != nil {
			t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
		}
		if len(recorder.queries) != 2 {
			t.Fatalf("query count = %d, want 2 (grant-bound evidence page plus ungranted signal read)", len(recorder.queries))
		}
		want := "AND row.repository_id = ANY($3)"
		if !strings.Contains(recorder.queries[0], want) {
			t.Fatalf("consumer-evidence SQL is missing %q, so the LIMIT is still drawn from every tenant's rows:\n%s", want, recorder.queries[0])
		}
		if strings.Contains(recorder.queries[1], "ANY(") {
			t.Fatalf("the signal read must carry no grant clause; it is what reports a consumer the caller cannot see:\n%s", recorder.queries[1])
		}
		bound := fmt.Sprintf("%s", recorder.args[0][2])
		if !strings.Contains(bound, codeGrantConsumerRepo) {
			t.Fatalf("grant argument = %q, want the encoded Postgres array carrying %q", bound, codeGrantConsumerRepo)
		}
	})

	t.Run("unscoped", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
			{columns: crossRepoDeadCodeEvidenceColumns()},
		})
		reader := NewContentReader(db)
		if _, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
			context.Background(),
			codeGrantGrantedRepo,
			[]string{"entity-1"},
			crossRepoDeadCodeConsumerReads{},
		); err != nil {
			t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
		}
		if len(recorder.queries) != 1 {
			t.Fatalf("query count = %d, want 1 -- an unscoped caller must not pay for the signal read", len(recorder.queries))
		}
		if strings.Contains(recorder.queries[0], "ANY(") {
			t.Fatalf("unscoped consumer-evidence SQL gained a grant clause:\n%s", recorder.queries[0])
		}
	})
}

func crossRepoDeadCodeEvidenceColumns() []string {
	return []string{
		"entity_id", "repository_id", "consumer_repo_name", "root_entity_id", "depth",
		"state", "confidence", "min_resolution_method", "evidence", "root_kinds",
		"generation_id", "generation_status", "observed_at", "updated_at",
	}
}
