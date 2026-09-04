// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// #5167 code family: /dead-code and /dead-code/investigate answer "is anything
// still calling this symbol", and the reads that answer it look outside the
// caller's grant.
//
// CodeReachabilityIncomingEntityIDs is deliberately not repo-scoped -- a
// library symbol is kept alive by the service repositories that call it -- and
// the graph probe matches an incoming edge from any source at all. For a scoped
// caller that turns another tenant's call into cleanup truth twice over: the
// candidate silently disappears from the answer, which is itself a readable
// signal that a hidden consumer exists, and the caller is told a symbol is
// reachable on evidence they may not see.
//
// The rule these tests hold to is the one the cross-repo route already follows:
// an edge from outside the grant is neither live nor dead. It resolves to
// unknown, carrying the permission_hidden_consumer reason, and no identifier
// from the ungranted side reaches the response.

// deadCodeHiddenConsumerStore serves one candidate in the granted repository
// whose only incoming edge comes from a repository the caller was not granted.
// CodeReachabilityIncomingEntityIDs mirrors the shipped SQL: the consumer grant
// decides whether the edge comes back as evidence or as a hidden marker, and an
// empty grant list (the unscoped caller) restricts nothing.
type deadCodeHiddenConsumerStore struct {
	fakeDeadCodeContentStore
	consumerRepoByEntity map[string]string
	boundConsumerGrant   []string
	reachabilityCalls    int
}

func (s *deadCodeHiddenConsumerStore) DeadCodeCandidateRows(
	_ context.Context,
	query deadCodeCandidateQuery,
) ([]map[string]any, error) {
	if query.Label != "Function" || query.Offset > 0 {
		return nil, nil
	}
	if !codeContentGrantAdmits(codeGrantGrantedRepo, query.RepoID, query.AllowedRepositoryIDs) {
		return nil, nil
	}
	return []map[string]any{{
		"entity_id":  deadCodeHiddenConsumerEntityID,
		"name":       "unusedHelper",
		"labels":     []any{"Function"},
		"file_path":  "internal/legacy/helper.go",
		"repo_id":    codeGrantGrantedRepo,
		"repo_name":  codeGrantGrantedRepo,
		"language":   "go",
		"start_line": 4,
		"end_line":   9,
	}}, nil
}

func (s *deadCodeHiddenConsumerStore) CodeReachabilityIncomingEntityIDs(
	_ context.Context,
	_ string,
	entityIDs []string,
	allowedRepositoryIDs []string,
) (map[string]deadCodeIncomingEdge, error) {
	s.reachabilityCalls++
	s.boundConsumerGrant = append([]string(nil), allowedRepositoryIDs...)
	incoming := make(map[string]deadCodeIncomingEdge, len(entityIDs))
	for _, entityID := range entityIDs {
		consumerRepoID, ok := s.consumerRepoByEntity[entityID]
		if !ok {
			continue
		}
		if len(allowedRepositoryIDs) == 0 || slices.Contains(allowedRepositoryIDs, consumerRepoID) {
			incoming[entityID] = deadCodeIncomingEdge{
				MaxConfidence: codeprovenance.Confidence(codeprovenance.MethodImportBinding),
				Method:        codeprovenance.MethodImportBinding,
			}
			continue
		}
		incoming[entityID] = deadCodeIncomingEdge{HiddenConsumer: true}
	}
	return incoming, nil
}

const deadCodeHiddenConsumerEntityID = "repo://tenant-a/granted-service#unusedHelper"

func newDeadCodeHiddenConsumerStore() *deadCodeHiddenConsumerStore {
	return &deadCodeHiddenConsumerStore{
		consumerRepoByEntity: map[string]string{deadCodeHiddenConsumerEntityID: codeGrantOtherRepo},
	}
}

