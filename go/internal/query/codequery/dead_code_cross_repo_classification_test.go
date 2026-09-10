// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// What the two reads' answers mean once they are back: a granted consumer
// outranks a hidden one, the sentinel boundary does not cost a complete entity
// its evidence, and a truncated page outranks strong evidence.
//
// The statement-shape pins are in code_dead_code_cross_repo_bound_test.go and
// the probe's own behavioural guards in
// code_dead_code_cross_repo_probe_guards_test.go.

// TestCrossRepoDeadCodeStrongGrantedEvidenceOutranksHiddenConsumer pins the
// order this route shares with /dead-code and /dead-code/investigate: a
// consumer the caller may read settles the question, and one they may not read
// only decides it when nothing granted does.
//
// The route used to answer unknown_needs_evidence for a producer with both, so
// the same mixed shape read as "reachable" on one route and "cannot tell" on
// the other. It is one rule now. The hidden count stays on the row in every
// case, so a caller told the symbol is live is also told a consumer exists
// outside their grant.
func TestCrossRepoDeadCodeStrongGrantedEvidenceOutranksHiddenConsumer(t *testing.T) {
	t.Parallel()

	weakConsumerRow := func(entityID string) deadcode.CrossRepoDeadCodeEvidence {
		row := crossRepoDeadCodeGrantConsumerRow(codeGrantConsumerRepo, entityID)
		row.Confidence = codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName)
		row.ConfidenceLabel = deadcode.CrossRepoDeadCodeConfidenceLabel(row.Confidence)
		row.ResolutionMethod = codeprovenance.MethodRepoUniqueName
		return row
	}
	entity := func(entityID string) EntityContent {
		return EntityContent{
			EntityID:     entityID,
			RepoID:       "repo-producer",
			RelativePath: "pkg/payments/" + entityID + ".go",
			EntityType:   "Function",
			EntityName:   "maybeLive",
			Language:     "go",
			SourceCache:  "func maybeLive() {}",
		}
	}
	content := &crossRepoDeadCodeContentStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			FakePortContentStore: querytestutil.FakePortContentStore{
				Repositories: []querycontract.RepositoryCatalogEntry{{ID: "repo-producer", Name: "payments-lib"}},
			},
			entities: map[string]EntityContent{
				"producer-strong-plus-hidden": entity("producer-strong-plus-hidden"),
				"producer-hidden-only":        entity("producer-hidden-only"),
				"producer-weak-plus-hidden":   entity("producer-weak-plus-hidden"),
			},
		},
		rows: []map[string]any{
			deadCodeInvestigationRow("producer-strong-plus-hidden", "maybeLive", "go", "pkg/payments/strong.go", 8, 12),
			deadCodeInvestigationRow("producer-hidden-only", "maybeLive", "go", "pkg/payments/hidden.go", 8, 12),
			deadCodeInvestigationRow("producer-weak-plus-hidden", "maybeLive", "go", "pkg/payments/weak.go", 8, 12),
		},
		evidenceByEntity: map[string][]deadcode.CrossRepoDeadCodeEvidence{
			"producer-strong-plus-hidden": {
				crossRepoDeadCodeGrantConsumerRow(codeGrantConsumerRepo, "producer-strong-plus-hidden"),
			},
			"producer-weak-plus-hidden": {weakConsumerRow("producer-weak-plus-hidden")},
		},
		hiddenConsumers: []string{
			"producer-strong-plus-hidden",
			"producer-hidden-only",
			"producer-weak-plus-hidden",
		},
	}
	handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Content: content, Neo4j: fakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code/cross-repo",
		bytes.NewBufferString(`{"repo_id":"repo-producer","limit":10}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	buckets := decodeEnvelopeData(t, rec.Body.Bytes())["candidate_buckets"].(map[string]any)

	// Strong granted evidence beside a hidden consumer: live, and still counted.
	live := assertCrossRepoDeadCodeBucketEntity(t, buckets, "live_by_consumer", "producer-strong-plus-hidden")
	assertCrossRepoDeadCodeBucketMissing(t, buckets, "unknown", "producer-strong-plus-hidden")
	assertCrossRepoDeadCodeBucketMissing(t, buckets, "dead", "producer-strong-plus-hidden")
	if got, want := live["hidden_consumer_evidence_count"], float64(1); got != want {
		t.Fatalf("hidden_consumer_evidence_count = %#v, want %#v; a live answer must still say a consumer is hidden", got, want)
	}

	// Nothing granted proves use, so the hidden consumer decides the answer.
	for _, entityID := range []string{"producer-hidden-only", "producer-weak-plus-hidden"} {
		unknown := assertCrossRepoDeadCodeBucketEntity(t, buckets, "unknown", entityID)
		assertCrossRepoDeadCodeReason(t, unknown, "permission_hidden_consumer")
		assertCrossRepoDeadCodeBucketMissing(t, buckets, "live_by_consumer", entityID)
		assertCrossRepoDeadCodeBucketMissing(t, buckets, "dead", entityID)
		if got, want := unknown["hidden_consumer_evidence_count"], float64(1); got != want {
			t.Fatalf("%s hidden_consumer_evidence_count = %#v, want %#v", entityID, got, want)
		}
	}
}

// TestCrossRepoDeadCodeCompletesTheEntityTheSentinelMovedPast is the exact
// boundary case: the read returns 1,000 rows for one entity and the 1,001st row
// -- the sentinel, which is dropped -- belongs to the next entity.
//
// The statement orders by entity id, so a sentinel carrying a different id is
// proof the read moved past the first entity and returned every row it has.
// Marking that entity consumer_evidence_truncated turns valid strong evidence
// into unknown_needs_evidence for a page that was never short.
// TestHandleCrossRepoDeadCodeTruncatedSignalOutranksStrongEvidence is the same
// case at the route: a candidate carrying both a strong granted consumer and
// the truncation marker answers unknown_needs_evidence, never live_by_consumer.
func TestHandleCrossRepoDeadCodeTruncatedSignalOutranksStrongEvidence(t *testing.T) {
	t.Parallel()

	content := &crossRepoDeadCodeContentStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			FakePortContentStore: querytestutil.FakePortContentStore{
				Repositories: []querycontract.RepositoryCatalogEntry{{ID: "repo-producer", Name: "payments-lib"}},
			},
			entities: map[string]EntityContent{
				"producer-late": {
					EntityID:     "producer-late",
					RepoID:       "repo-producer",
					RelativePath: "pkg/payments/late.go",
					EntityType:   "Function",
					EntityName:   "maybeLive",
					Language:     "go",
					SourceCache:  "func maybeLive() {}",
				},
			},
		},
		rows: []map[string]any{
			deadCodeInvestigationRow("producer-late", "maybeLive", "go", "pkg/payments/late.go", 8, 12),
		},
		evidenceByEntity: map[string][]deadcode.CrossRepoDeadCodeEvidence{
			"producer-late": {
				crossRepoDeadCodeGrantConsumerRow(codeGrantConsumerRepo, "producer-late"),
				truncatedCrossRepoDeadCodeEvidence(),
			},
		},
	}
	handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Content: content, Neo4j: fakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code/cross-repo",
		bytes.NewBufferString(`{"repo_id":"repo-producer","limit":10}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	buckets := decodeEnvelopeData(t, rec.Body.Bytes())["candidate_buckets"].(map[string]any)
	unknown := assertCrossRepoDeadCodeBucketEntity(t, buckets, "unknown", "producer-late")
	assertCrossRepoDeadCodeReason(t, unknown, "consumer_evidence_truncated")
	assertCrossRepoDeadCodeBucketMissing(t, buckets, "live_by_consumer", "producer-late")
}
