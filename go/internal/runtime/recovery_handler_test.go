// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

func TestNewRecoveryHandlerRequiresHandler(t *testing.T) {
	t.Parallel()

	_, err := NewRecoveryHandler(nil)
	if err == nil {
		t.Fatal("NewRecoveryHandler(nil) error = nil, want non-nil")
	}
}

func TestRecoveryHandlerReplayReturnsReplayedItems(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{
		replayResult: recovery.ReplayResult{
			Stage:                       recovery.StageProjector,
			Replayed:                    2,
			WorkItemIDs:                 []string{"item-1", "item-2"},
			SkippedSupersededGeneration: 3,
		},
	}
	handler := mustNewRecoveryHandler(t, store)

	body := mustMarshal(t, replayRequest{
		Stage:    "projector",
		ScopeIDs: []string{"s1"},
		Limit:    10,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/replay", bytes.NewReader(body))

	handler.handleReplay(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("POST /admin/replay status = %d, want %d", got, want)
	}

	var resp replayResponse
	mustUnmarshal(t, recorder.Body.Bytes(), &resp)
	if resp.Status != "replayed" {
		t.Fatalf("response status = %q, want %q", resp.Status, "replayed")
	}
	if resp.Replayed != 2 {
		t.Fatalf("response replayed = %d, want 2", resp.Replayed)
	}
	if len(resp.WorkItemIDs) != 2 {
		t.Fatalf("response work_item_ids len = %d, want 2", len(resp.WorkItemIDs))
	}
	// #7130: rows left terminal on a superseded generation are reported, not
	// silently dropped from the replay.
	if resp.SkippedSupersededGeneration != 3 {
		t.Fatalf("response skipped_superseded_generation = %d, want 3", resp.SkippedSupersededGeneration)
	}
}

func TestRecoveryHandlerReplayRejectsGetMethod(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{}
	handler := mustNewRecoveryHandler(t, store)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/replay", nil)

	handler.handleReplay(recorder, request)

	if got, want := recorder.Code, http.StatusMethodNotAllowed; got != want {
		t.Fatalf("GET /admin/replay status = %d, want %d", got, want)
	}
}

func TestRecoveryHandlerReplayRejectsInvalidStage(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{}
	handler := mustNewRecoveryHandler(t, store)

	body := mustMarshal(t, replayRequest{Stage: "invalid"})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/replay", bytes.NewReader(body))

	handler.handleReplay(recorder, request)

	if got, want := recorder.Code, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("POST /admin/replay invalid stage status = %d, want %d", got, want)
	}
}

// TestRecoveryHandlerReplayStoreErrorAnswersFixedMessage is the #8394
// regression test for the replay site: a store failure must answer a fixed
// message, never the store error text (which quotes SQL, hosts, and driver
// state), with the cause on the request span and in the server log.
func TestRecoveryHandlerReplayStoreErrorAnswersFixedMessage(t *testing.T) {
	t.Parallel()

	const storeErr = `replay failed work items: pq: relation "projector_work_items" does not exist (SQLSTATE 42P01) host=db-primary:5432`
	store := &fakeRecoveryStore{replayErr: errors.New(storeErr)}
	handler := mustNewRecoveryHandler(t, store)

	body := mustMarshal(t, replayRequest{Stage: "projector"})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/replay", bytes.NewReader(body))

	handler.handleReplay(recorder, request)

	if got, want := recorder.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("POST /admin/replay store error status = %d, want %d", got, want)
	}
	assertFixedErrorBody(t, recorder.Body.Bytes(), "replay failed", []string{`pq:`, `projector_work_items`, `SQLSTATE`, `db-primary`})
}

// TestRecoveryHandlerReplayCollectorGenerationsStoreErrorAnswersFixedMessage
// is the #8394 regression test for the collector-generations site.
func TestRecoveryHandlerReplayCollectorGenerationsStoreErrorAnswersFixedMessage(t *testing.T) {
	t.Parallel()

	const storeErr = `collector generation replay: dial tcp 10.0.4.15:5432: connect: connection refused host=db-replica`
	store := &fakeRecoveryStore{collectorGenerationReplayErr: errors.New(storeErr)}
	handler := mustNewRecoveryHandler(t, store)

	body := mustMarshal(t, collectorGenerationReplayRequest{CollectorKind: "git"})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/replay-collector-generations", bytes.NewReader(body))

	handler.handleReplayCollectorGenerations(recorder, request)

	if got, want := recorder.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("POST /admin/replay-collector-generations store error status = %d, want %d", got, want)
	}
	assertFixedErrorBody(t, recorder.Body.Bytes(), "collector generation replay failed", []string{`10.0.4.15`, `connection refused`, `db-replica`})
}

