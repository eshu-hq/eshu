// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"encoding/json"
	"net/http"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"
)

// TestCommitHandlerRejectsUnknownKind proves the per-collector-kind builder
// fails closed: a result for a kind the manifest never declared commits
// nothing and never probes the epoch.
func TestCommitHandlerRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	fact := testSDKFact(workItem)
	fact.Kind = "undeclared-kind"
	recorder := commitHandlerPost(t, bundle.handler, commitHandlerBody(t, mutation, commitHandlerResult(workItem, fact)))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %q)", recorder.Code, recorder.Body.String())
	}
	if code := commitHandlerEnvelopeCode(t, recorder); code != workflowv1.CodeInvalid {
		t.Fatalf("envelope code = %q, want invalid", code)
	}
	if bundle.store.heartbeatCalls != 0 || bundle.committer.calls != 0 {
		t.Fatalf("probe calls = %d, commit calls = %d, want 0/0", bundle.store.heartbeatCalls, bundle.committer.calls)
	}
}

// TestCommitHandlerRejectsUndeclaredEchoKind proves the per-kind gate in
// the identity match: an echo naming a collector kind the manifest never
// declared fails closed even when every fact kind is valid.
func TestCommitHandlerRejectsUndeclaredEchoKind(t *testing.T) {
	t.Parallel()

	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	result := commitHandlerResult(workItem, testSDKFact(workItem))
	result.Claim.CollectorKind = "undeclared-kind"
	recorder := commitHandlerPost(t, bundle.handler, commitHandlerBody(t, mutation, result))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %q)", recorder.Code, recorder.Body.String())
	}
	if code := commitHandlerEnvelopeCode(t, recorder); code != workflowv1.CodeInvalid {
		t.Fatalf("envelope code = %q, want invalid", code)
	}
	if bundle.store.heartbeatCalls != 0 || bundle.committer.calls != 0 {
		t.Fatalf("probe calls = %d, commit calls = %d, want 0/0", bundle.store.heartbeatCalls, bundle.committer.calls)
	}
}

// TestCommitHandlerRejectsLaneMismatchedEcho proves the shared carrier
// check fires arm by arm: the lane's decode-and-identity implementation
// (collector.CheckCommitClaimCarrier) rejects an echo from another epoch or
// producer before the handler's own admission runs.
func TestCommitHandlerRejectsLaneMismatchedEcho(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*sdkcollector.Result){
		"workItem":     func(r *sdkcollector.Result) { r.Claim.WorkItemID = "other-work-item" },
		"generation":   func(r *sdkcollector.Result) { r.Generation.ID = "other-generation" },
		"fencingToken": func(r *sdkcollector.Result) { r.Claim.FencingToken = "999" },
		"component":    func(r *sdkcollector.Result) { r.Claim.ComponentID = "other-component" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bundle := mustNewCommitHandlerTestBundle(t)
			workItem, mutation := commitHandlerPlanned(bundle.now)
			result := commitHandlerResult(workItem, testSDKFact(workItem))
			mutate(&result)
			recorder := commitHandlerPost(t, bundle.handler, commitHandlerBody(t, mutation, result))

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %q)", recorder.Code, recorder.Body.String())
			}
			if code := commitHandlerEnvelopeCode(t, recorder); code != workflowv1.CodeInvalid {
				t.Fatalf("envelope code = %q, want invalid", code)
			}
			if bundle.store.heartbeatCalls != 0 || bundle.committer.calls != 0 {
				t.Fatalf("probe calls = %d, commit calls = %d, want 0/0", bundle.store.heartbeatCalls, bundle.committer.calls)
			}
		})
	}
}

// TestCommitHandlerRejectsExtendedMismatch proves the handler's second,
// independent identity check: source run, scope, and instance have no lane
// equivalent, so only the extended match can reject an echo that names the
// right epoch but the wrong planned target or producer. Removing one of
// these arms must turn its row red.
func TestCommitHandlerRejectsExtendedMismatch(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*sdkcollector.Result){
		"sourceRun": func(r *sdkcollector.Result) { r.Claim.SourceRunID = "other-source-run" },
		"scope":     func(r *sdkcollector.Result) { r.Claim.Scope.ID = "other-scope" },
		"instance":  func(r *sdkcollector.Result) { r.Claim.InstanceID = "other-instance" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bundle := mustNewCommitHandlerTestBundle(t)
			workItem, mutation := commitHandlerPlanned(bundle.now)
			result := commitHandlerResult(workItem, testSDKFact(workItem))
			mutate(&result)
			recorder := commitHandlerPost(t, bundle.handler, commitHandlerBody(t, mutation, result))

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %q)", recorder.Code, recorder.Body.String())
			}
			if code := commitHandlerEnvelopeCode(t, recorder); code != workflowv1.CodeInvalid {
				t.Fatalf("envelope code = %q, want invalid", code)
			}
			if bundle.store.heartbeatCalls != 0 || bundle.committer.calls != 0 {
				t.Fatalf("probe calls = %d, commit calls = %d, want 0/0", bundle.store.heartbeatCalls, bundle.committer.calls)
			}
		})
	}
}

// TestCommitHandlerRejectsUnknownEnvelopeField proves the proxy never
// smuggles interpretation past core: a field the protocol does not define
// fails closed even though the proxy measured the same body as fine.
func TestCommitHandlerRejectsUnknownEnvelopeField(t *testing.T) {
	t.Parallel()

	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	body := commitHandlerBody(t, mutation, commitHandlerResult(workItem, testSDKFact(workItem)))
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("decode wire body: %v", err)
	}
	wire["smuggled"] = "nope"
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("re-marshal wire body: %v", err)
	}
	recorder := commitHandlerPost(t, bundle.handler, raw)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %q)", recorder.Code, recorder.Body.String())
	}
	if code := commitHandlerEnvelopeCode(t, recorder); code != workflowv1.CodeInvalid {
		t.Fatalf("envelope code = %q, want invalid", code)
	}
	if bundle.store.heartbeatCalls != 0 || bundle.committer.calls != 0 {
		t.Fatalf("probe calls = %d, commit calls = %d, want 0/0", bundle.store.heartbeatCalls, bundle.committer.calls)
	}
}