func runDeadCodeHiddenConsumerRoute(
	t *testing.T,
	store ContentStore,
	path string,
	auth *AuthContext,
) *httptest.ResponseRecorder {
	t.Helper()

	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := newCodeGrantRouteRequest(t, path, map[string]any{"language": "go"}, auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestDeadCodeKeepsACandidateWhoseOnlyConsumerIsOutsideTheGrant(t *testing.T) {
	t.Parallel()

	store := newDeadCodeHiddenConsumerStore()
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runDeadCodeHiddenConsumerRoute(t, store, "/api/v0/code/dead-code", &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got := store.boundConsumerGrant; !slices.Contains(got, codeGrantGrantedRepo) {
		t.Fatalf("consumer grant = %#v, want the caller's grant bound into the incoming-edge read", got)
	}
	data := decodeEnvelopeData(t, rec.Body.Bytes())
	results, _ := data["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results = %#v, want the candidate kept: an edge the caller cannot see is neither live nor dead; body = %s", results, rec.Body.String())
	}
	result, _ := results[0].(map[string]any)
	if got, want := result["classification"], deadCodeClassificationAmbiguous; got != want {
		t.Fatalf("classification = %v, want %q for a candidate whose only consumer is hidden", got, want)
	}
	if hidden, _ := result[deadCodeHiddenConsumerResultKey].(bool); !hidden {
		t.Fatalf("%s missing from the kept candidate: %s", deadCodeHiddenConsumerResultKey, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), codeGrantOtherRepo) {
		t.Fatalf("response named the ungranted consumer repository %q: %s", codeGrantOtherRepo, rec.Body.String())
	}
}

func TestDeadCodeInvestigateReportsThePermissionHiddenConsumerReason(t *testing.T) {
	t.Parallel()

	store := newDeadCodeHiddenConsumerStore()
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runDeadCodeHiddenConsumerRoute(t, store, "/api/v0/code/dead-code/investigate", &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	data := decodeEnvelopeData(t, rec.Body.Bytes())
	buckets, _ := data["candidate_buckets"].(map[string]any)
	cleanupReady, _ := buckets["cleanup_ready"].([]any)
	if len(cleanupReady) != 0 {
		t.Fatalf("cleanup_ready = %#v, want nothing: a hidden consumer is not proof the symbol is dead", cleanupReady)
	}
	ambiguous, _ := buckets["ambiguous"].([]any)
	if len(ambiguous) != 1 {
		t.Fatalf("ambiguous = %#v, want the candidate answered unknown; body = %s", ambiguous, rec.Body.String())
	}
	candidate, _ := ambiguous[0].(map[string]any)
	reasons, _ := candidate["ambiguity_reasons"].([]any)
	if !slices.Contains(reasons, any(deadCodeHiddenConsumerReason)) {
		t.Fatalf("ambiguity_reasons = %#v, want %q", reasons, deadCodeHiddenConsumerReason)
	}
	if strings.Contains(rec.Body.String(), codeGrantOtherRepo) {
		t.Fatalf("response named the ungranted consumer repository %q: %s", codeGrantOtherRepo, rec.Body.String())
	}
}

// TestDeadCodeSharedKeyIncomingProbeIsUnchanged is the other direction: the
// unscoped caller sees the same answer as before, with the candidate filtered
// out by the strong incoming edge and no grant bound into the read.
func TestDeadCodeSharedKeyIncomingProbeIsUnchanged(t *testing.T) {
	t.Parallel()

	store := newDeadCodeHiddenConsumerStore()
	rec := runDeadCodeHiddenConsumerRoute(t, store, "/api/v0/code/dead-code", nil)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got := store.boundConsumerGrant; len(got) != 0 {
		t.Fatalf("consumer grant = %#v, want nothing bound for a shared-key caller", got)
	}
	data := decodeEnvelopeData(t, rec.Body.Bytes())
	if results, _ := data["results"].([]any); len(results) != 0 {
		t.Fatalf("results = %#v, want the candidate filtered out by its strong incoming edge", results)
	}
}

func TestCodeReachabilityIncomingEntityIDsBindsTheConsumerGrant(t *testing.T) {
	t.Parallel()

	t.Run("scoped", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
			{
				columns: []string{"entity_id", "min_resolution_method", "consumer_in_grant"},
				rows: [][]driver.Value{
					{"content-entity:library-symbol", codeprovenance.MethodImportBinding, false},
				},
			},
		})
		reader := NewContentReader(db)
		incoming, err := reader.CodeReachabilityIncomingEntityIDs(
			context.Background(),
			"repository:library",
			[]string{"content-entity:library-symbol"},
			[]string{codeGrantGrantedRepo},
		)
		if err != nil {
			t.Fatalf("CodeReachabilityIncomingEntityIDs() error = %v, want nil", err)
		}
		edge := incoming["content-entity:library-symbol"]
		if !edge.HiddenConsumer {
			t.Fatalf("edge = %#v, want the out-of-grant consumer reported as hidden", edge)
		}
		if edge.MaxConfidence != 0 {
			t.Fatalf("MaxConfidence = %v, want 0: an edge the caller cannot see is not evidence", edge.MaxConfidence)
		}
		want := "(row.repository_id = ANY($2)) AS consumer_in_grant"
		if !strings.Contains(recorder.queries[0], want) {
			t.Fatalf("reachability SQL is missing %q, so an ungranted consumer still reads as evidence:\n%s", want, recorder.queries[0])
		}
	})

	t.Run("unscoped", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
			{
				columns: []string{"entity_id", "min_resolution_method"},
				rows: [][]driver.Value{
					{"content-entity:library-symbol", codeprovenance.MethodImportBinding},
				},
			},
		})
		reader := NewContentReader(db)
		if _, err := reader.CodeReachabilityIncomingEntityIDs(
			context.Background(),
			"repository:library",
			[]string{"content-entity:library-symbol"},
			nil,
		); err != nil {
			t.Fatalf("CodeReachabilityIncomingEntityIDs() error = %v, want nil", err)
		}
		if strings.Contains(recorder.queries[0], "consumer_in_grant") {
			t.Fatalf("unscoped reachability SQL gained a grant column:\n%s", recorder.queries[0])
		}
	})
}

