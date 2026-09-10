// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestHandleCrossRepoDeadCodeFiltersProducerLocalLiveCandidates(t *testing.T) {
	t.Parallel()

	content := &crossRepoDeadCodeIncomingContentStore{
		crossRepoDeadCodeContentStore: &crossRepoDeadCodeContentStore{
			fakeDeadCodeContentStore: fakeDeadCodeContentStore{
				FakePortContentStore: querytestutil.FakePortContentStore{
					Repositories: []querycontract.RepositoryCatalogEntry{{ID: "repo-producer", Name: "payments-lib"}},
				},
				entities: map[string]deadcode.EntityContent{
					"producer-local-live": {
						EntityID:     "producer-local-live",
						RepoID:       "repo-producer",
						RelativePath: "pkg/payments/local_live.go",
						EntityType:   "Function",
						EntityName:   "helper",
						Language:     "go",
						SourceCache:  "func helper() {}",
					},
				},
			},
			rows: []map[string]any{
				deadCodeInvestigationRow(
					"producer-local-live",
					"helper",
					"go",
					"pkg/payments/local_live.go",
					8,
					12,
				),
			},
			evidenceByEntity: map[string][]deadcode.CrossRepoDeadCodeEvidence{},
		},
		incoming: map[string]deadcode.DeadCodeIncomingEdge{
			"producer-local-live": {
				MaxConfidence: codeprovenance.Confidence(codeprovenance.MethodSCIP),
				Method:        codeprovenance.MethodSCIP,
			},
		},
	}
	handler := &codequery.CodeHandler{Profile: querycontract.ProfileLocalAuthoritative, Content: content, Neo4j: querytestutil.FakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code/cross-repo",
		bytes.NewBufferString(`{"repo_id":"repo-producer","limit":10}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := querytestutil.DecodeEnvelopeData(t, w.Body.Bytes())
	buckets := data["candidate_buckets"].(map[string]any)
	for _, bucket := range []string{"dead", "live_by_consumer", "unknown"} {
		assertCrossRepoDeadCodeBucketMissing(t, buckets, bucket, "producer-local-live")
	}
}

func TestHandleCrossRepoDeadCodeTruncatedEvidenceStaysUnknown(t *testing.T) {
	t.Parallel()

	content := &crossRepoDeadCodeContentStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			FakePortContentStore: querytestutil.FakePortContentStore{
				Repositories: []querycontract.RepositoryCatalogEntry{{ID: "repo-producer", Name: "payments-lib"}},
			},
			entities: map[string]deadcode.EntityContent{
				"producer-missing-evidence": {
					EntityID:     "producer-missing-evidence",
					RepoID:       "repo-producer",
					RelativePath: "pkg/payments/missing.go",
					EntityType:   "Function",
					EntityName:   "maybeLive",
					Language:     "go",
					SourceCache:  "func maybeLive() {}",
				},
			},
		},
		rows: []map[string]any{
			deadCodeInvestigationRow("producer-missing-evidence", "maybeLive", "go", "pkg/payments/missing.go", 8, 12),
		},
		evidenceByEntity: map[string][]deadcode.CrossRepoDeadCodeEvidence{
			"producer-missing-evidence": {truncatedCrossRepoDeadCodeEvidence()},
		},
	}
	handler := &codequery.CodeHandler{Profile: querycontract.ProfileLocalAuthoritative, Content: content, Neo4j: querytestutil.FakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code/cross-repo",
		bytes.NewBufferString(`{"repo_id":"repo-producer","limit":10}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := querytestutil.DecodeEnvelopeData(t, w.Body.Bytes())
	buckets := data["candidate_buckets"].(map[string]any)
	unknown := assertCrossRepoDeadCodeBucketEntity(t, buckets, "unknown", "producer-missing-evidence")
	assertCrossRepoDeadCodeReason(t, unknown, "consumer_evidence_truncated")
	assertCrossRepoDeadCodeBucketMissing(t, buckets, "dead", "producer-missing-evidence")
}

type crossRepoDeadCodeIncomingContentStore struct {
	*crossRepoDeadCodeContentStore
	incoming map[string]deadcode.DeadCodeIncomingEdge
}

func (s *crossRepoDeadCodeIncomingContentStore) DeadCodeIncomingEntityIDs(
	_ context.Context,
	_ string,
	entityIDs []string,
) (map[string]deadcode.DeadCodeIncomingEdge, error) {
	result := make(map[string]deadcode.DeadCodeIncomingEdge)
	for _, entityID := range entityIDs {
		if edge, ok := s.incoming[entityID]; ok {
			result[entityID] = edge
		}
	}
	return result, nil
}

func assertCrossRepoDeadCodeBucketMissing(
	t *testing.T,
	buckets map[string]any,
	name string,
	entityID string,
) {
	t.Helper()

	rawRows, ok := buckets[name].([]any)
	if !ok {
		t.Fatalf("candidate_buckets[%s] type = %T, want []any", name, buckets[name])
	}
	for _, raw := range rawRows {
		row := raw.(map[string]any)
		if row["entity_id"] == entityID {
			t.Fatalf("candidate_buckets[%s] unexpectedly contains entity %q: %#v", name, entityID, row)
		}
	}
}

func truncatedCrossRepoDeadCodeEvidence() deadcode.CrossRepoDeadCodeEvidence {
	return deadcode.CrossRepoDeadCodeEvidence{
		EvidenceFamily:   "code_reachability",
		Citation:         "code_reachability_rows:truncated",
		ConfidenceLabel:  "unknown",
		GenerationStatus: "active",
		NeedsEvidence:    true,
		Reason:           "consumer_evidence_truncated",
		RelationshipType: "REACHES",
		ResolutionMethod: "bounded_lookup",
	}
}