// TestRecoveryHandlerRefinalizeStoreErrorAnswersFixedMessage is the #8394
// regression test for the refinalize site.
func TestRecoveryHandlerRefinalizeStoreErrorAnswersFixedMessage(t *testing.T) {
	t.Parallel()

	const storeErr = `refinalize scope projections: pq: duplicate key value violates unique constraint "reducer_work_pkey" host=db-primary`
	store := &fakeRecoveryStore{refinalizeErr: errors.New(storeErr)}
	handler := mustNewRecoveryHandler(t, store)

	body := mustMarshal(t, refinalizeRequest{ScopeIDs: []string{"s1"}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/refinalize", bytes.NewReader(body))

	handler.handleRefinalize(recorder, request)

	if got, want := recorder.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("POST /admin/refinalize store error status = %d, want %d", got, want)
	}
	assertFixedErrorBody(t, recorder.Body.Bytes(), "refinalize failed", []string{`duplicate key`, `reducer_work_pkey`, `db-primary`})
}

// TestRecoveryHandlerValidationErrorsKeepTheirText pins the #8394 validation
// contract: a caller-actionable filter rejection keeps its text (the caller
// can act on it), still answered 422.
func TestRecoveryHandlerValidationErrorsKeepTheirText(t *testing.T) {
	t.Parallel()

	t.Run("replay invalid stage", func(t *testing.T) {
		t.Parallel()

		handler := mustNewRecoveryHandler(t, &fakeRecoveryStore{})
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/admin/replay",
			bytes.NewReader(mustMarshal(t, replayRequest{Stage: "bogus"})))

		handler.handleReplay(recorder, request)

		if got, want := recorder.Code, http.StatusUnprocessableEntity; got != want {
			t.Fatalf("status = %d, want %d", got, want)
		}
		if got := decodeErrorBody(t, recorder.Body.Bytes()); !strings.Contains(got, `replay filter requires a valid stage, got "bogus"`) {
			t.Fatalf("body error = %q, want it to keep the validation text", got)
		}
	})

	t.Run("collector generations blank kind", func(t *testing.T) {
		t.Parallel()

		handler := mustNewRecoveryHandler(t, &fakeRecoveryStore{})
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/admin/replay-collector-generations",
			bytes.NewReader(mustMarshal(t, collectorGenerationReplayRequest{ScopeIDs: []string{"s1"}})))

		handler.handleReplayCollectorGenerations(recorder, request)

		if got, want := recorder.Code, http.StatusUnprocessableEntity; got != want {
			t.Fatalf("status = %d, want %d", got, want)
		}
		if got := decodeErrorBody(t, recorder.Body.Bytes()); !strings.Contains(got, "collector generation replay requires collector_kind") {
			t.Fatalf("body error = %q, want it to keep the validation text", got)
		}
	})

	t.Run("refinalize empty scopes", func(t *testing.T) {
		t.Parallel()

		handler := mustNewRecoveryHandler(t, &fakeRecoveryStore{})
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/admin/refinalize",
			bytes.NewReader(mustMarshal(t, refinalizeRequest{ScopeIDs: []string{}})))

		handler.handleRefinalize(recorder, request)

		if got, want := recorder.Code, http.StatusUnprocessableEntity; got != want {
			t.Fatalf("status = %d, want %d", got, want)
		}
		if got := decodeErrorBody(t, recorder.Body.Bytes()); !strings.Contains(got, "at least one scope_id") {
			t.Fatalf("body error = %q, want it to keep the validation text", got)
		}
	})
}

