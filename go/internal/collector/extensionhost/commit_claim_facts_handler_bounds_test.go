// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestCommitHandlerResultCapBoundary proves the shared 4 MiB arithmetic at
// the core boundary: exactly MaxCommitResultBytes passes, one byte more is
// terminal-invalid with zero forwards. The proxy enforces the same bound
// without trusting it, so both ends cite workflowv1.
func TestCommitHandlerResultCapBoundary(t *testing.T) {
	t.Parallel()

	if workflowv1.MaxCommitResultBytes != workflow.MaxCommitClaimFactsBytes {
		t.Fatalf("workflowv1.MaxCommitResultBytes = %d, want the internal lane cap %d: one arithmetic source",
			workflowv1.MaxCommitResultBytes, workflow.MaxCommitClaimFactsBytes)
	}
	if workflowv1.MaxCommitBodyBytes != workflow.MaxCommitClaimFactsBodyBytes {
		t.Fatalf("workflowv1.MaxCommitBodyBytes = %d, want the internal lane cap %d: one arithmetic source",
			workflowv1.MaxCommitBodyBytes, workflow.MaxCommitClaimFactsBodyBytes)
	}

	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	padTo := func(size int) sdkcollector.Result {
		probe := testSDKFact(workItem)
		probe.Payload = map[string]any{"padding": ""}
		rawProbe, err := json.Marshal(commitHandlerResult(workItem, probe))
		if err != nil {
			t.Fatalf("marshal probe result: %v", err)
		}
		if len(rawProbe) > size {
			t.Fatalf("probe result is %d bytes, over the %d-byte probe size", len(rawProbe), size)
		}
		// The padded marshal is the probe framing plus exactly the padding
		// bytes, so one shot lands on the size with no iteration.
		fact := testSDKFact(workItem)
		fact.Payload = map[string]any{"padding": strings.Repeat("p", size-len(rawProbe))}
		return commitHandlerResult(workItem, fact)
	}

	exact := padTo(workflowv1.MaxCommitResultBytes)
	rawExact, err := json.Marshal(exact)
	if err != nil {
		t.Fatalf("marshal exact result: %v", err)
	}
	if len(rawExact) != workflowv1.MaxCommitResultBytes {
		t.Fatalf("exact result is %d bytes, want exactly %d", len(rawExact), workflowv1.MaxCommitResultBytes)
	}
	if recorder := commitHandlerPost(t, bundle.handler, commitHandlerBody(t, mutation, exact)); recorder.Code != http.StatusOK {
		t.Fatalf("exact-cap status = %d, want 200 (body %q)", recorder.Code, recorder.Body.String())
	}

	over := padTo(workflowv1.MaxCommitResultBytes + 1)
	before := bundle.committer.calls
	if recorder := commitHandlerPost(t, bundle.handler, commitHandlerBody(t, mutation, over)); recorder.Code != http.StatusBadRequest {
		t.Fatalf("over-cap status = %d, want 400 (body %q)", recorder.Code, recorder.Body.String())
	} else if code := commitHandlerEnvelopeCode(t, recorder); code != workflowv1.CodeInvalid {
		t.Fatalf("over-cap envelope code = %q, want invalid", code)
	}
	if bundle.committer.calls != before {
		t.Fatalf("commit calls grew on an over-cap carrier: the lane must fail before the boundary")
	}
}

// TestCommitHandlerRejectsOversizeBody proves the whole-body cap: an envelope
// past MaxCommitBodyBytes is refused before decode, admission, probe, or
// commit ever see it.
func TestCommitHandlerRejectsOversizeBody(t *testing.T) {
	t.Parallel()

	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	body := commitHandlerBody(t, mutation, commitHandlerResult(workItem, testSDKFact(workItem)))
	if len(body) >= workflowv1.MaxCommitBodyBytes {
		t.Fatalf("base body is %d bytes, expected well under the %d-byte cap", len(body), workflowv1.MaxCommitBodyBytes)
	}
	oversize := append(body, make([]byte, workflowv1.MaxCommitBodyBytes-len(body)+1)...)
	recorder := commitHandlerPost(t, bundle.handler, oversize)

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

// TestCommitHandlerRequiresPlannedIDs proves core never scrapes its target
// out of the result echo: a carrier missing the planned IDs beside the
// result is invalid even when the echo names them.
func TestCommitHandlerRequiresPlannedIDs(t *testing.T) {
	t.Parallel()

	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	body := commitHandlerBody(t, mutation, commitHandlerResult(workItem, testSDKFact(workItem)))
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("decode wire body: %v", err)
	}
	delete(wire, "scope_id")
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("re-marshal wire body: %v", err)
	}
	recorder := commitHandlerPost(t, bundle.handler, raw)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %q)", recorder.Code, recorder.Body.String())
	}
	if bundle.hits.heartbeat.Load() != 0 || bundle.committer.calls != 0 {
		t.Fatalf("probe calls = %d, commit calls = %d, want 0/0", bundle.hits.heartbeat.Load(), bundle.committer.calls)
	}
}