// TestDeadCodeGraphIncomingProbeIsGrantBound pins the graph half's statement.
// A scoped caller runs one probe that expands the candidate's incoming edges
// once and projects the grant per row, so an ungranted source is its own row
// rather than a row missing from a second read. An unscoped caller runs the
// unrestricted probe, and its text is unchanged.
func TestDeadCodeGraphIncomingProbeIsGrantBound(t *testing.T) {
	t.Parallel()

	access := repositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}}
	scoped := buildDeadCodeScopedIncomingBatchProbeCypher("Function", access)
	for _, want := range []string{
		"OPTIONAL MATCH (source)<-[:CONTAINS]-(:File)<-[:REPO_CONTAINS]-(source_repo:Repository)",
		"(source_repo IS NOT NULL AND " + access.GraphCondition("source_repo") + ") as in_grant",
		"count(*) as edge_count",
	} {
		if !strings.Contains(scoped, want) {
			t.Fatalf("scoped incoming probe is missing %q:\n%s", want, scoped)
		}
	}
	// RETURN DISTINCT after this OPTIONAL MATCH is not parsed by the pinned
	// NornicDB: the keyword is absorbed into the first projection's source text
	// and nothing is deduplicated. count(*) is what groups the rows instead.
	if strings.Contains(scoped, "RETURN DISTINCT") {
		t.Fatalf("the scoped probe must group with count(*), not RETURN DISTINCT:\n%s", scoped)
	}
	if strings.Count(scoped, "MATCH") != 2 {
		t.Fatalf("the scoped probe must expand incoming edges exactly once:\n%s", scoped)
	}
	if unscoped := buildDeadCodeScopedIncomingBatchProbeCypher("Function", repositoryAccessFilter{AllScopes: true}); unscoped != buildDeadCodeIncomingBatchProbeCypher("Function") {
		t.Fatalf("an unscoped caller must run the unchanged probe text:\n%s", unscoped)
	}
}

// TestDeadCodeGraphProbeTreatsAnUngrantedSourceAsUnknown runs the probe against
// a graph that answers it as the backend would: the candidate's one incoming
// edge comes from a repository outside the grant, so its row projects
// in_grant=false and becomes the hidden-consumer marker rather than evidence.
func TestDeadCodeGraphProbeTreatsAnUngrantedSourceAsUnknown(t *testing.T) {
	t.Parallel()

	var statements []string
	// The probe traverses incoming edges, which the shared fake routes to
	// runIncoming; run is set to the same answer so a probe that stopped
	// traversing incoming edges would still be seen here.
	probe := func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		statements = append(statements, cypher)
		return []map[string]any{{
			"incoming_entity_id": deadCodeHiddenConsumerEntityID,
			"resolution_method":  codeprovenance.MethodImportBinding,
			"in_grant":           false,
			"edge_count":         1,
		}}, nil
	}
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j:   fakeGraphReader{run: probe, runIncoming: probe},
	}
	results := []map[string]any{{
		"entity_id": deadCodeHiddenConsumerEntityID,
		"repo_id":   codeGrantGrantedRepo,
		"language":  "go",
		"name":      "unusedHelper",
		"labels":    []any{"Function"},
	}}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	ctx := ContextWithAuthContext(context.Background(), auth)
	incoming, err := handler.deadCodeResultsWithGraphIncomingEdges(ctx, results, "Function")
	if err != nil {
		t.Fatalf("deadCodeResultsWithGraphIncomingEdges() error = %v, want nil", err)
	}
	if len(statements) != 1 {
		t.Fatalf("statement count = %d, want 1 (one expansion with the grant projected per row)", len(statements))
	}
	edge := incoming[deadCodeHiddenConsumerEntityID]
	if !edge.HiddenConsumer {
		t.Fatalf("edge = %#v, want the ungranted source reported as hidden", edge)
	}
	if edge.MaxConfidence != 0 {
		t.Fatalf("MaxConfidence = %v, want 0 for an edge the caller cannot see", edge.MaxConfidence)
	}
}

