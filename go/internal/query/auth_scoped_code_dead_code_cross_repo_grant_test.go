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

// The consumer-side half of POST /api/v0/code/dead-code/cross-repo's grant
// proof: the evidence page, which must not return a consumer the caller may not
// read, and the ungranted-consumer probe, which must still report that one
// exists without naming it.
//
// The producer-side candidate scan the three dead-code routes share, and the
// contract note for all of them, are in auth_scoped_code_dead_code_grant_test.go.

// codeGrantConsumerRepo is a second repository inside the caller's grant, so
// the cross-repo consumer tests can tell "dropped because ungranted" apart
// from "dropped because it is the producer".
const codeGrantConsumerRepo = "repo://tenant-a/consumer-service"

// crossRepoDeadCodeGrantStore answers both reads POST
// /api/v0/code/dead-code/cross-repo makes: the producer candidate scan and the
// consumer-evidence lookup. The evidence half mirrors both statements the
// shipped reader runs -- the grant-bound page, which excludes the consumers the
// caller may not see, and the ungranted-consumer probe, which reports the
// producer entities that have one -- so a handler that stops passing the grant
// gets the other tenant's consumer back in the page.
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
) (map[string][]crossRepoDeadCodeEvidence, crossRepoDeadCodeHiddenConsumers, error) {
	s.boundConsumerGrant = append([]string(nil), reads.PageRepositoryIDs...)
	s.signalRead = len(reads.SignalGrant) > 0
	evidence := make(map[string][]crossRepoDeadCodeEvidence, len(entityIDs))
	hidden := crossRepoDeadCodeHiddenConsumers{}
	for _, entityID := range entityIDs {
		for _, consumerRepoID := range []string{codeGrantConsumerRepo, codeGrantOtherRepo} {
			if consumerRepoID == producerRepoID {
				continue
			}
			row := crossRepoDeadCodeGrantConsumerRow(consumerRepoID, entityID)
			// The probe answers over the complement of reads.SignalGrant, so a
			// consumer inside it is not hidden however the page was bound.
			if len(reads.SignalGrant) > 0 && !slices.Contains(reads.SignalGrant, consumerRepoID) {
				hidden[entityID] = struct{}{}
			}
			if len(reads.PageRepositoryIDs) > 0 && !slices.Contains(reads.PageRepositoryIDs, consumerRepoID) {
				continue
			}
			evidence[entityID] = append(evidence[entityID], row)
		}
	}
	return evidence, hidden, nil
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
// dead code: the probe reports that the entity has one, and the handler still
// answers unknown_needs_evidence with permission_hidden_consumer.
//
// The count is one per producer entity, not one per hidden consumer row. The
// probe stops at the first HIDDEN consumer -- ungranted and live -- rather than
// enumerating them, and the classification only ever depended on whether there
// was one. The number it
// replaced was never a total either: the read it came from was capped at 1,001
// rows across the whole page.
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
	if !strings.Contains(body, `"hidden_consumer_evidence_count":1`) {
		t.Fatalf("hidden consumer count is missing from the answer; this entity's consumers are outside this grant: %s", body)
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
			crossRepoDeadCodeConsumerReads{
				PageRepositoryIDs: []string{codeGrantConsumerRepo},
				SignalGrant:       []string{codeGrantConsumerRepo},
			},
		); err != nil {
			t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
		}
		if len(recorder.queries) != 2 {
			t.Fatalf("query count = %d, want 2 (grant-bound evidence page plus ungranted-consumer probe)", len(recorder.queries))
		}
		want := "AND row.repository_id = ANY($3)"
		if !strings.Contains(recorder.queries[0], want) {
			t.Fatalf("consumer-evidence SQL is missing %q, so the LIMIT is still drawn from every tenant's rows:\n%s", want, recorder.queries[0])
		}
		if recorder.queries[1] != crossRepoDeadCodeUngrantedConsumerProbeQuery {
			t.Fatalf("second statement is not the ungranted-consumer probe:\n%s", recorder.queries[1])
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
			t.Fatalf("query count = %d, want 1 -- an unscoped caller must not pay for the probe", len(recorder.queries))
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
