// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// CrossRepoDeadCodeConsumerCoverage answers the coverage check from the test's
// configured gaps and records the request, so a test can assert both what the
// handler asked and what it did with the answer (#7547).
func (s *crossRepoDeadCodeContentStore) CrossRepoDeadCodeConsumerCoverage(
	_ context.Context,
	request code.CrossRepoDeadCodeCoverageRequest,
) (code.CrossRepoDeadCodeCoverage, error) {
	s.coverageRequests = append(s.coverageRequests, request)
	if s.coverageErr != nil {
		return code.CrossRepoDeadCodeCoverage{}, s.coverageErr
	}
	return code.CrossRepoDeadCodeCoverage{IncompleteRepositoryIDs: slices.Clone(s.coverageGaps)}, nil
}

// coverageStore builds a store with one never-called producer symbol, so the
// only thing standing between it and "dead" is the coverage answer.
func coverageStore(evidence []deadcode.CrossRepoDeadCodeEvidence) *crossRepoDeadCodeContentStore {
	return &crossRepoDeadCodeContentStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			FakePortContentStore: content.FakePortContentStore{
				Repositories: []querycontract.RepositoryCatalogEntry{
					{ID: "repo-producer", Name: "payments-lib"},
					{ID: "repo-consumer", Name: "checkout-api"},
					{ID: "repo-other", Name: "reports-api"},
				},
			},
			entities: map[string]deadcode.EntityContent{
				"producer-symbol": {
					EntityID:     "producer-symbol",
					RepoID:       "repo-producer",
					RelativePath: "pkg/payments/symbol.go",
					EntityType:   "Function",
					EntityName:   "charge",
					Language:     "go",
					StartLine:    8,
					EndLine:      12,
					SourceCache:  "func charge() {}",
				},
			},
		},
		rows: []map[string]any{
			deadCodeInvestigationRow("producer-symbol", "charge", "go", "pkg/payments/symbol.go", 8, 12),
		},
		evidenceByEntity: map[string][]deadcode.CrossRepoDeadCodeEvidence{"producer-symbol": evidence},
	}
}

func strongConsumerEvidence(consumerRepoID string) deadcode.CrossRepoDeadCodeEvidence {
	return deadcode.CrossRepoDeadCodeEvidence{
		ConsumerRepoID:   consumerRepoID,
		ConsumerRepoName: consumerRepoID,
		ConsumerEntityID: consumerRepoID + "#caller",
		RelationshipType: "CALLS",
		EvidenceFamily:   "direct_code",
		Citation:         "code_reachability_rows:scope-a/gen-a/" + consumerRepoID + "/root/producer-symbol",
		Confidence:       codeprovenance.Confidence(codeprovenance.MethodImportBinding),
		ConfidenceLabel:  "high",
		ResolutionMethod: codeprovenance.MethodImportBinding,
		Depth:            2,
		GenerationID:     "gen-a",
		GenerationStatus: "active",
	}
}

// postCoverageRequest posts one cross-repo dead-code request and returns the
// status plus the decoded envelope data (nil unless the status is 200).
func postCoverageRequest(
	t *testing.T,
	store deadcode.ContentStore,
	body string,
	authCtx *auth.AuthContext,
) (int, map[string]any) {
	t.Helper()

	handler := &codequery.CodeHandler{Profile: querycontract.ProfileLocalAuthoritative, Content: store, Neo4j: graph.FakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/dead-code/cross-repo", bytes.NewBufferString(body))
	if authCtx != nil {
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), *authCtx))
	}
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		return w.Code, nil
	}
	return w.Code, testutil.DecodeEnvelopeData(t, w.Body.Bytes())
}

// coverageObject returns the response's consumer_coverage summary.
func coverageObject(t *testing.T, data map[string]any) map[string]any {
	t.Helper()

	coverage, ok := data["consumer_coverage"].(map[string]any)
	if !ok {
		t.Fatalf("consumer_coverage = %#v, want an object naming the incomplete consumers", data["consumer_coverage"])
	}
	return coverage
}

