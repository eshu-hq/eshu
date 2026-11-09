// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// commitHandlerTestBundle wires one handler from the test manifest with the
// production claim-plane client against a hit-counting stub plane plus a
// fenced recording committer: the test equivalent of the service wiring
// that pairs a claim-plane client with IngestionStore{ClaimPlane}. The
// constructor refuses anything else, because the scope-binding pin runs
// only on the plane path.
type commitHandlerTestBundle struct {
	handler   *CommitHandler
	server    *httptest.Server
	hits      *commitHandlerPlaneHits
	committer *commitHandlerStubCommitter
	manifest  component.Manifest
	now       time.Time
}

// TestCommitHandlerRequiresFencedCommitter proves the scope-binding pin
// always runs on every allowed wiring: a committer that does not fence
// the claim plane would commit without the commit-time scope match, so
// the constructor refuses it fail-closed even when the store pairing is
// otherwise consistent.
func TestCommitHandlerRequiresFencedCommitter(t *testing.T) {
	t.Parallel()

	_, err := NewCommitHandler(CommitHandlerConfig{
		Manifest:            testManifest(),
		SourceSystem:        "git",
		ScopeKind:           scope.KindRepository,
		CollectorInstanceID: "scorecard-instance",
		ControlStore:        &commitHandlerStubStore{},
		Committer:           &commitHandlerStubCommitter{},
	})
	if err == nil {
		t.Fatal("NewCommitHandler() error = nil, want fenced-committer refusal")
	}
}

