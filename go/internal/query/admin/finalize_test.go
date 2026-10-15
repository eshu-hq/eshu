// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// finalizeLedger is an idempotency ledger double for the three
// ledger-backed admin mutations. The mutation step cancels the request
// context (the client leaving after the mutation committed), and the
// finalize records the context it ran on. The first claim wins; a later
// claim sees the recorded outcome, as the Postgres ledger does.
type finalizeLedger struct {
	*stubAdminStore
	cancelRequest  context.CancelFunc
	finalizeErr    error
	blockFinalize  bool
	finalizeCalls  int
	finalizeCtxErr error
	finalizeDL     time.Time
	finalizeHasDL  bool
	finalizeSpan   trace.SpanContext
	fingerprint    string
	claimed        bool
	completed      bool
	recordedCount  int
	recordedIDs    []string
}

// finalizeSafetyWindow caps how long a blocked finalize double waits when the
// handler did not bound it.
const finalizeSafetyWindow = 3 * time.Second

func newFinalizeLedger() *finalizeLedger {
	return &finalizeLedger{stubAdminStore: &stubAdminStore{}, cancelRequest: func() {}}
}

func (l *finalizeLedger) ClaimReplayIdempotency(_ context.Context, _, fingerprint string, _ time.Time) (ReplayIdempotencyClaim, error) {
	if !l.claimed {
		l.claimed, l.fingerprint = true, fingerprint
		return ReplayIdempotencyClaim{Claimed: true}, nil
	}
	status := ReplayRequestStatusInProgress
	if l.completed {
		status = ReplayRequestStatusCompleted
	}
	return ReplayIdempotencyClaim{Status: status, Fingerprint: l.fingerprint, ReplayedCount: l.recordedCount, WorkItemIDs: l.recordedIDs}, nil
}

func (l *finalizeLedger) ReplayFailedWorkItems(context.Context, ReplayWorkItemFilter) ([]WorkItem, error) {
	l.cancelRequest()
	return []WorkItem{{WorkItemID: "wi-1"}}, nil
}

func (l *finalizeLedger) ReopenCompletedWork(context.Context, ReopenFilter) (ReopenResult, error) {
	l.cancelRequest()
	return ReopenResult{GenerationID: "gen-1", ReducerWorkItemIDs: []string{"wi-1"}}, nil
}

func (l *finalizeLedger) CompleteReplayIdempotency(ctx context.Context, _ string, count int, ids []string, _ time.Time) error {
	l.finalizeCalls++
	l.finalizeCtxErr = ctx.Err()
	l.finalizeDL, l.finalizeHasDL = ctx.Deadline()
	l.finalizeSpan = trace.SpanFromContext(ctx).SpanContext()
	if l.blockFinalize {
		// A finalize the handler failed to bound returns after a safety
		// window, so the test fails on elapsed time instead of hanging.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(finalizeSafetyWindow):
			return errors.New("finalize was not bounded")
		}
	}
	if l.finalizeErr != nil {
		return l.finalizeErr
	}
	l.completed, l.recordedCount, l.recordedIDs = true, count, ids
	return nil
}

// cancelingRecovery re-enqueues one scope and then cancels the request
// context, like a client leaving after the refinalize transaction committed.
type cancelingRecovery struct {
	stubRecoveryHandler
	ledger *finalizeLedger
}

func (c *cancelingRecovery) Refinalize(context.Context, recovery.RefinalizeFilter) (recovery.RefinalizeResult, error) {
	c.ledger.cancelRequest()
	return recovery.RefinalizeResult{Enqueued: 1, ScopeIDs: []string{"scope-a"}}, nil
}

// recordingAudit records each appended event's reason code and whether the
// append context was still live.
type recordingAudit struct {
	reasons []string
	ctxErrs []error
}

func (a *recordingAudit) Append(ctx context.Context, events []governanceaudit.Event) error {
	for _, event := range events {
		a.reasons = append(a.reasons, event.ReasonCode)
		a.ctxErrs = append(a.ctxErrs, ctx.Err())
	}
	return nil
}

