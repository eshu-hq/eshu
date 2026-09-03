// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCrossRepoDeadCodeHiddenConsumerCountIsBounded pins a row bound on the
// second statement, the way code_dead_code_cross_repo_test.go already pins the
// LIMIT on the first. The two statements read complementary sets: the evidence
// page reads the consumers the grant admits and stops at 1001 rows, while this
// one reads the consumers the grant excluded. Counting that complement with a
// plain GROUP BY put no ceiling on the rows read at all.
//
// The bound has to be per producer entity. One statement-wide LIMIT ordered by
// entity id would be spent on the first entity ids of the page, so a later
// producer would come back with a hidden count of zero and its candidate would
// be classified dead instead of unknown_needs_evidence.
func TestCrossRepoDeadCodeHiddenConsumerCountIsBounded(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: crossRepoDeadCodeEvidenceColumns()},
		{columns: []string{"entity_id", "hidden_count"}},
	})
	reader := NewContentReader(db)
	if _, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
		context.Background(),
		codeGrantGrantedRepo,
		[]string{"entity-1", "entity-2"},
		[]string{codeGrantConsumerRepo},
	); err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
	}
	if len(recorder.queries) != 2 {
		t.Fatalf("query count = %d, want 2 (evidence page plus hidden-consumer count)", len(recorder.queries))
	}
	hidden := recorder.queries[1]
	if !containsAllSubstrings(
		hidden,
		"unnest($2::text[]) AS ids(entity_id)",
		"CROSS JOIN LATERAL",
		"AND row.entity_id = ids.entity_id",
		"AND NOT (row.repository_id = ANY($3))",
		fmt.Sprintf("LIMIT %d", maxCrossRepoDeadCodeHiddenConsumerRowsPerEntity),
	) {
		t.Fatalf("hidden-consumer count is not bounded per producer entity:\n%s", hidden)
	}
	if got, want := len(recorder.args[1]), 3; got != want {
		t.Fatalf("len(args) = %d, want %d (producer repo, entity-id array, grant array)", got, want)
	}
	// The presence of the per-arm cap is not enough on its own. A statement-wide
	// LIMIT added after the LATERAL would be spent on the first entity ids of
	// the page and hand a later producer a hidden count of zero, which is the
	// wrong-answer shape this bound exists to prevent. The per-arm cap must
	// therefore be the only LIMIT in the statement, and nothing may follow the
	// closing of the LATERAL arm.
	if got := strings.Count(hidden, "LIMIT"); got != 1 {
		t.Fatalf("statement has %d LIMIT clauses, want exactly 1 (the per-entity cap inside the LATERAL):\n%s", got, hidden)
	}
	closing := strings.Index(hidden, ") AS capped_rows")
	if closing < 0 {
		t.Fatalf("the LATERAL arm's capped_rows subquery is gone:\n%s", hidden)
	}
	if strings.Contains(hidden[closing:], "LIMIT") {
		t.Fatalf("a LIMIT follows the LATERAL arm, so the bound is statement-wide rather than per producer entity:\n%s", hidden)
	}
}

// crossRepoDeadCodeSaturatedHiddenStore answers the consumer read the way the
// bounded statement does for a producer with more out-of-grant consumers than
// the per-entity cap: the count saturates at the cap and no consumer row is
// visible. It exists to prove the cap costs the magnitude and nothing else.
type crossRepoDeadCodeSaturatedHiddenStore struct {
	deadCodeGrantContentStore
}

func (*crossRepoDeadCodeSaturatedHiddenStore) CrossRepoDeadCodeConsumerEvidence(
	_ context.Context,
	_ string,
	entityIDs []string,
	_ []string,
) (map[string][]crossRepoDeadCodeEvidence, map[string]int, error) {
	hidden := make(map[string]int, len(entityIDs))
	for _, entityID := range entityIDs {
		hidden[entityID] = maxCrossRepoDeadCodeHiddenConsumerRowsPerEntity
	}
	return map[string][]crossRepoDeadCodeEvidence{}, hidden, nil
}

// TestCrossRepoDeadCodeHiddenConsumerSignalSurvivesTheCap is the other half of
// the bound. Capping the count must not cost the signal it carries: a producer
// whose out-of-grant consumers exceed the cap still answers
// unknown_needs_evidence with permission_hidden_consumer, and still reports a
// count, because the handler branches on "greater than zero" and that stays
// exact no matter where the magnitude saturates.
func TestCrossRepoDeadCodeHiddenConsumerSignalSurvivesTheCap(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: &crossRepoDeadCodeSaturatedHiddenStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	body := map[string]any{"repo_id": codeGrantGrantedRepo, "language": "go"}
	req := newCodeGrantRouteRequest(t, "/api/v0/code/dead-code/cross-repo", body, &auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	answer := rec.Body.String()
	if !strings.Contains(answer, "permission_hidden_consumer") {
		t.Fatalf("a saturated hidden count must still keep the candidate unknown_needs_evidence: %s", answer)
	}
	if !strings.Contains(answer, fmt.Sprintf(`"hidden_consumer_evidence_count":%d`, maxCrossRepoDeadCodeHiddenConsumerRowsPerEntity)) {
		t.Fatalf("hidden consumer count did not survive the cap: %s", answer)
	}
	if strings.Contains(answer, `"classification":"dead"`) {
		t.Fatalf("a symbol whose out-of-grant consumers exceed the cap was marked dead: %s", answer)
	}
}
