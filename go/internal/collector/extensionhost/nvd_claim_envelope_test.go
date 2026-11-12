// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

// deadlineRecordingRunner captures the context deadline the host enforces on
// the satellite run, so the trace below proves which instant actually bounds
// the walk.
type deadlineRecordingRunner struct {
	requests    []Request
	deadlines   []time.Time
	hasDeadline []bool
	result      sdkcollector.Result
}

func (r *deadlineRecordingRunner) RunCollector(ctx context.Context, request Request) (sdkcollector.Result, error) {
	r.requests = append(r.requests, request)
	deadline, ok := ctx.Deadline()
	r.deadlines = append(r.deadlines, deadline)
	r.hasDeadline = append(r.hasDeadline, ok)
	return r.result, nil
}

// TestNVDClaimEnvelopeCoversMeasuredWalk is the lane-B claim-plane trace
// proof for eshu-hq/eshu#10446 (option B: 600s NVD claim envelope). It walks
// one claim grant to satellite deadline through the real wiring and names
// each changed knob in the chain:
//
//	worker ClaimLeaseTTL (600s fallback, live 10m per-activation env)
//	-> dispatch-granted LeaseExpiresAt = ObservedAt + TTL
//	  (grant math pinned by TestClaimedServiceRestampsStaleLeaseAtDispatch)
//	-> Claim.Deadline = LeaseExpiresAt (request.go:63)
//	-> runCtx deadline = Claim.Deadline (source.go:185)
//
// The envelope must clear the measured walk floor (>=546s for the
// 4566-record / 92-page head window); the old ~300s envelope is what wedged
// the rolling stream on context-deadline launch failures.
func TestNVDClaimEnvelopeCoversMeasuredWalk(t *testing.T) {
	t.Parallel()

	const envelope = 600 * time.Second
	const measuredWalkFloor = 546 * time.Second
	if envelope <= measuredWalkFloor {
		t.Fatalf("ruled envelope = %v must clear the measured walk floor = %v", envelope, measuredWalkFloor)
	}

	now := testObservedAt()
	item := testWorkItem()
	// Dispatch-granted lease: the store grants ObservedAt + ClaimLeaseTTL at
	// dispatch, and the host ships that instant to the satellite.
	item.LeaseExpiresAt = now.Add(envelope)
	fact := testSDKFact(item)
	runner := &deadlineRecordingRunner{result: completeResult(item, fact)}
	recorder := &recordingStatusRecorder{}
	source := mustNewSource(t, runner, recorder)

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if len(runner.requests) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.requests))
	}
	// request.go:63 — the satellite deadline is the granted lease, nothing else.
	if got := runner.requests[0].Claim.Deadline; !got.Equal(item.LeaseExpiresAt) {
		t.Fatalf("Claim.Deadline = %s, want granted lease %s", got, item.LeaseExpiresAt)
	}
	// source.go:185 — the host run context enforces exactly that deadline.
	if len(runner.hasDeadline) != 1 || !runner.hasDeadline[0] {
		t.Fatal("satellite run context has no deadline, want the granted lease as deadline")
	}
	if got := runner.deadlines[0]; !got.Equal(item.LeaseExpiresAt) {
		t.Fatalf("run context deadline = %s, want granted lease %s", got, item.LeaseExpiresAt)
	}
	// The walk completes inside the envelope: one fact out, no terminal verdict.
	if got, want := collected.Scope.ScopeID, item.ScopeID; got != want {
		t.Fatalf("Scope.ScopeID = %q, want %q", got, want)
	}
	if envelopes := collectFacts(t, collected); len(envelopes) != 1 {
		t.Fatalf("fact count = %d, want 1", len(envelopes))
	}
}
