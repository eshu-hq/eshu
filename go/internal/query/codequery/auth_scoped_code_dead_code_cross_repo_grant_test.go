// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
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
) (map[string][]deadcode.CrossRepoDeadCodeEvidence, crossRepoDeadCodeHiddenConsumers, error) {
	s.boundConsumerGrant = append([]string(nil), reads.PageRepositoryIDs...)
	s.signalRead = len(reads.SignalGrant) > 0
	evidence := make(map[string][]deadcode.CrossRepoDeadCodeEvidence, len(entityIDs))
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

func crossRepoDeadCodeGrantConsumerRow(consumerRepoID string, entityID string) deadcode.CrossRepoDeadCodeEvidence {
	return deadcode.CrossRepoDeadCodeEvidence{
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
	auth := querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo, codeGrantConsumerRepo})
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
	auth := querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
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
