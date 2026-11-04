// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package claim

// Wired-path replay of the EXTRACTION_PREP.md section 7 hostile-input sites
// through the cutover gate, one row per test. This header extends the
// replay_test.go map for the flag-on path; replay_test.go itself is at the
// file-size cap, so its table stays untouched and every wired row lives here:
//
//	PREP row                          Wired test
//	Revoked grant at wiring           TestCutover_EnabledWiringDeniesRevokedGrant
//	                                  (emission re-read in
//	                                  TestCutover_EnabledLiveGrantRereadDeniesRevoked)
//	Revoked/expired at emission       TestCutover_EnabledLiveGrantRereadDeniesRevoked,
//	                                  TestCutover_WiredDecisionsMatchInTreeAuthority
//	                                  (revoked, expired, unreadable, neighbor,
//	                                  scope, and schema rows vs direct in-tree
//	                                  EvaluateEmission/ClassifyEmission)
//	Grant-before-admission            TestCutover_DisabledParityMakesNoCalls (sentinel rows),
//	                                  TestCutover_EnabledLiveGrantRereadDeniesRevoked
//	Resource exhaustion             NOT APPLICABLE at the gate: the gate adds no
//	                                  buffering (see TestCutover_DisabledParityMakesNoCalls
//	                                  zero-call rows for claim/commit/pin)
//	Cancellation                      TestCutover_DisabledParityMakesNoCalls (canceled contexts
//	                                  return the disabled sentinel before any wait)
//	Malformed results                 Through the plane client unchanged (claim_test.go,
//	                                  hardening_test.go); the gate adds no decoder
//	Identity mismatch                 TestCutover_EnabledDeniesASecondProducer
//	                                  (holder + scope rows)
//	Credential leakage                Through the plane client unchanged; the gate
//	                                  carries no Bearer [REDACTED] no body
//	Single-holder authority           TestCutover_EnabledDeniesASecondProducer
//	Concurrency (new lock/wait edge)  TestCutover_EnabledConcurrentUseIsRaceClean:
//	                                  the gate is immutable after construction and
//	                                  adds no mutex, goroutine, or channel
//
//	Enabled-path delegation (all ten delegates route through the plane):
//	TestCutover_EnabledClaimRoutesThroughThePlane,
//	TestCutover_EnabledCommitRoutesThroughThePlane,
//	TestCutover_EnabledLifecycleRoutesThroughThePlane (heartbeat, release,
//	fail retryable, fail terminal),
//	TestCutover_EnabledVerifyWiringRoutesThroughThePlane.
//	Nil-gate contract: TestCutover_NilGateReportsDisabled.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"

	"github.com/eshu-hq/eshu/go/internal/component"
)

func disabledCutover() *Cutover {
	cutover, err := NewCutover(CutoverConfig{})
	if err != nil {
		panic("NewCutover(disabled): " + err.Error())
	}
	return cutover
}

func enabledCutover(t *testing.T, baseURL string, live *mutableGrants, observer Observer) *Cutover {
	t.Helper()
	cutover, err := NewCutover(CutoverConfig{Enabled: true, Client: replayClient(t, baseURL, live, observer)})
	if err != nil {
		t.Fatal(err)
	}
	return cutover
}

func TestCutover_DisabledIsTheDefault(t *testing.T) {
	t.Parallel()

	cutover, err := NewCutover(CutoverConfig{})
	if err != nil {
		t.Fatalf("NewCutover(zero) = %v, want the default-off gate without a client", err)
	}
	if cutover.Enabled() {
		t.Fatal("Enabled() = true for the zero config, want the default-off gate")
	}
}

func TestCutover_EnabledRequiresAClient(t *testing.T) {
	t.Parallel()

	if _, err := NewCutover(CutoverConfig{Enabled: true}); err == nil {
		t.Fatal("NewCutover(enabled without a client) = nil, want fail-closed")
	}
}