// finalizeRoute is one ledger-backed admin mutation.
type finalizeRoute struct {
	name           string
	path           string
	body           string
	countKey       string
	recordMessage  string
	acceptedReason string
	handler        func(l *finalizeLedger, a *recordingAudit) *Handler
}

func finalizeRoutes() []finalizeRoute {
	return []finalizeRoute{
		{
			name: "replay", path: "/api/v0/admin/replay", body: failureReplayBody,
			countKey: "replayed_count", recordMessage: replayRecordFailedMessage, acceptedReason: "replay_accepted",
			handler: func(l *finalizeLedger, a *recordingAudit) *Handler { return &Handler{Store: l, Audit: a} },
		},
		{
			name: "reopen", path: "/api/v0/admin/reopen", body: failureReopenBody,
			countKey: "reopened_total_count", recordMessage: reopenRecordFailedMessage, acceptedReason: "reopen_accepted",
			handler: func(l *finalizeLedger, a *recordingAudit) *Handler { return &Handler{Store: l, Audit: a} },
		},
		{
			name: "recover generations", path: "/api/v0/admin/recover-generations", body: failureRecoverBody,
			countKey: "enqueued", recordMessage: recoverGenerationsRecordFailedMessage, acceptedReason: "recover_generations_accepted",
			handler: func(l *finalizeLedger, a *recordingAudit) *Handler {
				return &Handler{Store: l, Audit: a, Recovery: &cancelingRecovery{ledger: l}}
			},
		},
	}
}

// serveFinalize serves one POST under a recording request span whose context
// the ledger's mutation step cancels when cancelOnMutate is set. It returns
// the response, the ended spans, and the request span's context.
func serveFinalize(
	t *testing.T, h *Handler, l *finalizeLedger, route finalizeRoute, cancelOnMutate bool,
) (*httptest.ResponseRecorder, []sdktrace.ReadOnlySpan, trace.SpanContext) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, parent := provider.Tracer("admin-finalize-test").Start(context.Background(), "request")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	l.cancelRequest = func() {}
	if cancelOnMutate {
		l.cancelRequest = cancel
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, route.path, strings.NewReader(route.body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-ID", "corr-finalize")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	parent.End()
	return rec, recorder.Ended(), parent.SpanContext()
}

// TestAdminMutationFinalizeOutlivesClientCancel pins the ruling for #7674:
// once the mutation committed, the idempotency finalize and the accepted
// audit append run on a context detached from the request's cancel and
// bounded by the finalize timeout, still under the request span. The client
// leaving answers 200 with the recorded outcome, logs one INFO line, and a
// retry with the same key returns the prior outcome. It swaps the default
// slog logger, so it must not be parallel.
func TestAdminMutationFinalizeOutlivesClientCancel(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	for _, route := range finalizeRoutes() {
		t.Run(route.name, func(t *testing.T) {
			logs.Reset()
			ledger, auditLog := newFinalizeLedger(), &recordingAudit{}
			h := route.handler(ledger, auditLog)
			started := time.Now()
			rec, _, requestSpan := serveFinalize(t, h, ledger, route, true)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
			}
			body := decodeFinalizeBody(t, rec)
			if body["duplicate"] != false || body[route.countKey] != float64(1) {
				t.Fatalf("body = %v, want duplicate=false and %s=1", body, route.countKey)
			}
			if ledger.finalizeCalls != 1 || ledger.finalizeCtxErr != nil {
				t.Fatalf("finalize calls = %d, ctx err = %v; want 1 call on a live context", ledger.finalizeCalls, ledger.finalizeCtxErr)
			}
			if !ledger.finalizeHasDL || ledger.finalizeDL.After(started.Add(defaultFinalizeTimeout+time.Second)) {
				t.Fatalf("finalize deadline = %v (set %v), want one within %v of the request", ledger.finalizeDL, ledger.finalizeHasDL, defaultFinalizeTimeout)
			}
			if !ledger.finalizeSpan.Equal(requestSpan) {
				t.Fatalf("finalize span = %v, want the request span %v", ledger.finalizeSpan, requestSpan)
			}
			assertAcceptedAuditOnLiveContext(t, auditLog, route.acceptedReason)
			assertFinalizeAfterCancelLog(t, logs.String(), route.path)

			// A retry with the same key returns the recorded outcome.
			retry, _, _ := serveFinalize(t, h, ledger, route, false)
			retryBody := decodeFinalizeBody(t, retry)
			if retry.Code != http.StatusOK || retryBody["duplicate"] != true || retryBody[route.countKey] != float64(1) {
				t.Fatalf("retry = %d %v, want 200 duplicate=true with %s=1", retry.Code, retryBody, route.countKey)
			}
		})
	}
}