// assertSymbolBucket requires the one producer symbol to sit in bucket and in
// no other.
func assertSymbolBucket(t *testing.T, data map[string]any, bucket string) map[string]any {
	t.Helper()

	buckets := data["candidate_buckets"].(map[string]any)
	row := assertCrossRepoDeadCodeBucketEntity(t, buckets, bucket, "producer-symbol")
	for _, other := range []string{"dead", "live_by_consumer", "unknown"} {
		if other == bucket {
			continue
		}
		if rows := buckets[other].([]any); len(rows) != 0 {
			t.Fatalf("%s bucket = %#v, want empty when the symbol is %s", other, rows, bucket)
		}
	}
	return row
}

// A named consumer whose active-generation snapshot has no complete watermark
// must not turn "no consumer row" into "dead": its snapshot may simply not
// contain the call (#7547). Before the reader consulted the watermark table
// this classified dead with medium confidence.
func TestCrossRepoDeadCodeIncompleteNamedConsumerCoverageIsNotDead(t *testing.T) {
	t.Parallel()

	store := coverageStore(nil)
	store.coverageGaps = []string{"repo-consumer"}
	status, data := postCoverageRequest(t, store,
		`{"repo_id":"repo-producer","consumer_repo_ids":["repo-consumer"],"limit":10}`, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	row := assertSymbolBucket(t, data, "unknown")
	assertCrossRepoDeadCodeReason(t, row, "consumer_coverage_incomplete")
	if got, want := row["classification"], "unknown_needs_evidence"; got != want {
		t.Fatalf("classification = %v, want %v", got, want)
	}
	coverage := coverageObject(t, data)
	if got, want := coverage["complete"], false; got != want {
		t.Fatalf("consumer_coverage.complete = %v, want %v", got, want)
	}
	assertQueryTestStringSliceEqual(t, coverage["incomplete_repo_ids"], []string{"repo-consumer"})
	if got, want := len(store.coverageRequests), 1; got != want {
		t.Fatalf("coverage requests = %d, want %d: one statement per request, not per candidate", got, want)
	}
	request := store.coverageRequests[0]
	if request.AllRepositories || !request.RequireActiveScope ||
		!slices.Equal(request.RepositoryIDs, []string{"repo-consumer"}) {
		t.Fatalf("coverage request = %#v, want the named consumer, active scope required", request)
	}
}

// The positive control: a complete watermark and no consumer evidence is still
// dead. The fix must not make every symbol unknown.
func TestCrossRepoDeadCodeCompleteConsumerCoverageStaysDead(t *testing.T) {
	t.Parallel()

	store := coverageStore(nil)
	status, data := postCoverageRequest(t, store,
		`{"repo_id":"repo-producer","consumer_repo_ids":["repo-consumer"],"limit":10}`, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	row := assertSymbolBucket(t, data, "dead")
	if got, want := row["confidence_label"], "medium"; got != want {
		t.Fatalf("confidence_label = %v, want %v", got, want)
	}
	coverage := coverageObject(t, data)
	if got, want := coverage["complete"], true; got != want {
		t.Fatalf("consumer_coverage.complete = %v, want %v", got, want)
	}
}

// Used stays used: strong evidence from a covered consumer proves the symbol
// live however another consumer's snapshot looks, the same order the hidden
// consumer count already follows.
func TestCrossRepoDeadCodeStrongLiveEvidenceOutranksIncompleteCoverage(t *testing.T) {
	t.Parallel()

	store := coverageStore([]deadcode.CrossRepoDeadCodeEvidence{strongConsumerEvidence("repo-consumer")})
	store.coverageGaps = []string{"repo-other"}
	status, data := postCoverageRequest(t, store,
		`{"repo_id":"repo-producer","consumer_repo_ids":["repo-consumer","repo-other"],"limit":10}`, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	assertSymbolBucket(t, data, "live_by_consumer")
	coverage := coverageObject(t, data)
	if got, want := coverage["complete"], false; got != want {
		t.Fatalf("consumer_coverage.complete = %v, want %v: a live answer must still say coverage is partial", got, want)
	}
}

// An unscoped request names no consumer, so the check covers every repository
// with an active generation.
func TestCrossRepoDeadCodeUnscopedRequestChecksEveryRepository(t *testing.T) {
	t.Parallel()

	store := coverageStore(nil)
	store.coverageGaps = []string{"repo-other"}
	status, data := postCoverageRequest(t, store, `{"repo_id":"repo-producer","limit":10}`, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	row := assertSymbolBucket(t, data, "unknown")
	assertCrossRepoDeadCodeReason(t, row, "consumer_coverage_incomplete")
	if got, want := len(store.coverageRequests), 1; got != want {
		t.Fatalf("coverage requests = %d, want %d", got, want)
	}
	request := store.coverageRequests[0]
	if !request.AllRepositories || len(request.RepositoryIDs) != 0 || request.RequireActiveScope {
		t.Fatalf("coverage request = %#v, want every repository", request)
	}
}

// A scoped caller who named no consumer is checked over their grant only, and
// a granted repository nobody ingested is not a coverage gap.
func TestCrossRepoDeadCodeGrantBoundRequestChecksTheGrant(t *testing.T) {
	t.Parallel()

	store := coverageStore(nil)
	authCtx := &auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-producer", "repo-consumer"},
	}
	status, data := postCoverageRequest(t, store, `{"repo_id":"repo-producer","limit":10}`, authCtx)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	assertSymbolBucket(t, data, "dead")
	if got, want := len(store.coverageRequests), 1; got != want {
		t.Fatalf("coverage requests = %d, want %d", got, want)
	}
	request := store.coverageRequests[0]
	if request.AllRepositories || request.RequireActiveScope || len(request.RepositoryIDs) == 0 {
		t.Fatalf("coverage request = %#v, want the grant list without requiring an active scope", request)
	}
	for _, id := range request.RepositoryIDs {
		if id != "repo-producer" && id != "repo-consumer" {
			t.Fatalf("coverage request = %#v, names a repository outside the grant", request)
		}
	}
}

// A coverage read that fails is a failed request, never a silent "covered".
func TestCrossRepoDeadCodeCoverageErrorFailsTheRequest(t *testing.T) {
	t.Parallel()

	store := coverageStore(nil)
	store.coverageErr = context.DeadlineExceeded
	status, _ := postCoverageRequest(t, store,
		`{"repo_id":"repo-producer","consumer_repo_ids":["repo-consumer"],"limit":10}`, nil)
	if status == http.StatusOK {
		t.Fatalf("status = %d, want a store error", status)
	}
}

// A content store that cannot answer the coverage question must not be read as
// "covered".
func TestCrossRepoDeadCodeStoreWithoutCoverageIsUnavailableNotDead(t *testing.T) {
	t.Parallel()

	inner := coverageStore(nil)
	store := coverageBlindStore{ContentStore: inner, inner: inner}
	status, data := postCoverageRequest(t, store,
		`{"repo_id":"repo-producer","consumer_repo_ids":["repo-consumer"],"limit":10}`, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	row := assertSymbolBucket(t, data, "unknown")
	assertCrossRepoDeadCodeReason(t, row, "cross_repo_evidence_unavailable")
}

// With nothing to classify there is nothing to cover, so the request pays for
// no coverage statement.
func TestCrossRepoDeadCodeNoCandidatesSkipsTheCoverageCheck(t *testing.T) {
	t.Parallel()

	store := coverageStore(nil)
	store.rows = nil
	status, _ := postCoverageRequest(t, store,
		`{"repo_id":"repo-producer","consumer_repo_ids":["repo-consumer"],"limit":10}`, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := len(store.coverageRequests); got != 0 {
		t.Fatalf("coverage requests = %d, want 0 when no candidate needs classifying", got)
	}
}

// coverageBlindStore exposes the evidence read but not the coverage read.
type coverageBlindStore struct {
	deadcode.ContentStore
	inner *crossRepoDeadCodeContentStore
}

func (s coverageBlindStore) CrossRepoDeadCodeConsumerEvidence(
	ctx context.Context,
	producerRepoID string,
	entityIDs []string,
	reads code.CrossRepoDeadCodeConsumerReads,
) (map[string][]deadcode.CrossRepoDeadCodeEvidence, code.CrossRepoDeadCodeHiddenConsumers, error) {
	return s.inner.CrossRepoDeadCodeConsumerEvidence(ctx, producerRepoID, entityIDs, reads)
}

func (s coverageBlindStore) DeadCodeCandidateRows(
	ctx context.Context,
	query codeshaping.DeadCodeCandidateQuery,
) ([]map[string]any, error) {
	return s.inner.DeadCodeCandidateRows(ctx, query)
}