// TestRecoveryHandlerClientCancelAnswers499 pins the #8394 cancel contract: a
// caller that walks away from its own request answers 499 with the fixed
// message, not a server fault, and the store text still stays out of the body.
func TestRecoveryHandlerClientCancelAnswers499(t *testing.T) {
	t.Parallel()

	const storeErr = "tcp read host=db-primary: i/o timeout"
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	for _, site := range []struct {
		name    string
		store   *fakeRecoveryStore
		path    string
		body    []byte
		serve   func(h *RecoveryHandler, w http.ResponseWriter, r *http.Request)
		message string
	}{
		{
			name:    "replay",
			store:   &fakeRecoveryStore{replayErr: fmt.Errorf("replay failed work items: %w: %s", context.Canceled, storeErr)},
			path:    "/admin/replay",
			body:    mustMarshal(t, replayRequest{Stage: "projector"}),
			serve:   func(h *RecoveryHandler, w http.ResponseWriter, r *http.Request) { h.handleReplay(w, r) },
			message: "replay failed",
		},
		{
			name:  "collector generations",
			store: &fakeRecoveryStore{collectorGenerationReplayErr: fmt.Errorf("collector generation replay: %w: %s", context.Canceled, storeErr)},
			path:  "/admin/replay-collector-generations",
			body:  mustMarshal(t, collectorGenerationReplayRequest{CollectorKind: "git"}),
			serve: func(h *RecoveryHandler, w http.ResponseWriter, r *http.Request) {
				h.handleReplayCollectorGenerations(w, r)
			},
			message: "collector generation replay failed",
		},
		{
			name:    "refinalize",
			store:   &fakeRecoveryStore{refinalizeErr: fmt.Errorf("refinalize scope projections: %w: %s", context.Canceled, storeErr)},
			path:    "/admin/refinalize",
			body:    mustMarshal(t, refinalizeRequest{ScopeIDs: []string{"s1"}}),
			serve:   func(h *RecoveryHandler, w http.ResponseWriter, r *http.Request) { h.handleRefinalize(w, r) },
			message: "refinalize failed",
		},
	} {
		site := site
		t.Run(site.name, func(t *testing.T) {
			t.Parallel()

			handler := mustNewRecoveryHandler(t, site.store)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, site.path, bytes.NewReader(site.body)).WithContext(canceled)

			site.serve(handler, recorder, request)

			if got, want := recorder.Code, 499; got != want {
				t.Fatalf("POST %s canceled status = %d, want %d", site.path, got, want)
			}
			assertFixedErrorBody(t, recorder.Body.Bytes(), site.message, []string{"db-primary", "i/o timeout"})
		})
	}
}

// TestRecoveryHandlerInnerCancelOnLiveRequestIsAFault pins that a
// context.Canceled from an inner context on a live request is a server fault
// (500), not a client cancel: only a canceled request context answers 499.
func TestRecoveryHandlerInnerCancelOnLiveRequestIsAFault(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{replayErr: fmt.Errorf("replay failed work items: %w", context.Canceled)}
	handler := mustNewRecoveryHandler(t, store)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/replay",
		bytes.NewReader(mustMarshal(t, replayRequest{Stage: "projector"})))

	handler.handleReplay(recorder, request)

	if got, want := recorder.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("POST /admin/replay inner-cancel status = %d, want %d", got, want)
	}
	assertFixedErrorBody(t, recorder.Body.Bytes(), "replay failed", []string{"canceled"})
}

// TestRecoveryHandlerRefinalizeClaimFenceBusyAnswersConflict pins that the
// refinalize claim-fence contention verdict keeps its 409 signal with the
// fixed sentinel text, matching the /api/v0/admin/refinalize surface.
func TestRecoveryHandlerRefinalizeClaimFenceBusyAnswersConflict(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{refinalizeErr: recovery.ErrRefinalizeClaimFenceBusy}
	handler := mustNewRecoveryHandler(t, store)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/refinalize",
		bytes.NewReader(mustMarshal(t, refinalizeRequest{ScopeIDs: []string{"s1"}})))

	handler.handleRefinalize(recorder, request)

	if got, want := recorder.Code, http.StatusConflict; got != want {
		t.Fatalf("POST /admin/refinalize claim-fence-busy status = %d, want %d", got, want)
	}
	assertFixedErrorBody(t, recorder.Body.Bytes(), "blocked/claim_fence_busy", nil)
}

// assertFixedErrorBody checks the #8394 body contract: the error is exactly
// the fixed message, and none of the leak markers (store error fragments)
// appear anywhere in the body.
func assertFixedErrorBody(t *testing.T, body []byte, message string, leakMarkers []string) {
	t.Helper()

	if got := decodeErrorBody(t, body); got != message {
		t.Fatalf("body error = %q, want fixed message %q", got, message)
	}
	for _, marker := range leakMarkers {
		if strings.Contains(string(body), marker) {
			t.Fatalf("body leaks store text %q: %s", marker, body)
		}
	}
}

// decodeErrorBody extracts the error text from a JSON error body.
func decodeErrorBody(t *testing.T, body []byte) string {
	t.Helper()

	var resp jsonErrorResponse
	mustUnmarshal(t, body, &resp)
	return resp.Error
}

