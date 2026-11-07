// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// commitHandlerPlaneHits counts the plane operations a test's handler
// drove: heartbeat probes and completions. Counters are atomic because
// the stub server answers on its own goroutines.
type commitHandlerPlaneHits struct {
	heartbeat atomic.Int64
	complete  atomic.Int64
}

// commitHandlerOKPlaneStub serves the plane operations the handler's fence
// probe and completion use: heartbeat and complete answer {"ok":true} while
// counting hits, so tests assert probe-before-commit ordering through the
// production ClaimPlaneClient instead of a stub copy. Any other path
// answers 404: the handler must never call it.
func commitHandlerOKPlaneStub(t *testing.T, hits *commitHandlerPlaneHits) *httptest.Server {
	t.Helper()
	answer := func(hit *atomic.Int64) http.HandlerFunc {
		return func(writer http.ResponseWriter, _ *http.Request) {
			hit.Add(1)
			writer.Header().Set("Content-Type", "application/json")
			writer.Header().Set(workflowv1.VersionHeader, "1")
			_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true})
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/workflow.v1/HeartbeatClaim", answer(&hits.heartbeat))
	mux.HandleFunc("/workflow.v1/CompleteClaim", answer(&hits.complete))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// commitHandlerPlaneStub serves one coded HeartbeatClaim refusal: the fence
// the plane holds, with the production client classifying the envelope. The
// handler must pass the code and the 409 status through verbatim so the
// proxy can copy them back end to end.
func commitHandlerPlaneStub(t *testing.T, code string) *httptest.Server {
	t.Helper()
	status := http.StatusConflict
	if code == workflowv1.CodeLeaseExpired {
		status = http.StatusConflict
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/workflow.v1/HeartbeatClaim", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set(workflowv1.VersionHeader, "1")
		writer.WriteHeader(status)
		_ = json.NewEncoder(writer).Encode(map[string]any{"code": code, "message": "fenced away"})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// TestCommitHandlerFenceEqualityTable proves proof item 2 against the
// execCompleteClaimMutation guard family: every fenced-away epoch — stale
// token, wrong owner, expired lease, already-completed claim, revoked grant
// — commits zero facts and answers the verbatim 409 envelope through the
// handler. The plane-coded rows run through the production ClaimPlaneClient
// so the code classification is the real one, not a stub copy.
func TestCommitHandlerFenceEqualityTable(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	manifest, grants := grantedCoreKindManifest()
	manifest.Spec.CollectorKinds = []string{"repo"}

	planeCases := []struct {
		name string
		code string
	}{
		{"staleToken", workflowv1.CodeStaleFence},
		{"wrongOwner", workflowv1.CodeStaleFence},
		{"expiredLease", workflowv1.CodeLeaseExpired},
		{"completedClaim", workflowv1.CodeClaimConflict},
	}
	// fencedResult strips the SDK complete-status row the lane cannot
	// record: the table probes the epoch fence, not the status
	// disposition (pinned separately by
	// TestCommitHandlerRejectsStatuses).
	fencedResult := func(workItem workflow.WorkItem) sdkcollector.Result {
		result := grantedCoreKindResult(workItem)
		result.Statuses = nil
		return result
	}
	for _, tc := range planeCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commitHandlerPlaneStub(t, tc.code)
			// The epoch is plane-held, so the committer must report the
			// claim-plane fence or the lane refuses the mismatched pair.
			committer := &commitHandlerStubCommitter{fenced: true}
			handler, err := NewCommitHandler(CommitHandlerConfig{
				Manifest:            manifest,
				Grants:              grants,
				SourceSystem:        "git",
				ScopeKind:           scope.KindRepository,
				CollectorInstanceID: "scorecard-instance",
				ControlStore:        workflow.NewClaimPlaneClient(server.URL, server.Client()),
				Committer:           committer,
			})
			if err != nil {
				t.Fatalf("NewCommitHandler() error = %v, want nil", err)
			}
			workItem, mutation := commitHandlerPlanned(now)
			result := fencedResult(workItem)
			recorder := commitHandlerPost(t, handler, commitHandlerBody(t, mutation, result))

			if recorder.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409 (body %q)", recorder.Code, recorder.Body.String())
			}
			if code := commitHandlerEnvelopeCode(t, recorder); code != tc.code {
				t.Fatalf("envelope code = %q, want the verbatim plane code %q", code, tc.code)
			}
			if committer.calls != 0 || len(committer.recorded) != 0 {
				t.Fatalf("commit calls = %d, recorded = %d, want 0/0: a fenced-away epoch commits nothing",
					committer.calls, len(committer.recorded))
			}
		})
	}

	t.Run("revokedGrant", func(t *testing.T) {
		t.Parallel()
		revoked := grants[0]
		revoked.Revoked = true
		live := []component.ProducerGrant{revoked}
		store := &commitHandlerStubStore{}
		committer := &commitHandlerStubCommitter{fenced: true}
		handler, err := NewCommitHandler(CommitHandlerConfig{
			Manifest:            manifest,
			Grants:              grants,
			LiveGrants:          func() ([]component.ProducerGrant, error) { return live, nil },
			SourceSystem:        "git",
			ScopeKind:           scope.KindRepository,
			CollectorInstanceID: "scorecard-instance",
			ControlStore:        store,
			Committer:           committer,
		})
		if err != nil {
			t.Fatalf("NewCommitHandler() error = %v, want nil", err)
		}
		workItem, mutation := commitHandlerPlanned(now)
		result := fencedResult(workItem)
		recorder := commitHandlerPost(t, handler, commitHandlerBody(t, mutation, result))

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", recorder.Code, recorder.Body.String())
		}
		if code := commitHandlerEnvelopeCode(t, recorder); code != workflowv1.CodeInvalid {
			t.Fatalf("envelope code = %q, want invalid", code)
		}
		if store.heartbeatCalls != 0 || committer.calls != 0 {
			t.Fatalf("probe calls = %d, commit calls = %d, want 0/0: a revoked grant never reaches the epoch",
				store.heartbeatCalls, committer.calls)
		}
	})
}

