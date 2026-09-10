// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// The ungranted-consumer probe's behavioural guards: the one input that would
// make it lie, the one it used to answer for a question nobody asked, and the
// coverage the row-returning read it replaced could not give.
//
// The statement-shape pins these sit beside are in
// code_dead_code_cross_repo_bound_test.go, whose header carries the contract
// all three of these files pin.

// TestCrossRepoDeadCodeProbeRefusesAnEmptyGrant covers the one input that would
// make the probe lie. Its ranges are the complement of the grant, so an empty
// grant makes every range empty and the answer "nothing is hidden" -- for a
// caller who may see nothing, where everything is.
//
// The refusals that keep such a caller away from the probe are
// crossRepoDeadCodeConsumerReadPlan's, and they are pinned where that function
// is exercised: TestCrossRepoDeadCodeConsumerReadPlan's "scoped caller with no
// grant at all reads nothing" and "scoped request naming only ungranted
// consumers reads nothing" (code_dead_code_cross_repo_selector_test.go).
// Neither subtest here calls the plan. The second one below is the read's own
// refusal, one call further down, for a future caller that reaches it directly.
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
	handler := &codequery.CodeHandler{Content: store, Profile: querycontract.ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo, codeGrantConsumerRepo})
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
	// The selector is what empties the signal read's contribution, so the read
	// itself is skipped rather than run and discarded.
	if store.signalRead {
		t.Fatal("signal read ran for a request whose consumer selector drops every one of its rows")
	}
	if !slices.Equal(store.boundConsumerGrant, []string{codeGrantConsumerRepo}) {
		t.Fatalf("page read bound %#v, want only the requested consumer", store.boundConsumerGrant)
	}
}

// TestCrossRepoDeadCodeProbeLeavesNoEntityUnproven is what the probe bought.
//
// The read it replaced returned rows and stopped at a shared 1001-row sentinel,
// so one producer entity with a large fan-in spent the whole budget and every
// later entity on the page came back consumer_evidence_truncated -- unknown, for
// symbols whose own evidence was complete. The probe answers per entity and
// returns at most one row each, so the busy entity costs the others nothing:
// only the evidence page can still leave an entity unproven.