// TestAdminMutationFinalizeStoreErrorIsServerFault proves a finalize store
// failure after the client left is a server fault, not a client cancel: the
// failure is classified on the detached context, which is live, so the
// answer is 500 with the fixed record message, an Error span, and one
// exception event. That holds even when the store error itself wraps
// context.Canceled (an inner cancel unrelated to the gone client).
func TestAdminMutationFinalizeStoreErrorIsServerFault(t *testing.T) {
	t.Parallel()
	storeErrors := map[string]error{
		"store error":          fmt.Errorf("%s: %w", adminFailureCanary, errors.New("pg")),
		"inner canceled error": fmt.Errorf("%s: %w", adminFailureCanary, context.Canceled),
	}
	for _, route := range finalizeRoutes() {
		for name, storeErr := range storeErrors {
			t.Run(route.name+"/"+name, func(t *testing.T) {
				t.Parallel()
				ledger := newFinalizeLedger()
				ledger.finalizeErr = storeErr
				rec, ended, _ := serveFinalize(t, route.handler(ledger, &recordingAudit{}), ledger, route, true)
				assertAdminFailure(t, adminFailureStep{message: route.recordMessage}, adminFailureBackend, rec, ended)
			})
		}
	}
}

// TestAdminMutationFinalizeTimeoutIsBounded proves a hung finalize after the
// client left stops at Handler.FinalizeTimeout and answers 500 with the
// record message: its deadline is not a client cancel.
func TestAdminMutationFinalizeTimeoutIsBounded(t *testing.T) {
	t.Parallel()
	const bound = 50 * time.Millisecond
	for _, route := range finalizeRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			ledger := newFinalizeLedger()
			ledger.blockFinalize = true
			h := route.handler(ledger, &recordingAudit{})
			h.FinalizeTimeout = bound
			started := time.Now()
			rec, ended, _ := serveFinalize(t, h, ledger, route, true)
			if elapsed := time.Since(started); elapsed >= finalizeSafetyWindow {
				t.Fatalf("handler took %v, want it bounded near %v", elapsed, bound)
			}
			if ledger.finalizeCtxErr != nil {
				t.Fatalf("finalize started on a done context: %v", ledger.finalizeCtxErr)
			}
			assertAdminFailure(t, adminFailureStep{message: route.recordMessage}, adminFailureBackend, rec, ended)
		})
	}
}

func decodeFinalizeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body = %s", err, rec.Body.String())
	}
	return body
}

func assertAcceptedAuditOnLiveContext(t *testing.T, a *recordingAudit, reason string) {
	t.Helper()
	for i, got := range a.reasons {
		if got == reason {
			if a.ctxErrs[i] != nil {
				t.Fatalf("audit %q appended on a done context: %v", reason, a.ctxErrs[i])
			}
			return
		}
	}
	t.Fatalf("audit reasons = %v, want %q", a.reasons, reason)
}

func assertFinalizeAfterCancelLog(t *testing.T, logs, route string) {
	t.Helper()
	found := 0
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil || entry["event_name"] != finalizeAfterClientCancelEvent {
			continue
		}
		found++
		if entry["level"] != "INFO" || entry["route"] != route || entry["correlation_id"] != "corr-finalize" {
			t.Fatalf("finalize log = %v, want INFO with route %q and correlation_id", entry, route)
		}
	}
	if found != 1 {
		t.Fatalf("found %d %s log lines, want 1; logs = %s", found, finalizeAfterClientCancelEvent, logs)
	}
}