// TestCommitHandlerScopeMirrorsInTreeMapping proves the per-kind builder
// matches what the in-tree path would commit for the same item and result:
// same scope identity, same generation identity, same partition fallback.
// A divergence here would fork graph truth between the cutover and in-tree
// paths, so the test builds both and compares field by field.
func TestCommitHandlerScopeMirrorsInTreeMapping(t *testing.T) {
	t.Parallel()

	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	_ = mutation
	// The wire carries no fairness key, so core always builds the
	// source-system:scope-id fallback partition. Prove the builders agree
	// on exactly that shape by clearing the item's fairness key: with a
	// divergent key the in-tree partition would differ by design.
	workItem.FairnessKey = ""
	fact := testSDKFact(workItem)
	result := commitHandlerResult(workItem, fact)

	source, err := NewSource(Config{
		Manifest:            bundle.manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Runner:              &commitReplayRunner{result: result},
		Clock:               func() time.Time { return bundle.now },
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	envelopes := source.envelopesForResult(workItem, result)
	collected := source.collectedGeneration(workItem, result, envelopes, "")

	builtScope, builtGeneration, err := bundle.handler.buildTarget(result)
	if err != nil {
		t.Fatalf("buildTarget() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(builtScope, collected.Scope) {
		t.Fatalf("built scope = %+v, want in-tree scope %+v", builtScope, collected.Scope)
	}
	if builtGeneration.GenerationID != collected.Generation.GenerationID ||
		builtGeneration.ScopeID != collected.Generation.ScopeID ||
		builtGeneration.Status != collected.Generation.Status ||
		builtGeneration.TriggerKind != collected.Generation.TriggerKind ||
		builtGeneration.SourceCommitSHA != collected.Generation.SourceCommitSHA ||
		builtGeneration.TrustedDeletion != collected.Generation.TrustedDeletion ||
		!builtGeneration.ObservedAt.Equal(collected.Generation.ObservedAt) {
		t.Fatalf("built generation = %+v, want in-tree generation %+v", builtGeneration, collected.Generation)
	}
}

// TestCommitHandlerRouteMatchesProtocolTable pins the mount to the released
// protocol: the handler serves exactly the 21st route's method and path, so
// the proxy forward lands on it. A drift between the two repos would 404
// every forwarded commit, so the path comes from the shared RouteTable,
// never a local literal.
func TestCommitHandlerRouteMatchesProtocolTable(t *testing.T) {
	t.Parallel()

	route, ok := workflowv1.RouteFor(workflowv1.OpCommitClaimFacts)
	if !ok {
		t.Fatalf("RouteFor(OpCommitClaimFacts) not found: the handler has no protocol route")
	}
	if route.Method != http.MethodPost {
		t.Fatalf("commit route method = %q, want POST", route.Method)
	}
	if got := CommitClaimFactsRoutePath(); got != route.Path {
		t.Fatalf("handler route path = %q, want the protocol table path %q", got, route.Path)
	}
	bundle := mustNewCommitHandlerTestBundle(t)
	workItem, mutation := commitHandlerPlanned(bundle.now)
	recorder := commitHandlerPost(t, NewCommitClaimFactsMux(bundle.handler),
		commitHandlerBody(t, mutation, commitHandlerResult(workItem, testSDKFact(workItem))))
	if recorder.Code != http.StatusOK {
		t.Fatalf("mux status = %d, want 200 (body %q)", recorder.Code, recorder.Body.String())
	}
}

// TestCommitHandlerBoundStaysUnderProxyTimeout proves the 120s proxy bound
// check: the core commit bound the handler enforces leaves margin under the
// satellite CommitForwardTimeout, so a slow synchronous commit still answers
// through the proxy instead of dying to the proxy client timeout. A commit
// that overruns the bound answers 500, never an ack: the proxy must retry,
// not complete.
func TestCommitHandlerBoundStaysUnderProxyTimeout(t *testing.T) {
	t.Parallel()

	if DefaultCommitHandlerTimeout >= 120*time.Second {
		t.Fatalf("DefaultCommitHandlerTimeout = %v, want under the 120s proxy bound", DefaultCommitHandlerTimeout)
	}

	bundle := mustNewCommitHandlerTestBundle(t)
	bundle.handler.commitTimeout = 200 * time.Millisecond
	workItem, mutation := commitHandlerPlanned(bundle.now)
	bundle.committer.block = make(chan struct{})
	defer close(bundle.committer.block)
	recorder := commitHandlerPost(t, bundle.handler, commitHandlerBody(t, mutation, commitHandlerResult(workItem, testSDKFact(workItem))))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for an overrun commit (body %q)", recorder.Code, recorder.Body.String())
	}
	// "internal" is the proxy's own literal for non-protocol failures; the
	// handler mirrors it so the envelope stays verbatim-compatible.
	if code := commitHandlerEnvelopeCode(t, recorder); code != "internal" {
		t.Fatalf("envelope code = %q, want internal", code)
	}
}

// commitReplayRunner replays one canned result for scope-parity construction.
type commitReplayRunner struct {
	result sdkcollector.Result
}

func (r *commitReplayRunner) RunCollector(context.Context, Request) (sdkcollector.Result, error) {
	return r.result, nil
}
