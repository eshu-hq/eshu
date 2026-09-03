// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// POST /api/v0/code/dead-code/cross-repo runs two consumer reads for a scoped
// caller, and the pair is what this file pins.
//
// The evidence page carries the grant, so the row cap falls on consumers the
// caller may see. The signal read carries no grant and answers the other half:
// is there a consumer this caller cannot see? Both stop at the same 1001-row
// sentinel, so neither is an unbounded scan, and the second one's text is the
// statement this route shipped before the grant landed.
//
// The count taken from the signal read is not a raw row count. The request's
// own consumer_repo_ids selector is applied to those rows first, because a
// consumer the caller explicitly excluded must not override the evidence of
// one it asked about.

// crossRepoDeadCodeUngrantedConsumerStatement is the signal read's text for a
// two-entity page, pinned as a literal rather than rebuilt from the builder, so
// this test fails if the statement drifts rather than drifting with it. It is
// byte for byte the statement `buildCrossRepoDeadCodeConsumerEvidenceQuery`
// emitted before the grant clause existed.
const crossRepoDeadCodeUngrantedConsumerStatement = `
SELECT row.entity_id,
       row.repository_id,
       '' AS consumer_repo_name,
       row.root_entity_id,
       row.depth,
       row.state,
       row.confidence,
       row.min_resolution_method,
       row.evidence,
       row.root_kinds,
       row.generation_id,
       generation.status AS generation_status,
       row.observed_at,
       row.updated_at
FROM code_reachability_rows AS row
JOIN ingestion_scopes AS scope
  ON scope.scope_id = row.scope_id
 AND scope.active_generation_id = row.generation_id
JOIN scope_generations AS generation
  ON generation.generation_id = row.generation_id
 AND generation.status = 'active'
WHERE row.repository_id <> $1
  AND row.entity_id IN ($2, $3)
  AND row.depth > 0
ORDER BY row.entity_id ASC, row.confidence DESC, row.depth ASC,
         row.repository_id ASC, row.root_entity_id ASC
LIMIT 1001
`

// TestCrossRepoDeadCodeSignalReadRepeatsTheUngrantedStatement pins both
// statements a scoped request sends. The page must carry the grant ahead of its
// LIMIT; the signal read must carry no grant at all and be the statement this
// route already shipped, whose plan and 1001-row bound are what let it run on
// every scoped request.
func TestCrossRepoDeadCodeSignalReadRepeatsTheUngrantedStatement(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: crossRepoDeadCodeEvidenceColumns()},
		{columns: crossRepoDeadCodeEvidenceColumns()},
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
		t.Fatalf("query count = %d, want 2 (grant-bound evidence page plus ungranted signal read)", len(recorder.queries))
	}

	page := recorder.queries[0]
	grant := "AND row.repository_id = ANY($4)"
	if !strings.Contains(page, grant) {
		t.Fatalf("evidence page is missing %q, so its LIMIT is drawn from every tenant's rows:\n%s", grant, page)
	}
	if strings.Index(page, grant) > strings.Index(page, "LIMIT") {
		t.Fatalf("the grant sits after the LIMIT, so the page is still cut from a mixed set:\n%s", page)
	}

	signal := recorder.queries[1]
	if signal != crossRepoDeadCodeUngrantedConsumerStatement {
		t.Fatalf("signal read drifted from the statement this route already shipped:\ngot:\n%s\nwant:\n%s", signal, crossRepoDeadCodeUngrantedConsumerStatement)
	}
	if got, want := len(recorder.args[1]), 3; got != want {
		t.Fatalf("len(args) = %d, want %d (producer repo plus one per producer entity, no grant array)", got, want)
	}
}

// TestCrossRepoDeadCodeHiddenCountHonoursTheConsumerSelector is the case the
// count exists to get right and the one it used to get wrong.
//
// The caller is granted the producer and one consumer, and asks about that
// consumer alone. An unrelated repository outside the grant also consumes the
// symbol. That consumer is not part of the question, so it must not be counted
// as hidden -- counting it buried the requested consumer's own strong evidence
// under permission_hidden_consumer and answered unknown_needs_evidence for a
// symbol the answer could prove live.
func TestCrossRepoDeadCodeHiddenCountHonoursTheConsumerSelector(t *testing.T) {
	t.Parallel()

	store := &crossRepoDeadCodeGrantStore{}
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo, codeGrantConsumerRepo})
	req := newCodeGrantRouteRequest(t, "/api/v0/code/dead-code/cross-repo", map[string]any{
		"repo_id":           codeGrantGrantedRepo,
		"language":          "go",
		"consumer_repo_ids": []string{codeGrantConsumerRepo},
	}, &auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	answer := rec.Body.String()
	if !strings.Contains(answer, `"classification":"live_by_consumer"`) {
		t.Fatalf("the requested consumer's own strong evidence must answer live_by_consumer: %s", answer)
	}
	if strings.Contains(answer, "permission_hidden_consumer") {
		t.Fatalf("a consumer outside the requested set was counted as hidden: %s", answer)
	}
	if strings.Contains(answer, "hidden_consumer_evidence_count") {
		t.Fatalf("hidden count reported for a consumer the caller did not ask about: %s", answer)
	}
	if strings.Contains(answer, codeGrantOtherRepo) {
		t.Fatalf("response leaked the out-of-grant consumer %q: %s", codeGrantOtherRepo, answer)
	}
}

// TestCrossRepoDeadCodeSignalTruncationKeepsCandidatesUnknown covers the case
// the second read adds to the truncation fail-safe. The signal read can reach
// the 1001-row sentinel on its own -- it sees every tenant's consumers, so it
// reaches it sooner than the page does -- and an entity whose evidence stops
// there must stay unknown, never fall through to dead.
func TestCrossRepoDeadCodeSignalTruncationKeepsCandidatesUnknown(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 29, 13, 0, 0, 0, time.UTC)
	signalRows := make([][]driver.Value, 0, maxCrossRepoDeadCodeConsumerEvidenceRows+1)
	for i := 0; i < maxCrossRepoDeadCodeConsumerEvidenceRows+1; i++ {
		signalRows = append(signalRows, []driver.Value{
			"producer-live", codeGrantOtherRepo, "checkout-api", "checkout-root",
			int64(2), "reachable", 0.9, codeprovenance.MethodImportBinding,
			[]byte(`["CALLS:checkout-root->producer-live"]`), []byte(`["go.main_function"]`),
			"gen-a", "active", observedAt, observedAt,
		})
	}
	db, _ := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: crossRepoDeadCodeEvidenceColumns()},
		{columns: crossRepoDeadCodeEvidenceColumns(), rows: signalRows},
	})
	reader := NewContentReader(db)

	evidence, signal, err := reader.CrossRepoDeadCodeConsumerEvidence(
		context.Background(),
		codeGrantGrantedRepo,
		[]string{"producer-live", "producer-missing"},
		[]string{codeGrantConsumerRepo},
	)
	if err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
	}
	if got, want := len(signal["producer-live"]), maxCrossRepoDeadCodeConsumerEvidenceRows; got != want {
		t.Fatalf("len(signal[producer-live]) = %d, want the read to stop at the sentinel (%d)", got, want)
	}
	for _, entityID := range []string{"producer-live", "producer-missing"} {
		rows := evidence[entityID]
		if len(rows) != 1 || rows[0].Reason != "consumer_evidence_truncated" || !rows[0].NeedsEvidence {
			t.Fatalf("evidence[%s] = %#v, want the consumer_evidence_truncated marker so the candidate cannot fall through to dead", entityID, rows)
		}
	}
}