// TestDeadCodeWeakGrantedEdgeBesideAnUngrantedOneReadsHiddenOnBothBackends
// covers the candidate the caller can see one weak edge into and cannot see
// another. The SQL half reports permission_hidden_consumer for it, because the
// grant is decided per row. The graph half diffs two probes, so it has to diff
// them per edge as well: diffing whole entities lets the granted edge hide the
// ungranted one, and the same candidate then reads as a weak-evidence review
// item on one backend and a permission question on the other.
func TestDeadCodeWeakGrantedEdgeBesideAnUngrantedOneReadsHiddenOnBothBackends(t *testing.T) {
	t.Parallel()

	for _, backend := range []struct {
		name     string
		incoming func(*testing.T) (content, graph map[string]deadCodeIncomingEdge)
	}{
		{name: "sql", incoming: deadCodeWeakGrantedPlusUngrantedFromSQL},
		{name: "graph", incoming: deadCodeWeakGrantedPlusUngrantedFromGraph},
	} {
		t.Run(backend.name, func(t *testing.T) {
			t.Parallel()

			content, graph := backend.incoming(t)
			results := []map[string]any{{
				"entity_id": deadCodeHiddenConsumerEntityID,
				"repo_id":   codeGrantGrantedRepo,
				"language":  "go",
				"name":      "unusedHelper",
			}}
			kept := applyDeadCodeIncomingEdges(results, content, graph)
			if len(kept) != 1 {
				t.Fatalf("kept = %#v, want the candidate kept: a weak edge is not proof it is reachable", kept)
			}
			if got, want := kept[0]["classification"], deadCodeClassificationAmbiguous; got != want {
				t.Fatalf("classification = %v, want %q", got, want)
			}
			reasons := deadCodeInvestigationAmbiguityReasons(kept[0])
			if !slices.Contains(reasons, deadCodeHiddenConsumerReason) {
				t.Fatalf("ambiguity_reasons = %#v, want %q: an edge the caller cannot see decides the answer even when a weak one beside it can be seen", reasons, deadCodeHiddenConsumerReason)
			}
		})
	}
}

// deadCodeWeakGrantedPlusUngrantedFromSQL runs the shipped reachability read
// over two materialized rows for one entity: a weak consumer inside the grant
// and a stronger one outside it.
func deadCodeWeakGrantedPlusUngrantedFromSQL(t *testing.T) (map[string]deadCodeIncomingEdge, map[string]deadCodeIncomingEdge) {
	t.Helper()

	db, _ := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{"entity_id", "min_resolution_method", "consumer_in_grant"},
		rows: [][]driver.Value{
			{deadCodeHiddenConsumerEntityID, codeprovenance.MethodRepoUniqueName, true},
			{deadCodeHiddenConsumerEntityID, codeprovenance.MethodImportBinding, false},
		},
	}})
	incoming, err := NewContentReader(db).CodeReachabilityIncomingEntityIDs(
		context.Background(),
		codeGrantGrantedRepo,
		[]string{deadCodeHiddenConsumerEntityID},
		[]string{codeGrantGrantedRepo},
	)
	if err != nil {
		t.Fatalf("CodeReachabilityIncomingEntityIDs() error = %v, want nil", err)
	}
	return incoming, nil
}

// deadCodeWeakGrantedPlusUngrantedFromGraph runs the shipped graph probe over
// the same shape: one weak edge from inside the grant and one stronger edge
// from outside it, each its own row with its own in_grant answer.
func deadCodeWeakGrantedPlusUngrantedFromGraph(t *testing.T) (map[string]deadCodeIncomingEdge, map[string]deadCodeIncomingEdge) {
	t.Helper()

	probe := func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		return []map[string]any{{
			"incoming_entity_id": deadCodeHiddenConsumerEntityID,
			"resolution_method":  codeprovenance.MethodRepoUniqueName,
			"in_grant":           true,
			"edge_count":         1,
		}, {
			"incoming_entity_id": deadCodeHiddenConsumerEntityID,
			"resolution_method":  codeprovenance.MethodImportBinding,
			"in_grant":           false,
			"edge_count":         1,
		}}, nil
	}
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j:   fakeGraphReader{run: probe, runIncoming: probe},
	}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	ctx := ContextWithAuthContext(context.Background(), auth)
	graph, err := handler.deadCodeResultsWithGraphIncomingEdges(ctx, []map[string]any{{
		"entity_id": deadCodeHiddenConsumerEntityID,
		"repo_id":   codeGrantGrantedRepo,
		"language":  "go",
		"labels":    []any{"Function"},
	}}, "Function")
	if err != nil {
		t.Fatalf("deadCodeResultsWithGraphIncomingEdges() error = %v, want nil", err)
	}
	return nil, graph
}
