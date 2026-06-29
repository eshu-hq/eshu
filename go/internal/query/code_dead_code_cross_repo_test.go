// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func TestHandleCrossRepoDeadCodeClassifiesConsumerEvidence(t *testing.T) {
	t.Parallel()

	content := &crossRepoDeadCodeContentStore{
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
				"producer-dead": {
					EntityID:     "producer-dead",
					RepoID:       "repo-producer",
					RelativePath: "pkg/payments/dead.go",
					EntityType:   "Function",
					EntityName:   "unused",
					Language:     "go",
					StartLine:    20,
					EndLine:      21,
					SourceCache:  "func unused() {}",
				},
				"producer-ambiguous": {
					EntityID:     "producer-ambiguous",
					RepoID:       "repo-producer",
					RelativePath: "pkg/payments/maybe.go",
					EntityType:   "Function",
					EntityName:   "maybe",
					Language:     "go",
					StartLine:    30,
					EndLine:      33,
					SourceCache:  "func maybe() {}",
				},
				"producer-stale": {
					EntityID:     "producer-stale",
					RepoID:       "repo-producer",
					RelativePath: "pkg/payments/stale.go",
					EntityType:   "Function",
					EntityName:   "stale",
					Language:     "go",
					StartLine:    40,
					EndLine:      41,
					SourceCache:  "func stale() {}",
				},
				"producer-cycle": {
					EntityID:     "producer-cycle",
					RepoID:       "repo-producer",
					RelativePath: "pkg/payments/cycle.go",
					EntityType:   "Function",
					EntityName:   "cycleSafe",
					Language:     "go",
					StartLine:    50,
					EndLine:      53,
					SourceCache:  "func cycleSafe() {}",
				},
			},
		},
		rows: []map[string]any{
			deadCodeInvestigationRow("producer-live", "charge", "go", "pkg/payments/live.go", 8, 12),
			deadCodeInvestigationRow("producer-dead", "unused", "go", "pkg/payments/dead.go", 20, 21),
			deadCodeInvestigationRow("producer-ambiguous", "maybe", "go", "pkg/payments/maybe.go", 30, 33),
			deadCodeInvestigationRow("producer-stale", "stale", "go", "pkg/payments/stale.go", 40, 41),
			deadCodeInvestigationRow("producer-cycle", "cycleSafe", "go", "pkg/payments/cycle.go", 50, 53),
		},
		evidenceByEntity: map[string][]crossRepoDeadCodeEvidence{
			"producer-live": {{
				ConsumerRepoID:   "repo-consumer",
				ConsumerRepoName: "checkout-api",
				ConsumerEntityID: "checkout-call",
				RelationshipType: "CALLS",
				EvidenceFamily:   "direct_code",
				Citation:         "code_reachability_rows:scope-a/gen-a/repo-consumer/root-checkout/producer-live",
				Confidence:       codeprovenance.Confidence(codeprovenance.MethodImportBinding),
				ConfidenceLabel:  "high",
				ResolutionMethod: codeprovenance.MethodImportBinding,
				Depth:            2,
				GenerationID:     "gen-a",
				GenerationStatus: "active",
				ObservedAt:       time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC),
			}},
			"producer-ambiguous": {{
				ConsumerRepoID:   "repo-candidate",
				EvidenceFamily:   "package_module_repo",
				Citation:         "repository_relationships:repo-candidate->repo-producer",
				Confidence:       codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName),
				ConfidenceLabel:  "low",
				ResolutionMethod: codeprovenance.MethodRepoUniqueName,
				Ambiguous:        true,
				NeedsEvidence:    true,
				Reason:           "ambiguous_consumer_ownership",
				GenerationID:     "gen-a",
				GenerationStatus: "active",
			}},
			"producer-stale": {{
				ConsumerRepoID:   "repo-consumer",
				EvidenceFamily:   "direct_code",
				Citation:         "code_reachability_rows:scope-a/gen-old/repo-consumer/root-checkout/producer-stale",
				Confidence:       codeprovenance.Confidence(codeprovenance.MethodImportBinding),
				ConfidenceLabel:  "high",
				ResolutionMethod: codeprovenance.MethodImportBinding,
				NeedsEvidence:    true,
				Reason:           "stale_generation",
				GenerationID:     "gen-old",
				GenerationStatus: "stale",
			}},
			"producer-cycle": {{
				ConsumerRepoID:   "repo-consumer",
				ConsumerRepoName: "checkout-api",
				ConsumerEntityID: "checkout-cycle-root",
				RelationshipType: "CALLS",
				EvidenceFamily:   "direct_code",
				Citation:         "code_reachability_rows:scope-a/gen-a/repo-consumer/checkout-cycle-root/producer-cycle",
				Confidence:       codeprovenance.Confidence(codeprovenance.MethodSCIP),
				ConfidenceLabel:  "high",
				ResolutionMethod: codeprovenance.MethodSCIP,
				Depth:            3,
				GenerationID:     "gen-a",
				GenerationStatus: "active",
				ObservedAt:       time.Date(2026, 6, 29, 10, 5, 0, 0, time.UTC),
			}},
		},
	}
	handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Content: content, Neo4j: fakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code/cross-repo",
		bytes.NewBufferString(`{"repo_id":"payments-lib","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := decodeEnvelopeData(t, w.Body.Bytes())
	if got, want := data["repo_id"], "repo-producer"; got != want {
		t.Fatalf("repo_id = %#v, want %#v", got, want)
	}
	if got, want := data["query_shape"], "bounded_cross_repo_dead_code"; got != want {
		t.Fatalf("query_shape = %#v, want %#v", got, want)
	}

	buckets := data["candidate_buckets"].(map[string]any)
	assertCrossRepoDeadCodeBucketEntity(t, buckets, "dead", "producer-dead")
	live := assertCrossRepoDeadCodeBucketEntity(t, buckets, "live_by_consumer", "producer-live")
	if got, want := live["classification"], "live_by_consumer"; got != want {
		t.Fatalf("live classification = %#v, want %#v", got, want)
	}
	evidence := live["consumer_evidence"].([]any)
	if got, want := len(evidence), 1; got != want {
		t.Fatalf("len(consumer_evidence) = %d, want %d", got, want)
	}
	evidenceRow := evidence[0].(map[string]any)
	for _, field := range []string{"citation", "confidence_label", "confidence", "consumer_repo_id", "evidence_family"} {
		if _, ok := evidenceRow[field]; !ok {
			t.Fatalf("consumer evidence missing %q: %#v", field, evidenceRow)
		}
	}
	cycleLive := assertCrossRepoDeadCodeBucketEntity(t, buckets, "live_by_consumer", "producer-cycle")
	assertCrossRepoDeadCodeEvidenceCitation(
		t,
		cycleLive,
		"code_reachability_rows:scope-a/gen-a/repo-consumer/checkout-cycle-root/producer-cycle",
	)
	unknown := assertCrossRepoDeadCodeBucketEntity(t, buckets, "unknown", "producer-ambiguous")
	if got, want := unknown["classification"], "unknown_needs_evidence"; got != want {
		t.Fatalf("unknown classification = %#v, want %#v", got, want)
	}
	assertCrossRepoDeadCodeReason(t, unknown, "ambiguous_consumer_ownership")
	stale := assertCrossRepoDeadCodeBucketEntity(t, buckets, "unknown", "producer-stale")
	assertCrossRepoDeadCodeReason(t, stale, "stale_generation")
}

func TestContentReaderCrossRepoDeadCodeEvidenceUsesBoundedEntityLookup(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 29, 11, 30, 0, 0, time.UTC)
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{
			"entity_id", "repository_id", "consumer_repo_name", "root_entity_id",
			"depth", "state", "confidence", "min_resolution_method", "evidence",
			"root_kinds", "generation_id", "generation_status", "observed_at", "updated_at",
		},
		rows: [][]driver.Value{{
			"producer-live", "repo-consumer", "checkout-api", "checkout-root",
			int64(2), "reachable", 0.9, codeprovenance.MethodImportBinding,
			[]byte(`["CALLS:checkout-root->producer-live"]`), []byte(`["go.main_function"]`),
			"gen-a", "active", observedAt, observedAt,
		}},
	}})
	reader := NewContentReader(db)

	evidence, err := reader.CrossRepoDeadCodeConsumerEvidence(
		context.Background(),
		"repo-producer",
		[]string{"producer-live", "producer-dead"},
	)
	if err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
	}
	if got, want := len(evidence["producer-live"]), 1; got != want {
		t.Fatalf("len(evidence[producer-live]) = %d, want %d", got, want)
	}
	query := recorder.queries[0]
	if strings.Contains(query, "MATCH ") || strings.Contains(query, "*]") {
		t.Fatalf("cross-repo dead-code evidence query must not use graph traversal:\n%s", query)
	}
	if !containsAllSubstrings(
		query,
		"FROM code_reachability_rows AS row",
		"row.entity_id IN ($2, $3)",
		"row.repository_id <> $1",
		"scope.active_generation_id = row.generation_id",
		"ORDER BY row.entity_id ASC, row.confidence DESC",
		"LIMIT",
	) {
		t.Fatalf("query missing bounded active-generation lookup clauses:\n%s", query)
	}
	if got, want := len(recorder.args[0]), 3; got != want {
		t.Fatalf("len(args) = %d, want %d args=%#v", got, want, recorder.args[0])
	}
}

func TestHandleCrossRepoDeadCodeRepositoryBoundaryEvidenceStaysUnknown(t *testing.T) {
	t.Parallel()

	content := &crossRepoDeadCodeContentStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			fakePortContentStore: fakePortContentStore{
				repositories: []RepositoryCatalogEntry{{ID: "repo-producer", Name: "payments-lib"}},
				relationshipReadModel: repositoryRelationshipReadModel{
					Available: true,
					Relationships: []map[string]any{{
						"direction":         "incoming",
						"type":              "DEPENDS_ON",
						"source_id":         "repo-consumer",
						"source_name":       "checkout-api",
						"target_id":         "repo-producer",
						"target_name":       "payments-lib",
						"resolved_id":       "relationship-1",
						"generation_id":     "relationship-generation-1",
						"confidence":        0.91,
						"evidence_count":    1,
						"evidence_type":     "go_module_import",
						"resolution_source": "evidence",
					}},
				},
			},
			entities: map[string]EntityContent{
				"producer-boundary": {
					EntityID:     "producer-boundary",
					RepoID:       "repo-producer",
					RelativePath: "pkg/payments/boundary.go",
					EntityType:   "Function",
					EntityName:   "maybeImported",
					Language:     "go",
					SourceCache:  "func maybeImported() {}",
				},
			},
		},
		rows: []map[string]any{
			deadCodeInvestigationRow("producer-boundary", "maybeImported", "go", "pkg/payments/boundary.go", 4, 8),
		},
		evidenceByEntity: map[string][]crossRepoDeadCodeEvidence{},
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
	unknown := assertCrossRepoDeadCodeBucketEntity(t, buckets, "unknown", "producer-boundary")
	assertCrossRepoDeadCodeReason(t, unknown, "package_module_repo_needs_symbol_evidence")
	assertCrossRepoDeadCodeEvidenceCitation(
		t,
		unknown,
		"repository_relationships:relationship-generation-1/relationship-1",
	)
	deadRows := buckets["dead"].([]any)
	if len(deadRows) != 0 {
		t.Fatalf("dead bucket = %#v, want repository-boundary candidate kept unknown", deadRows)
	}
}

func TestHandleCrossRepoDeadCodeScopedConsumerEvidenceBecomesUnknown(t *testing.T) {
	t.Parallel()

	content := &crossRepoDeadCodeContentStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			fakePortContentStore: fakePortContentStore{
				repositories: []RepositoryCatalogEntry{{ID: "repo-producer", Name: "payments-lib"}},
			},
			entities: map[string]EntityContent{
				"producer-live": {
					EntityID:     "producer-live",
					RepoID:       "repo-producer",
					RelativePath: "pkg/payments/live.go",
					EntityType:   "Function",
					EntityName:   "charge",
					Language:     "go",
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
				EvidenceFamily:   "direct_code",
				Citation:         "code_reachability_rows:scope-a/gen-a/repo-consumer/root-checkout/producer-live",
				Confidence:       codeprovenance.Confidence(codeprovenance.MethodImportBinding),
				ConfidenceLabel:  "high",
				ResolutionMethod: codeprovenance.MethodImportBinding,
				GenerationID:     "gen-a",
				GenerationStatus: "active",
			}},
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
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-producer"},
	}))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := decodeEnvelopeData(t, w.Body.Bytes())
	buckets := data["candidate_buckets"].(map[string]any)
	unknown := assertCrossRepoDeadCodeBucketEntity(t, buckets, "unknown", "producer-live")
	assertCrossRepoDeadCodeReason(t, unknown, "permission_hidden_consumer")
	if got, want := unknown["hidden_consumer_evidence_count"], float64(1); got != want {
		t.Fatalf("hidden_consumer_evidence_count = %#v, want %#v", got, want)
	}
	deadRows := buckets["dead"].([]any)
	if len(deadRows) != 0 {
		t.Fatalf("dead bucket = %#v, want no hidden-consumer candidate marked dead", deadRows)
	}
}

type crossRepoDeadCodeContentStore struct {
	fakeDeadCodeContentStore
	rows             []map[string]any
	evidenceByEntity map[string][]crossRepoDeadCodeEvidence
}

func (s *crossRepoDeadCodeContentStore) DeadCodeCandidateRows(
	_ context.Context,
	repoID string,
	label string,
	language string,
	limit int,
	offset int,
) ([]map[string]any, error) {
	if repoID != "repo-producer" || label != "Function" || language != "go" && language != "" {
		return nil, nil
	}
	if offset >= len(s.rows) {
		return nil, nil
	}
	end := offset + limit
	if end > len(s.rows) {
		end = len(s.rows)
	}
	return s.rows[offset:end], nil
}

func (s *crossRepoDeadCodeContentStore) CrossRepoDeadCodeConsumerEvidence(
	_ context.Context,
	_ string,
	entityIDs []string,
) (map[string][]crossRepoDeadCodeEvidence, error) {
	result := make(map[string][]crossRepoDeadCodeEvidence)
	for _, entityID := range entityIDs {
		result[entityID] = append([]crossRepoDeadCodeEvidence(nil), s.evidenceByEntity[entityID]...)
	}
	return result, nil
}

func assertCrossRepoDeadCodeBucketEntity(
	t *testing.T,
	buckets map[string]any,
	name string,
	entityID string,
) map[string]any {
	t.Helper()

	rawRows, ok := buckets[name].([]any)
	if !ok {
		t.Fatalf("candidate_buckets[%s] type = %T, want []any", name, buckets[name])
	}
	for _, raw := range rawRows {
		row := raw.(map[string]any)
		if row["entity_id"] == entityID {
			return row
		}
	}
	t.Fatalf("candidate_buckets[%s] missing entity %q: %#v", name, entityID, rawRows)
	return nil
}

func assertCrossRepoDeadCodeReason(t *testing.T, row map[string]any, want string) {
	t.Helper()

	rawReasons, ok := row["needs_evidence_reasons"].([]any)
	if !ok {
		t.Fatalf("needs_evidence_reasons type = %T, want []any", row["needs_evidence_reasons"])
	}
	for _, reason := range rawReasons {
		if reason == want {
			return
		}
	}
	t.Fatalf("needs_evidence_reasons = %#v, want %q", rawReasons, want)
}

func assertCrossRepoDeadCodeEvidenceCitation(t *testing.T, row map[string]any, want string) {
	t.Helper()

	rawEvidence, ok := row["consumer_evidence"].([]any)
	if !ok {
		t.Fatalf("consumer_evidence type = %T, want []any", row["consumer_evidence"])
	}
	for _, raw := range rawEvidence {
		evidence := raw.(map[string]any)
		if evidence["citation"] == want {
			return
		}
	}
	t.Fatalf("consumer_evidence = %#v, want citation %q", rawEvidence, want)
}