func TestRecoveryHandlerReplayCollectorGenerationsReturnsReplayRequests(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{
		collectorGenerationReplayResult: recovery.CollectorGenerationReplayResult{
			Replayed:      1,
			GenerationIDs: []string{"generation-456"},
		},
	}
	handler := mustNewRecoveryHandler(t, store)

	body := mustMarshal(t, collectorGenerationReplayRequest{
		ScopeIDs:      []string{"scope-123"},
		FailureClass:  "commit_failure",
		CollectorKind: "git",
		Limit:         10,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/replay-collector-generations", bytes.NewReader(body))

	handler.handleReplayCollectorGenerations(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("POST /admin/replay-collector-generations status = %d, want %d", got, want)
	}

	var resp collectorGenerationReplayResponse
	mustUnmarshal(t, recorder.Body.Bytes(), &resp)
	if resp.Status != "replay_requested" {
		t.Fatalf("response status = %q, want replay_requested", resp.Status)
	}
	if resp.Replayed != 1 || len(resp.GenerationIDs) != 1 || resp.GenerationIDs[0] != "generation-456" {
		t.Fatalf("response = %#v, want generation replay request", resp)
	}
	if got := store.collectorGenerationReplayFilter.CollectorKind; got != "git" {
		t.Fatalf("store collector kind = %q, want git", got)
	}
}

func TestRecoveryHandlerReplayCollectorGenerationsRejectsBlankCollectorKind(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{}
	handler := mustNewRecoveryHandler(t, store)

	body := mustMarshal(t, collectorGenerationReplayRequest{ScopeIDs: []string{"scope-123"}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/replay-collector-generations", bytes.NewReader(body))

	handler.handleReplayCollectorGenerations(recorder, request)

	if got, want := recorder.Code, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("POST /admin/replay-collector-generations invalid request status = %d, want %d", got, want)
	}
}

func TestRecoveryHandlerRefinalizeReturnsEnqueuedScopes(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{
		refinalizeResult: recovery.RefinalizeResult{
			Enqueued:                          2,
			ScopeIDs:                          []string{"s1", "s2"},
			SharedProjectionAcceptanceCleared: 4,
		},
	}
	handler := mustNewRecoveryHandler(t, store)

	body := mustMarshal(t, refinalizeRequest{ScopeIDs: []string{"s1", "s2"}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/refinalize", bytes.NewReader(body))

	handler.handleRefinalize(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("POST /admin/refinalize status = %d, want %d", got, want)
	}

	var resp refinalizeResponse
	mustUnmarshal(t, recorder.Body.Bytes(), &resp)
	if resp.Status != "enqueued" {
		t.Fatalf("response status = %q, want %q", resp.Status, "enqueued")
	}
	if resp.Enqueued != 2 {
		t.Fatalf("response enqueued = %d, want 2", resp.Enqueued)
	}
	if resp.SharedProjectionAcceptanceCleared != 4 {
		t.Fatalf("response shared_projection_acceptance_cleared = %d, want 4", resp.SharedProjectionAcceptanceCleared)
	}
}

// TestRecoveryHandlerRefinalizeReportsSkippedScopes carries the #7116 skipped
// report on the runtime admin surface: a rebuild that left a scope out must say
// so, and must say "nothing skipped" with an object, not a null.
func TestRecoveryHandlerRefinalizeReportsSkippedScopes(t *testing.T) {
	t.Parallel()

	var skipped recovery.SkippedScopes
	skipped.Add(recovery.SkipReasonNoRecoverableGeneration, "s3")
	store := &fakeRecoveryStore{
		refinalizeResult: recovery.RefinalizeResult{Enqueued: 1, ScopeIDs: []string{"s1"}, Skipped: skipped},
	}
	handler := mustNewRecoveryHandler(t, store)

	recorder := httptest.NewRecorder()
	handler.handleRefinalize(recorder, httptest.NewRequest(http.MethodPost, "/admin/refinalize",
		bytes.NewReader(mustMarshal(t, refinalizeRequest{ScopeIDs: []string{"s1", "s3"}}))))
	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	var resp refinalizeResponse
	mustUnmarshal(t, recorder.Body.Bytes(), &resp)
	if got, want := resp.SkippedScopes.Total, 1; got != want {
		t.Fatalf("skipped_scopes.total = %d, want %d", got, want)
	}
	if got := resp.SkippedScopes.ByReason[recovery.SkipReasonNoRecoverableGeneration]; got != 1 {
		t.Fatalf("skipped_scopes.by_reason[no_recoverable_generation] = %d, want 1", got)
	}

	empty := &fakeRecoveryStore{refinalizeResult: recovery.RefinalizeResult{Enqueued: 1, ScopeIDs: []string{"s1"}}}
	recorder = httptest.NewRecorder()
	mustNewRecoveryHandler(t, empty).handleRefinalize(recorder, httptest.NewRequest(http.MethodPost, "/admin/refinalize",
		bytes.NewReader(mustMarshal(t, refinalizeRequest{ScopeIDs: []string{"s1"}}))))
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"by_reason":{}`)) ||
		!bytes.Contains(recorder.Body.Bytes(), []byte(`"sample_scope_ids":{}`)) {
		t.Fatalf("empty report must encode objects, not null: %s", recorder.Body.String())
	}
}

// TestRecoveryHandlerRefinalizeReportsDeltaActiveScopes carries the #7797
// delta-active report on the runtime admin surface, always as an object.
func TestRecoveryHandlerRefinalizeReportsDeltaActiveScopes(t *testing.T) {
	t.Parallel()

	var delta recovery.DeltaActiveScopes
	delta.Add(recovery.DeltaActiveOutcomeReindexRequested, "s1")
	store := &fakeRecoveryStore{
		refinalizeResult: recovery.RefinalizeResult{Enqueued: 1, ScopeIDs: []string{"s1"}, DeltaActive: delta},
	}
	recorder := httptest.NewRecorder()
	mustNewRecoveryHandler(t, store).handleRefinalize(recorder, httptest.NewRequest(http.MethodPost, "/admin/refinalize",
		bytes.NewReader(mustMarshal(t, refinalizeRequest{ScopeIDs: []string{"s1"}}))))
	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	var resp refinalizeResponse
	mustUnmarshal(t, recorder.Body.Bytes(), &resp)
	if got := resp.DeltaActiveScopes.ByOutcome[recovery.DeltaActiveOutcomeReindexRequested]; got != 1 || resp.DeltaActiveScopes.Total != 1 {
		t.Fatalf("delta_active_scopes = %+v, want one reindex_requested scope", resp.DeltaActiveScopes)
	}
	if resp.DeltaActiveScopes.Detail != recovery.DeltaActiveDetail {
		t.Fatalf("delta_active_scopes.detail = %q, want the incomplete-graph message", resp.DeltaActiveScopes.Detail)
	}
	if got := resp.ReindexRequestsWritten; got.Count != 1 || len(got.ScopeIDs) != 1 || got.ScopeIDs[0] != "s1" {
		t.Fatalf("reindex_requests_written = %+v, want count 1 naming s1", got)
	}
}

// TestRecoveryHandlerRefinalizeReportsUnsupportedDeltaWithoutReindex pins the
// reindex_unsupported outcome on the runtime surface: a ref-scope delta is
// reported as delta-active, and reindex_requests_written stays a zero count
// with an empty, non-null list, because no reindex row was written for it.
func TestRecoveryHandlerRefinalizeReportsUnsupportedDeltaWithoutReindex(t *testing.T) {
	t.Parallel()

	var delta recovery.DeltaActiveScopes
	delta.Add(recovery.DeltaActiveOutcomeReindexUnsupported, "git-repository-scope:repo-a@feature")
	store := &fakeRecoveryStore{
		refinalizeResult: recovery.RefinalizeResult{Enqueued: 1, ScopeIDs: []string{"git-repository-scope:repo-a@feature"}, DeltaActive: delta},
	}
	recorder := httptest.NewRecorder()
	mustNewRecoveryHandler(t, store).handleRefinalize(recorder, httptest.NewRequest(http.MethodPost, "/admin/refinalize",
		bytes.NewReader(mustMarshal(t, refinalizeRequest{ScopeIDs: []string{"git-repository-scope:repo-a@feature"}}))))
	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"reindex_requests_written":{"count":0,"scope_ids":[]}`)) {
		t.Fatalf("body = %s, want reindex_requests_written with a zero count and an empty list", recorder.Body.String())
	}
	var resp refinalizeResponse
	mustUnmarshal(t, recorder.Body.Bytes(), &resp)
	if got := resp.DeltaActiveScopes.ByOutcome[recovery.DeltaActiveOutcomeReindexUnsupported]; got != 1 {
		t.Fatalf("delta_active_scopes.by_outcome[reindex_unsupported] = %d, want 1", got)
	}
	if _, ok := resp.DeltaActiveScopes.ByOutcome[recovery.DeltaActiveOutcomeReindexRequested]; ok {
		t.Fatalf("delta_active_scopes.by_outcome = %v, want no reindex_requested entry", resp.DeltaActiveScopes.ByOutcome)
	}
}

func TestRecoveryHandlerRefinalizeRejectsEmptyScopeIDs(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{}
	handler := mustNewRecoveryHandler(t, store)

	body := mustMarshal(t, refinalizeRequest{ScopeIDs: []string{}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/refinalize", bytes.NewReader(body))

	handler.handleRefinalize(recorder, request)

	if got, want := recorder.Code, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("POST /admin/refinalize empty scopes status = %d, want %d", got, want)
	}
}

func TestRecoveryHandlerRefinalizeRejectsGetMethod(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{}
	handler := mustNewRecoveryHandler(t, store)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/refinalize", nil)

	handler.handleRefinalize(recorder, request)

	if got, want := recorder.Code, http.StatusMethodNotAllowed; got != want {
		t.Fatalf("GET /admin/refinalize status = %d, want %d", got, want)
	}
}

func TestRecoveryHandlerMount(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{
		replayResult: recovery.ReplayResult{Stage: recovery.StageProjector},
	}
	handler := mustNewRecoveryHandler(t, store)

	mux := http.NewServeMux()
	handler.Mount(mux)

	// Verify replay route is mounted
	body := mustMarshal(t, replayRequest{Stage: "projector"})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/replay", bytes.NewReader(body))
	mux.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("mounted POST /admin/replay status = %d, want %d", got, want)
	}

	collectorBody := mustMarshal(t, collectorGenerationReplayRequest{CollectorKind: "git"})
	collectorRecorder := httptest.NewRecorder()
	collectorRequest := httptest.NewRequest(http.MethodPost, "/admin/replay-collector-generations", bytes.NewReader(collectorBody))
	mux.ServeHTTP(collectorRecorder, collectorRequest)

	if got, want := collectorRecorder.Code, http.StatusOK; got != want {
		t.Fatalf("mounted POST /admin/replay-collector-generations status = %d, want %d", got, want)
	}
}

// --- helpers ---

func mustNewRecoveryHandler(t *testing.T, store recovery.ReplayStore) *RecoveryHandler {
	t.Helper()

	recoveryHandler, err := recovery.NewHandler(store)
	if err != nil {
		t.Fatalf("recovery.NewHandler() error = %v", err)
	}

	httpHandler, err := NewRecoveryHandler(recoveryHandler)
	if err != nil {
		t.Fatalf("NewRecoveryHandler() error = %v", err)
	}

	return httpHandler
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	return data
}

func mustUnmarshal(t *testing.T, data []byte, v any) {
	t.Helper()

	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, body = %s", err, string(data))
	}
}

// --- fakes ---

type fakeRecoveryStore struct {
	replayResult                    recovery.ReplayResult
	replayErr                       error
	refinalizeResult                recovery.RefinalizeResult
	refinalizeErr                   error
	collectorGenerationReplayFilter recovery.CollectorGenerationReplayFilter
	collectorGenerationReplayResult recovery.CollectorGenerationReplayResult
	collectorGenerationReplayErr    error
}

func (f *fakeRecoveryStore) ReplayFailedWorkItems(
	_ context.Context,
	_ recovery.ReplayFilter,
	_ time.Time,
) (recovery.ReplayResult, error) {
	return f.replayResult, f.replayErr
}

func (f *fakeRecoveryStore) CountDeadLetterBacklog(
	_ context.Context,
	_ recovery.ReplayFilter,
) (int, error) {
	return 0, nil
}

func (f *fakeRecoveryStore) RefinalizeScopeProjections(
	_ context.Context,
	_ recovery.RefinalizeFilter,
	_ time.Time,
) (recovery.RefinalizeResult, error) {
	return f.refinalizeResult, f.refinalizeErr
}

func (f *fakeRecoveryStore) ReplayCollectorGenerations(
	_ context.Context,
	filter recovery.CollectorGenerationReplayFilter,
	_ time.Time,
) (recovery.CollectorGenerationReplayResult, error) {
	f.collectorGenerationReplayFilter = filter
	return f.collectorGenerationReplayResult, f.collectorGenerationReplayErr
}