// TestCommitHandlerOrderingAfterAckBeforeComplete proves proof item 3: after
// the commit ack, the facts are durable in the committer before CompleteClaim
// ever runs. A crash between ack and complete retries the same idempotency
// key; the replay commits the identical FactID set (production upsert, not
// duplicates) and complete then succeeds on the untouched store.
func TestCommitHandlerOrderingAfterAckBeforeComplete(t *testing.T) {
	t.Parallel()

	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	body := commitHandlerBody(t, mutation, commitHandlerResult(workItem, testSDKFact(workItem)))

	first := commitHandlerPost(t, bundle.handler, body)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200 (body %q)", first.Code, first.Body.String())
	}
	if len(bundle.committer.recorded) != 1 {
		t.Fatalf("recorded envelopes = %d, want 1: facts visible before CompleteClaim", len(bundle.committer.recorded))
	}
	if bundle.hits.complete.Load() != 0 {
		t.Fatalf("CompleteClaim calls = %d, want 0: the handler never completes", bundle.hits.complete.Load())
	}
	firstIDs := map[string]struct{}{bundle.committer.recorded[0].FactID: {}}

	// Crash between ack and complete: retry the identical carrier.
	second := commitHandlerPost(t, bundle.handler, body)
	if second.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200 (body %q)", second.Code, second.Body.String())
	}
	if len(bundle.committer.recorded) != 2 {
		t.Fatalf("recorded envelopes = %d, want 2 (one per attempt)", len(bundle.committer.recorded))
	}
	for _, envelope := range bundle.committer.recorded[1:] {
		if _, ok := firstIDs[envelope.FactID]; !ok {
			t.Fatalf("retry FactID = %q, want the original set: a replay must upsert, never duplicate", envelope.FactID)
		}
	}

	// Complete then succeeds on the same fencing token.
	plane := workflow.NewClaimPlaneClient(bundle.server.URL, bundle.server.Client())
	if err := plane.CompleteClaim(context.Background(), mutation); err != nil {
		t.Fatalf("CompleteClaim() error = %v, want nil", err)
	}
	if bundle.hits.complete.Load() != 1 {
		t.Fatalf("CompleteClaim calls = %d, want 1", bundle.hits.complete.Load())
	}
}

// TestCommitHandlerAmbiguousAckRetryIsIdempotent proves proof item 4: the
// proxy path dies after core COMMITs (response never reaches the caller);
// the retry with the same key answers ok with no duplicate facts. The
// generation-finalized skip turns the replayed commit into a nil no-op in
// production; the stub mimics it by recording nothing on the replay while
// the handler still answers ok.
func TestCommitHandlerAmbiguousAckRetryIsIdempotent(t *testing.T) {
	t.Parallel()

	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	body := commitHandlerBody(t, mutation, commitHandlerResult(workItem, testSDKFact(workItem)))

	first := commitHandlerPost(t, bundle.handler, body)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200 (body %q)", first.Code, first.Body.String())
	}
	if len(bundle.committer.recorded) != 1 {
		t.Fatalf("recorded envelopes = %d, want 1", len(bundle.committer.recorded))
	}

	// Ambiguous ack: COMMIT landed, the answer was lost. The replayed commit
	// is a generation-finalized no-op that still acks.
	bundle.committer.skipReplay = true
	second := commitHandlerPost(t, bundle.handler, body)
	if second.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200 (body %q)", second.Code, second.Body.String())
	}
	var ack struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &ack); err != nil || !ack.OK {
		t.Fatalf("retry ack = %q, want {\"ok\":true}", second.Body.String())
	}
	if len(bundle.committer.recorded) != 1 {
		t.Fatalf("recorded envelopes = %d, want 1: the replay must not duplicate facts", len(bundle.committer.recorded))
	}
}
