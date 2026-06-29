// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func TestHandleCrossRepoDeadCodeFiltersProducerLocalLiveCandidates(t *testing.T) {
	t.Parallel()

	content := &crossRepoDeadCodeIncomingContentStore{
		crossRepoDeadCodeContentStore: &crossRepoDeadCodeContentStore{
			fakeDeadCodeContentStore: fakeDeadCodeContentStore{
				fakePortContentStore: fakePortContentStore{
					repositories: []RepositoryCatalogEntry{{ID: "repo-producer", Name: "payments-lib"}},
				},
				entities: map[string]EntityContent{
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
			evidenceByEntity: map[string][]crossRepoDeadCodeEvidence{},
		},
		incoming: map[string]deadCodeIncomingEdge{
			"producer-local-live": {
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
		bytes.NewBufferString(`{"repo_id":"repo-producer","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := decodeEnvelopeData(t, w.Body.Bytes())
	buckets := data["candidate_buckets"].(map[string]any)
	for _, bucket := range []string{"dead", "live_by_consumer", "unknown"} {
		assertCrossRepoDeadCodeBucketMissing(t, buckets, bucket, "producer-local-live")
	}
}

func TestHandleCrossRepoDeadCodeTruncatedEvidenceStaysUnknown(t *testing.T) {
	t.Parallel()

	content := &crossRepoDeadCodeContentStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			fakePortContentStore: fakePortContentStore{
				repositories: []RepositoryCatalogEntry{{ID: "repo-producer", Name: "payments-lib"}},
			},
			entities: map[string]EntityContent{
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
		evidenceByEntity: map[string][]crossRepoDeadCodeEvidence{
			"producer-missing-evidence": {truncatedCrossRepoDeadCodeEvidence()},
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
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := decodeEnvelopeData(t, w.Body.Bytes())
	buckets := data["candidate_buckets"].(map[string]any)
	unknown := assertCrossRepoDeadCodeBucketEntity(t, buckets, "unknown", "producer-missing-evidence")
	assertCrossRepoDeadCodeReason(t, unknown, "consumer_evidence_truncated")
	assertCrossRepoDeadCodeBucketMissing(t, buckets, "dead", "producer-missing-evidence")
}

func TestContentReaderCrossRepoDeadCodeEvidenceMarksMissingEntitiesUnknownWhenTruncated(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 29, 13, 0, 0, 0, time.UTC)
	rows := make([][]driver.Value, 0, maxCrossRepoDeadCodeConsumerEvidenceRows+1)
	for i := 0; i < maxCrossRepoDeadCodeConsumerEvidenceRows+1; i++ {
		rows = append(rows, []driver.Value{
			"producer-live", "repo-consumer", "checkout-api", "checkout-root",
			int64(2), "reachable", 0.9, codeprovenance.MethodImportBinding,
			[]byte(`["CALLS:checkout-root->producer-live"]`), []byte(`["go.main_function"]`),
			"gen-a", "active", observedAt, observedAt,
		})
	}
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{
			"entity_id", "repository_id", "consumer_repo_name", "root_entity_id",
			"depth", "state", "confidence", "min_resolution_method", "evidence",
			"root_kinds", "generation_id", "generation_status", "observed_at", "updated_at",
		},
		rows: rows,
	}})
	reader := NewContentReader(db)

	evidence, err := reader.CrossRepoDeadCodeConsumerEvidence(
		context.Background(),
		"repo-producer",
		[]string{"producer-live", "producer-missing"},
	)
	if err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
	}
	missing := evidence["producer-missing"]
	if got, want := len(missing), 1; got != want {
		t.Fatalf("len(evidence[producer-missing]) = %d, want %d", got, want)
	}
	if got, want := missing[0].Reason, "consumer_evidence_truncated"; got != want {
		t.Fatalf("truncation reason = %q, want %q", got, want)
	}
	if !missing[0].NeedsEvidence {
		t.Fatal("truncation evidence NeedsEvidence = false, want true")
	}
	if got, want := missing[0].Citation, "code_reachability_rows:truncated"; got != want {
		t.Fatalf("truncation citation = %q, want %q", got, want)
	}
	if got, want := len(evidence["producer-live"]), maxCrossRepoDeadCodeConsumerEvidenceRows; got != want {
		t.Fatalf("len(evidence[producer-live]) = %d, want %d", got, want)
	}
	if !containsAllSubstrings(recorder.queries[0], "LIMIT 1001") {
		t.Fatalf("query missing sentinel limit:\n%s", recorder.queries[0])
	}
}

type crossRepoDeadCodeIncomingContentStore struct {
	*crossRepoDeadCodeContentStore
	incoming map[string]deadCodeIncomingEdge
}

func (s *crossRepoDeadCodeIncomingContentStore) DeadCodeIncomingEntityIDs(
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

func truncatedCrossRepoDeadCodeEvidence() crossRepoDeadCodeEvidence {
	return crossRepoDeadCodeEvidence{
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