func TestCutover_DisabledParityMakesNoCalls(t *testing.T) {
	t.Parallel()

	cutover := disabledCutover()
	ctx := context.Background()
	now := time.Now().UTC()

	// Representative valid inputs plus section 7 hostile rows: the disabled
	// gate holds no client, so every one reports the disabled sentinel
	// without touching grants, observers, or the wire. The caller keeps the
	// in-tree path, byte-identically to a binary without the gate.
	if _, _, err := cutover.ClaimNext(ctx, validSelector(), now, time.Minute); !errors.Is(err, ErrCutoverDisabled) {
		t.Fatalf("ClaimNext(disabled) = %v, want ErrCutoverDisabled", err)
	}
	if err := cutover.Heartbeat(ctx, validMutation(now)); !errors.Is(err, ErrCutoverDisabled) {
		t.Fatalf("Heartbeat(disabled) = %v, want ErrCutoverDisabled", err)
	}
	if err := cutover.Complete(ctx, validMutation(now)); !errors.Is(err, ErrCutoverDisabled) {
		t.Fatalf("Complete(disabled) = %v, want ErrCutoverDisabled", err)
	}
	if err := cutover.Release(ctx, validMutation(now)); !errors.Is(err, ErrCutoverDisabled) {
		t.Fatalf("Release(disabled) = %v, want ErrCutoverDisabled", err)
	}
	if err := cutover.FailRetryable(ctx, validMutation(now)); !errors.Is(err, ErrCutoverDisabled) {
		t.Fatalf("FailRetryable(disabled) = %v, want ErrCutoverDisabled", err)
	}
	if err := cutover.FailTerminal(ctx, validMutation(now)); !errors.Is(err, ErrCutoverDisabled) {
		t.Fatalf("FailTerminal(disabled) = %v, want ErrCutoverDisabled", err)
	}
	if _, err := cutover.PinForCommit(ctx, workflowv1.CommitPinRequest{
		ClaimID:        "claim-1",
		FencingToken:   9,
		ScopeID:        "scope-1",
		SourceRunID:    "sr-1",
		GenerationID:   "gen-1",
		CommitDeadline: now.Add(time.Minute),
	}); !errors.Is(err, ErrCutoverDisabled) {
		t.Fatalf("PinForCommit(disabled) = %v, want ErrCutoverDisabled", err)
	}
	if err := cutover.AuthorizeEmission(ctx, replayCoreKind, replayCoreSchema); !errors.Is(err, ErrCutoverDisabled) {
		t.Fatalf("AuthorizeEmission(disabled) = %v, want ErrCutoverDisabled", err)
	}
	if err := cutover.VerifyWiring(ctx, []DeclaredEmission{{Kind: replayCoreKind, SchemaVersion: replayCoreSchema}}); !errors.Is(err, ErrCutoverDisabled) {
		t.Fatalf("VerifyWiring(disabled) = %v, want ErrCutoverDisabled", err)
	}
	if _, err := cutover.ReadLiveGrants(ctx); !errors.Is(err, ErrCutoverDisabled) {
		t.Fatalf("ReadLiveGrants(disabled) = %v, want ErrCutoverDisabled", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := cutover.ClaimNext(canceled, validSelector(), now, time.Minute); !errors.Is(err, ErrCutoverDisabled) {
		t.Fatalf("ClaimNext(disabled, canceled) = %v, want the sentinel before any wait", err)
	}
}

// TestCutover_WiredDecisionsMatchInTreeAuthority proves the flag-on path is
// behaviorally identical to the in-tree path on the decision surface: the
// wired Client reuses component.EvaluateEmission and component.ClassifyEmission
// (the same authority Source.validateGrantCoverage consults), so for every
// representative and hostile grant set the wired verdict equals the direct
// in-tree verdict, byte for byte on allow and reason.
func TestCutover_WiredDecisionsMatchInTreeAuthority(t *testing.T) {
	t.Parallel()

	otherProducer := liveGrant()
	otherProducer.ProducerID = "some-other-producer"
	otherScope := liveGrant()
	otherScope.Scope = "not-a-declared-kind"
	otherSchema := liveGrant()
	otherSchema.SchemaVersions = []string{"9.9.9"}
	expired := liveGrant()
	expired.ExpiresAt = time.Now().UTC().Add(-time.Hour)
	revoked := liveGrant()
	revoked.Revoked = true
	tests := []struct {
		name   string
		grants []component.ProducerGrant
		err    error
	}{
		{name: "live grant allows", grants: []component.ProducerGrant{liveGrant()}},
		{name: "revoked grant denies", grants: []component.ProducerGrant{revoked}},
		{name: "expired grant denies", grants: []component.ProducerGrant{expired}},
		{name: "unreadable set denies fail-closed", err: errors.New("registry unreadable")},
		{name: "neighbor producer denies", grants: []component.ProducerGrant{otherProducer}},
		{name: "undeclared scope denies", grants: []component.ProducerGrant{otherScope}},
		{name: "uncovered schema denies", grants: []component.ProducerGrant{otherSchema}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			now := time.Now().UTC()
			live := &mutableGrants{grants: tc.grants, err: tc.err}
			cutover := enabledCutover(t, "http://localhost:1", live, nil)

			var inTree error
			grants, readErr := live.read(context.Background())
			if readErr != nil {
				grants = nil
			}
			allowed, governed := component.EvaluateEmission(
				grants, testProducerID, testProducerVersion,
				replayCoreKind, replayCoreSchema, []string{testCollectorKind}, now,
			)
			_ = governed
			if !allowed {
				reason := component.GrantReasonGrantsUnreadable
				if readErr == nil {
					reason = component.ClassifyEmission(
						grants, testProducerID, testProducerVersion,
						replayCoreKind, replayCoreSchema, []string{testCollectorKind}, now,
					)
				}
				inTree = &DenyError{Reason: reason}
			}

			wired := cutover.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema)
			if (wired == nil) != (inTree == nil) {
				t.Fatalf("wired = %v, in-tree = %v, want the same allow/deny", wired, inTree)
			}
			if wired != nil {
				var wiredDeny, treeDeny *DenyError
				if !errors.As(wired, &wiredDeny) || !errors.As(inTree, &treeDeny) {
					t.Fatalf("wired = %v (%T), in-tree = %v (%T), want *DenyError on both", wired, wired, inTree, inTree)
				}
				if wiredDeny.Reason != treeDeny.Reason {
					t.Fatalf("wired reason = %q, in-tree reason = %q, want byte-identical", wiredDeny.Reason, treeDeny.Reason)
				}
			}
		})
	}
}