func mustNewCommitHandlerTestBundle(t *testing.T) *commitHandlerTestBundle {
	t.Helper()
	now := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	manifest := testManifest()
	hits := &commitHandlerPlaneHits{}
	server := commitHandlerOKPlaneStub(t, hits)
	// Fenced: the stub records the commit instead of pinning; pin behavior
	// is covered in the fence package and the plane's pin SQL.
	committer := &commitHandlerStubCommitter{fenced: true}
	handler, err := NewCommitHandler(CommitHandlerConfig{
		Manifest:            manifest,
		SourceSystem:        "git",
		ScopeKind:           scope.KindRepository,
		CollectorInstanceID: "scorecard-instance",
		ControlStore:        workflow.NewClaimPlaneClient(server.URL, server.Client()),
		Committer:           committer,
		Clock:               func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewCommitHandler() error = %v, want nil", err)
	}
	return &commitHandlerTestBundle{handler: handler, server: server, hits: hits, committer: committer, manifest: manifest, now: now}
}

// commitHandlerPlanned returns the fenced epoch the wire carries beside the
// result: work item, claim, fencing token, owner, and the planned IDs the
// commit pin matched against the plane row.
func commitHandlerPlanned(now time.Time) (workItem workflow.WorkItem, mutation workflow.ClaimMutation) {
	workItem = testWorkItem()
	mutation = workflow.ClaimMutation{
		WorkItemID:    workItem.WorkItemID,
		ClaimID:       workItem.CurrentClaimID,
		FencingToken:  workItem.CurrentFencingToken,
		OwnerID:       workItem.CurrentOwnerID,
		ObservedAt:    now,
		LeaseDuration: time.Minute,
		ScopeID:       workItem.ScopeID,
		SourceRunID:   workItem.SourceRunID,
		GenerationID:  workItem.GenerationID,
	}
	return workItem, mutation
}

// commitHandlerResult returns one complete single-call result echoing the
// planned epoch with no statuses: the status-free shape the commit lane
// records alongside complete-class markers.
func commitHandlerResult(workItem workflow.WorkItem, resultFacts ...sdkcollector.Fact) sdkcollector.Result {
	result := baseResult(workItem, sdkcollector.ResultComplete)
	result.Facts = resultFacts
	result.Statuses = nil
	return result
}

// commitHandlerBody marshals the v0.2.0 wire shape: the embedded mutation,
// the planned IDs, the idempotency key, and the verbatim result.
func commitHandlerBody(t *testing.T, mutation workflow.ClaimMutation, result sdkcollector.Result) []byte {
	t.Helper()
	rawResult, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	wire := workflowv1.CommitClaimFactsRequest{
		ClaimMutation: workflowv1.ClaimMutation{
			WorkItemID:    mutation.WorkItemID,
			ClaimID:       mutation.ClaimID,
			FencingToken:  mutation.FencingToken,
			OwnerID:       mutation.OwnerID,
			ObservedAt:    mutation.ObservedAt,
			LeaseDuration: mutation.LeaseDuration,
		},
		IdempotencyKey: "commit-handler-key-1",
		ResultJSON:     rawResult,
		ScopeID:        mutation.ScopeID,
		SourceRunID:    mutation.SourceRunID,
		GenerationID:   mutation.GenerationID,
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal wire body: %v", err)
	}
	return raw
}

func commitHandlerPost(t *testing.T, handler http.Handler, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, CommitClaimFactsRoutePath(), bytes.NewReader(body))
	request.Header.Set(workflowv1.VersionHeader, "1")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func commitHandlerEnvelopeCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Code string `json:"code"`
		OK   bool   `json:"ok"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response envelope: %v (body %q)", err, recorder.Body.String())
	}
	return envelope.Code
}

// commitHandlerStubStore is a ClaimControlStore stub: the fence probe answers
// with heartbeatErr. Zero heartbeat calls means the handler never reached
// the epoch, and zero CompleteClaim calls means commit stayed separate from
// completion.
type commitHandlerStubStore struct {
	heartbeatErr      error
	heartbeatCalls    int
	heartbeatMutation workflow.ClaimMutation
	completeCalls     int
}

func (s *commitHandlerStubStore) ClaimNextEligible(
	context.Context,
	workflow.ClaimSelector,
	time.Time,
	time.Duration,
) (workflow.WorkItem, workflow.Claim, bool, error) {
	return workflow.WorkItem{}, workflow.Claim{}, false, nil
}

func (s *commitHandlerStubStore) HeartbeatClaim(_ context.Context, mutation workflow.ClaimMutation) error {
	s.heartbeatCalls++
	s.heartbeatMutation = mutation
	return s.heartbeatErr
}

func (s *commitHandlerStubStore) CompleteClaim(_ context.Context, _ workflow.ClaimMutation) error {
	s.completeCalls++
	return nil
}

func (s *commitHandlerStubStore) ReleaseClaim(context.Context, workflow.ClaimMutation) error {
	return nil
}

func (s *commitHandlerStubStore) FailClaimRetryable(context.Context, workflow.ClaimMutation) error {
	return nil
}

func (s *commitHandlerStubStore) FailClaimTerminal(context.Context, workflow.ClaimMutation) error {
	return nil
}

// commitHandlerStubCommitter records claimed commits without a database: the
// production generation-finalized skip (replay idempotency) lives in the
// real boundary, so a retry reaches this stub twice with identical FactIDs.
type commitHandlerStubCommitter struct {
	calls       int
	mutations   []workflow.ClaimMutation
	scopes      []scope.IngestionScope
	generations []scope.ScopeGeneration
	recorded    []facts.Envelope
	commitErr   error
	block       chan struct{}
	// skipReplay mimics the production generation-finalized skip: the
	// replayed commit drains the stream and returns nil without recording,
	// so a retry after an ambiguous ack still answers ok with no duplicates.
	skipReplay bool
	// fenced reports the claim-plane commit fence for
	// validateClaimPlaneFence: plane-held epochs require it, in-tree epochs
	// refuse it. Tests set it to match their control store.
	fenced bool
}

// ClaimPlaneFenced implements collector.ClaimPlaneFencedCommitter.
func (c *commitHandlerStubCommitter) ClaimPlaneFenced() bool { return c.fenced }

func (c *commitHandlerStubCommitter) CommitScopeGeneration(
	context.Context,
	scope.IngestionScope,
	scope.ScopeGeneration,
	<-chan facts.Envelope,
) error {
	return errors.New("generic commit should not be used")
}

func (c *commitHandlerStubCommitter) CommitClaimedScopeGeneration(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	if c.block != nil {
		// A production committer honors the commit deadline; the stub
		// models that so an overrun test fails instead of hanging the
		// suite on a channel no test will ever close in time.
		select {
		case <-c.block:
		case <-ctx.Done():
			for range factStream {
			}
			return ctx.Err()
		}
	}
	c.calls++
	if c.skipReplay {
		for range factStream {
		}
		return nil
	}
	c.mutations = append(c.mutations, mutation)
	c.scopes = append(c.scopes, scopeValue)
	c.generations = append(c.generations, generation)
	for envelope := range factStream {
		c.recorded = append(c.recorded, envelope)
	}
	return c.commitErr
}

// TestCommitHandlerAcksOnlyAfterCommit is the core-handler contract: a valid
// bounded carrier commits through the fenced epoch and only then answers
// {"ok":true}. CompleteClaim stays untouched: commit and complete are
// separate operations under one fencing token.
func TestCommitHandlerAcksOnlyAfterCommit(t *testing.T) {
	t.Parallel()

	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	fact := testSDKFact(workItem)
	result := commitHandlerResult(workItem, fact)
	recorder := commitHandlerPost(t, bundle.handler, commitHandlerBody(t, mutation, result))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", recorder.Code, recorder.Body.String())
	}
	var ack struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &ack); err != nil || !ack.OK {
		t.Fatalf("ack = %q, want {\"ok\":true}", recorder.Body.String())
	}
	if bundle.hits.heartbeat.Load() != 1 {
		t.Fatalf("fence probe calls = %d, want 1", bundle.hits.heartbeat.Load())
	}
	if bundle.committer.calls != 1 {
		t.Fatalf("commit calls = %d, want 1", bundle.committer.calls)
	}
	if len(bundle.committer.recorded) != 1 {
		t.Fatalf("recorded envelopes = %d, want 1", len(bundle.committer.recorded))
	}
	if bundle.hits.complete.Load() != 0 {
		t.Fatalf("CompleteClaim calls = %d, want 0: the handler never completes", bundle.hits.complete.Load())
	}
	if got := bundle.committer.mutations[0]; got.ClaimID != mutation.ClaimID || got.FencingToken != mutation.FencingToken {
		t.Fatalf("commit mutation = %+v, want the fenced epoch", got)
	}
	if got := bundle.committer.mutations[0]; got.ScopeID != mutation.ScopeID || got.GenerationID != mutation.GenerationID {
		t.Fatalf("commit planned IDs = %q/%q, want the wire planned IDs", got.ScopeID, got.GenerationID)
	}
	if len(bundle.committer.scopes) != 1 {
		t.Fatalf("recorded scopes = %d, want 1", len(bundle.committer.scopes))
	}
	if got := bundle.committer.scopes[0]; got.SourceSystem != "git" {
		t.Fatalf("commit scope source system = %q, want the configured binding", got.SourceSystem)
	}
	if got := bundle.committer.scopes[0]; got.PartitionKey != "git:repo-eshu" {
		t.Fatalf("commit scope partition = %q, want the fallback shape", got.PartitionKey)
	}
}

// TestCommitHandlerCommitterErrorIsNoAck proves {"ok":true} never precedes
// durability: a refused commit answers 409 with the fence envelope, never an
// acknowledgement.
func TestCommitHandlerCommitterErrorIsNoAck(t *testing.T) {
	t.Parallel()

	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	bundle.committer.commitErr = workflow.ErrClaimRejected
	recorder := commitHandlerPost(t, bundle.handler, commitHandlerBody(t, mutation, commitHandlerResult(workItem, testSDKFact(workItem))))

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %q)", recorder.Code, recorder.Body.String())
	}
	if code := commitHandlerEnvelopeCode(t, recorder); code != workflowv1.CodeStaleFence {
		t.Fatalf("envelope code = %q, want stale_fence", code)
	}
}

// TestCommitHandlerRejectsVersionMismatch proves the version gate runs before
// any byte is interpreted: a wrong protocol major fails closed without a
// fence probe or a commit.
func TestCommitHandlerRejectsVersionMismatch(t *testing.T) {
	t.Parallel()

	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	request := httptest.NewRequest(http.MethodPost, "/workflow.v1/CommitClaimFacts",
		bytes.NewReader(commitHandlerBody(t, mutation, commitHandlerResult(workItem, testSDKFact(workItem)))))
	request.Header.Set(workflowv1.VersionHeader, "999")
	recorder := httptest.NewRecorder()
	bundle.handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %q)", recorder.Code, recorder.Body.String())
	}
	if code := commitHandlerEnvelopeCode(t, recorder); code != workflowv1.CodeVersionMismatch {
		t.Fatalf("envelope code = %q, want version_mismatch", code)
	}
	if bundle.hits.heartbeat.Load() != 0 || bundle.committer.calls != 0 {
		t.Fatalf("probe calls = %d, commit calls = %d, want 0/0", bundle.hits.heartbeat.Load(), bundle.committer.calls)
	}
}

// TestCommitHandlerRejectsTrailingEnvelopeData proves the envelope decode
// is strict end to end: bytes after the JSON document fail closed before
// admission, probe, or commit ever see the carrier.
func TestCommitHandlerRejectsTrailingEnvelopeData(t *testing.T) {
	t.Parallel()

	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	body := append(commitHandlerBody(t, mutation, commitHandlerResult(workItem, testSDKFact(workItem))), '{', '}')
	recorder := commitHandlerPost(t, bundle.handler, body)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %q)", recorder.Code, recorder.Body.String())
	}
	if code := commitHandlerEnvelopeCode(t, recorder); code != workflowv1.CodeInvalid {
		t.Fatalf("envelope code = %q, want invalid", code)
	}
	if bundle.hits.heartbeat.Load() != 0 || bundle.committer.calls != 0 {
		t.Fatalf("probe calls = %d, commit calls = %d, want 0/0", bundle.hits.heartbeat.Load(), bundle.committer.calls)
	}
}

// TestCommitHandlerRejectsStatuses proves the single-call lane disposition:
// a result carrying signaling statuses the lane cannot record is
// terminal-invalid before the fence probe, exactly like the in-tree path.
// A complete-class marker without the partial flag commits instead: it
// carries no signal the lane would drop.
func TestCommitHandlerRejectsStatuses(t *testing.T) {
	t.Parallel()

	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	result := baseResult(workItem, sdkcollector.ResultComplete)
	result.Facts = []sdkcollector.Fact{testSDKFact(workItem)}
	result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusWarning, WarningCount: 1}}
	recorder := commitHandlerPost(t, bundle.handler, commitHandlerBody(t, mutation, result))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %q)", recorder.Code, recorder.Body.String())
	}
	if code := commitHandlerEnvelopeCode(t, recorder); code != workflowv1.CodeInvalid {
		t.Fatalf("envelope code = %q, want invalid", code)
	}
	if bundle.hits.heartbeat.Load() != 0 || bundle.committer.calls != 0 {
		t.Fatalf("probe calls = %d, commit calls = %d, want 0/0", bundle.hits.heartbeat.Load(), bundle.committer.calls)
	}
}
