// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func TestHandleCrossRepoDeadCodeKeepsMaterializedConsumerEvidence(t *testing.T) {
	t.Parallel()

	content := &crossRepoDeadCodeMaterializedContentStore{
		crossRepoDeadCodeContentStore: &crossRepoDeadCodeContentStore{
			fakeDeadCodeContentStore: fakeDeadCodeContentStore{
				fakePortContentStore: fakePortContentStore{
					repositories: []RepositoryCatalogEntry{
						{ID: "repo-producer", Name: "payments-lib"},
						{ID: "repo-consumer", Name: "checkout-api"},
					},
				},
				entities: map[string]EntityContent{
					"producer-live": {
						EntityID:     "producer-live",
						RepoID:       "repo-producer",
						RelativePath: "pkg/payments/live.go",
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
				deadCodeInvestigationRow("producer-live", "charge", "go", "pkg/payments/live.go", 8, 12),
			},
			evidenceByEntity: map[string][]crossRepoDeadCodeEvidence{
				"producer-live": {{
					ConsumerRepoID:   "repo-consumer",
					ConsumerRepoName: "checkout-api",
					ConsumerEntityID: "checkout-call",
					RelationshipType: "CALLS",
					EvidenceFamily:   "direct_code",
					Citation:         "code_reachability_rows:scope-a/gen-a/repo-consumer/root-checkout/producer-live",
					Confidence:       codeprovenance.Confidence(codeprovenance.MethodSCIP),
					ConfidenceLabel:  "high",
					ResolutionMethod: codeprovenance.MethodSCIP,
					Depth:            2,
					GenerationID:     "gen-a",
					GenerationStatus: "active",
					ObservedAt:       time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC),
				}},
			},
		},
		incoming: map[string]deadCodeIncomingEdge{
			"producer-live": {
				MaxConfidence: codeprovenance.Confidence(codeprovenance.MethodSCIP),
				Method:        codeprovenance.MethodSCIP,
			},
		},
	}
	handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Content: content, Neo4j: fakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code/cross-repo",
		bytes.NewBufferString(`{"repo_id":"payments-lib","consumer_repo_ids":["checkout-api"],"limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := decodeEnvelopeData(t, w.Body.Bytes())
	buckets := data["candidate_buckets"].(map[string]any)
	live := assertCrossRepoDeadCodeBucketEntity(t, buckets, "live_by_consumer", "producer-live")
	assertCrossRepoDeadCodeEvidenceCitation(
		t,
		live,
		"code_reachability_rows:scope-a/gen-a/repo-consumer/root-checkout/producer-live",
	)
}

type crossRepoDeadCodeMaterializedContentStore struct {
	*crossRepoDeadCodeContentStore
	incoming map[string]deadCodeIncomingEdge
}

func (s *crossRepoDeadCodeMaterializedContentStore) CodeReachabilityIncomingEntityIDs(
	_ context.Context,
	_ string,
	entityIDs []string,
) (map[string]deadCodeIncomingEdge, error) {
	result := make(map[string]deadCodeIncomingEdge)
	for _, entityID := range entityIDs {
		if edge, ok := s.incoming[entityID]; ok {
			result[entityID] = edge
		}
	}
	return result, nil
}