func TestCutover_EnabledClaimRoutesThroughThePlane(t *testing.T) {
	t.Parallel()

	fake := newFakeWorkflow(t)
	now := time.Now().UTC().Truncate(time.Second)
	fake.okJSON(workflowv1.OpClaimNext, mustGrantJSON(t, validGrant(now)))
	live := &mutableGrants{grants: []component.ProducerGrant{liveGrant()}}
	cutover := enabledCutover(t, fake.server.URL, live, nil)

	grant, found, err := cutover.ClaimNext(context.Background(), validSelector(), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("found = false, want the issued grant through the wired path")
	}
	if grant.WorkItem.WorkItemID != "wi-1" || grant.Claim.ClaimID != "claim-1" {
		t.Fatalf("grant = %+v, want wi-1/claim-1", grant)
	}
	if got := fake.calls(workflowv1.OpClaimNext); got != 1 {
		t.Fatalf("ClaimNext calls = %d, want 1 on the plane", got)
	}
}

func TestCutover_EnabledCommitRoutesThroughThePlane(t *testing.T) {
	t.Parallel()

	fake := newFakeWorkflow(t)
	fake.okJSON(workflowv1.OpValidateAndPinClaimForCommit, `{"pinned":true,"commit_id":"commit-1"}`)
	fake.okJSON(workflowv1.OpCompleteClaim, okAckBody)
	live := &mutableGrants{grants: []component.ProducerGrant{liveGrant()}}
	cutover := enabledCutover(t, fake.server.URL, live, nil)
	ctx := context.Background()
	now := time.Now().UTC()

	pin, err := cutover.PinForCommit(ctx, workflowv1.CommitPinRequest{
		ClaimID:        "claim-1",
		FencingToken:   9,
		ScopeID:        "scope-1",
		SourceRunID:    "sr-1",
		GenerationID:   "gen-1",
		CommitDeadline: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if pin.CommitID != "commit-1" {
		t.Fatalf("commit id = %q, want commit-1 through the wired path", pin.CommitID)
	}
	if err := cutover.Complete(ctx, validMutation(now)); err != nil {
		t.Fatalf("Complete() = %v, want the ok acknowledgement through the wired path", err)
	}
	if got := fake.calls(workflowv1.OpValidateAndPinClaimForCommit); got != 1 {
		t.Fatalf("PinForCommit calls = %d, want 1 on the plane", got)
	}
	if got := fake.calls(workflowv1.OpCompleteClaim); got != 1 {
		t.Fatalf("Complete calls = %d, want 1 on the plane", got)
	}
}

func TestCutover_EnabledLiveGrantRereadDeniesRevoked(t *testing.T) {
	t.Parallel()

	revoked := liveGrant()
	revoked.Revoked = true
	live := &mutableGrants{grants: []component.ProducerGrant{revoked}}
	observer := &recordingObserver{}
	cutover := enabledCutover(t, "http://localhost:1", live, observer)
	ctx := context.Background()

	err := cutover.AuthorizeEmission(ctx, replayCoreKind, replayCoreSchema)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("AuthorizeEmission() = %v (%T), want *DenyError through the wired path", err, err)
	}
	if deny.Reason != component.GrantReasonRevoked {
		t.Fatalf("reason = %q, want revoked", deny.Reason)
	}

	// The re-read observes state changes without reconstruction: restoring
	// the grant allows the next emission again.
	live.set([]component.ProducerGrant{liveGrant()}, nil)
	if err := cutover.AuthorizeEmission(ctx, replayCoreKind, replayCoreSchema); err != nil {
		t.Fatalf("AuthorizeEmission() after restore = %v, want allow without reconstruction", err)
	}
	current, err := cutover.ReadLiveGrants(ctx)
	if err != nil {
		t.Fatalf("ReadLiveGrants() = %v, want the restored set", err)
	}
	if len(current) != 1 || current[0].Revoked {
		t.Fatalf("live grants = %+v, want the one restored grant", current)
	}
	decisions := observer.snapshot()
	if len(decisions) != 2 || decisions[0].Allowed || !decisions[1].Allowed {
		t.Fatalf("decisions = %+v, want deny then allow through the wired path", decisions)
	}
}

// TestCutover_EnabledWiringDeniesRevokedGrant replays the section 7
// "revoked grant at wiring" row through the enabled gate: VerifyWiring
// denies a revoked grant at the wiring stage before touching the wire,
// mirroring TestReplay_RevokedGrantAtWiringFailsClosed on the client.
func TestCutover_EnabledWiringDeniesRevokedGrant(t *testing.T) {
	t.Parallel()

	revoked := liveGrant()
	revoked.Revoked = true
	live := &mutableGrants{grants: []component.ProducerGrant{revoked}}
	observer := &recordingObserver{}
	cutover := enabledCutover(t, "http://localhost:1", live, observer)

	err := cutover.VerifyWiring(context.Background(), []DeclaredEmission{{Kind: replayCoreKind, SchemaVersion: replayCoreSchema}})
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("VerifyWiring() = %v (%T), want *DenyError through the wired path", err, err)
	}
	if deny.Reason != component.GrantReasonRevoked {
		t.Fatalf("reason = %q, want revoked", deny.Reason)
	}
	decisions := observer.snapshot()
	if len(decisions) != 1 || decisions[0].Allowed || decisions[0].Reason != component.GrantReasonRevoked {
		t.Fatalf("decisions = %+v, want one wiring deny with reason revoked", decisions)
	}
	if decisions[0].Stage != StageWiring {
		t.Fatalf("stage = %q, want wiring", decisions[0].Stage)
	}
}

func TestCutover_EnabledDeniesASecondProducer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*component.ProducerGrant)
		reason component.GrantReason
	}{
		{
			name:   "another producer",
			mutate: func(g *component.ProducerGrant) { g.ProducerID = "some-other-producer" },
			reason: component.GrantReasonNoMatchingGrant,
		},
		{
			name:   "undeclared scope",
			mutate: func(g *component.ProducerGrant) { g.Scope = "not-a-declared-kind" },
			reason: component.GrantReasonScopeMismatch,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			grant := liveGrant()
			tc.mutate(&grant)
			live := &mutableGrants{grants: []component.ProducerGrant{grant}}
			cutover := enabledCutover(t, "http://localhost:1", live, nil)

			err := cutover.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema)
			var deny *DenyError
			if !errors.As(err, &deny) {
				t.Fatalf("AuthorizeEmission() = %v (%T), want *DenyError for a second producer", err, err)
			}
			if deny.Reason != tc.reason {
				t.Fatalf("reason = %q, want %q", deny.Reason, tc.reason)
			}
			if deny.ProducerID != testProducerID || deny.Version != testProducerVersion {
				t.Fatalf("deny = %+v, want the holder identity, never the wire's", deny)
			}
		})
	}
}

func TestCutover_EnabledConcurrentUseIsRaceClean(t *testing.T) {
	t.Parallel()

	fake := newFakeWorkflow(t)
	now := time.Now().UTC().Truncate(time.Second)
	fake.okJSON(workflowv1.OpClaimNext, mustGrantJSON(t, validGrant(now)))
	live := &mutableGrants{grants: []component.ProducerGrant{liveGrant()}}
	cutover := enabledCutover(t, fake.server.URL, live, nil)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			for j := 0; j < 25; j++ {
				if err := cutover.AuthorizeEmission(ctx, replayCoreKind, replayCoreSchema); err != nil {
					t.Error(err)
					return
				}
				if _, err := cutover.ReadLiveGrants(ctx); err != nil {
					t.Error(err)
					return
				}
				if _, _, err := cutover.ClaimNext(ctx, validSelector(), now, time.Minute); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestCutover_EnabledLifecycleRoutesThroughThePlane(t *testing.T) {
	t.Parallel()

	fake := newFakeWorkflow(t)
	fake.okJSON(workflowv1.OpHeartbeatClaim, okAckBody)
	fake.okJSON(workflowv1.OpReleaseClaim, okAckBody)
	fake.okJSON(workflowv1.OpFailClaimRetryable, okAckBody)
	fake.okJSON(workflowv1.OpFailClaimTerminal, okAckBody)
	live := &mutableGrants{grants: []component.ProducerGrant{liveGrant()}}
	cutover := enabledCutover(t, fake.server.URL, live, nil)
	ctx := context.Background()
	now := time.Now().UTC()

	tests := []struct {
		name string
		call func(context.Context, workflowv1.ClaimMutation) error
		op   workflowv1.Operation
	}{
		{name: "heartbeat", call: cutover.Heartbeat, op: workflowv1.OpHeartbeatClaim},
		{name: "release", call: cutover.Release, op: workflowv1.OpReleaseClaim},
		{name: "fail retryable", call: cutover.FailRetryable, op: workflowv1.OpFailClaimRetryable},
		{name: "fail terminal", call: cutover.FailTerminal, op: workflowv1.OpFailClaimTerminal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := tc.call(ctx, validMutation(now)); err != nil {
				t.Fatalf("%s() = %v, want the ok acknowledgement through the wired path", tc.name, err)
			}
			if got := fake.calls(tc.op); got != 1 {
				t.Fatalf("%s calls = %d, want 1 on the plane", tc.name, got)
			}
		})
	}
}

func TestCutover_EnabledVerifyWiringRoutesThroughThePlane(t *testing.T) {
	t.Parallel()

	live := &mutableGrants{grants: []component.ProducerGrant{liveGrant()}}
	cutover := enabledCutover(t, "http://localhost:1", live, nil)

	if err := cutover.VerifyWiring(context.Background(), []DeclaredEmission{{Kind: replayCoreKind, SchemaVersion: replayCoreSchema}}); err != nil {
		t.Fatalf("VerifyWiring() = %v, want allow against the live grant through the wired path", err)
	}
}

func TestCutover_NilGateReportsDisabled(t *testing.T) {
	t.Parallel()

	var cutover *Cutover
	if cutover.Enabled() {
		t.Fatal("nil gate Enabled() = true, want disabled")
	}
	if _, err := cutover.ReadLiveGrants(context.Background()); !errors.Is(err, ErrCutoverDisabled) {
		t.Fatalf("nil gate ReadLiveGrants() = %v, want ErrCutoverDisabled", err)
	}
}
