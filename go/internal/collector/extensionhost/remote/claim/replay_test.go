// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package claim

// Replay of the EXTRACTION_PREP.md section 7 hostile-input proof sites
// through the remote seam, one row per test:
//
//	PREP row                          Replay test
//	Revoked grant at wiring           TestReplay_RevokedGrantAtWiringFailsClosed
//	Revoked/expired at emission       TestReplay_RevokedGrantAtEmissionDenied,
//	                                  TestReplay_ExpiredGrantAtEmissionDenied,
//	                                  TestReplay_RestoredGrantAllowsAgain
//	Sandbox escape / floating image   NOT APPLICABLE at this seam: the client
//	                                  holds no runner and launches no
//	                                  processes (no exec surface exists to
//	                                  pin); digest pinning stays in oci.go.
//	Resource exhaustion               TestReplay_OversizedResponseFailsClosed,
//	                                  TestReplay_AtBoundResponsePasses
//	Cancellation                      TestReplay_CanceledContextSendsNothing,
//	                                  TestReplay_CancelMidFlightAborts
//	Malformed results                 TestReplay_TrailingJSONRejected,
//	                                  TestReplay_UnknownFieldRejected,
//	                                  TestReplay_UnpinnedCommitIsStaleFence,
//	                                  TestReplay_LifecycleBodyBytesRejected,
//	                                  TestLifecycleAckIsDecodedStrictly
//	                                  (hardening_test.go)
//	Identity mismatch                 TestReplay_ClaimItemMismatchRejected,
//	                                  TestClaimNextRejectsAGrantForAnotherSelector
//	                                  (hardening_test.go)
//	Credential leakage                TestReplay_AuthTokenNeverSurfacesInErrors,
//	                                  TestReplay_HostileServerEchoScrubbed,
//	                                  TestEchoedTokenStraddlingTheBoundIsScrubbedBeforeTruncation
//	                                  (hardening_test.go)
//	Grant-before-admission            TestReplay_UnreadableGrantsDenyCoreKinds,
//	                                  TestReplay_NonCoreKindNeedsNoGrant,
//	                                  TestClaimNextValidatesLocallyWithoutTouchingTheWire
//	                                  (in claim_test.go)
//	Grant expiry under the default    TestNilClockTracksWallTimeSoExpiredGrantsDeny
//	clock                             (hardening_test.go)
//
// contract_test.go replays the wire shapes through the real pinned
// eshu-workflow server handler, so the fake cannot drift from the satellite.
//
// Single-holder producer authority (the grant names exactly one producer
// identity, fixed at construction, never taken from the wire) is proved in
// holder_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"

	"github.com/eshu-hq/eshu/go/internal/component"
)

const (
	replayCoreKind   = "aws_resource"
	replayCoreSchema = "1.0.0"
)

// mutableGrants is a live grant set the test can revoke mid-flight, proving
// the re-read observes state changes without client reconstruction.
type mutableGrants struct {
	mu     sync.Mutex
	grants []component.ProducerGrant
	err    error
}

func (m *mutableGrants) read(context.Context) ([]component.ProducerGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return append([]component.ProducerGrant(nil), m.grants...), nil
}

func (m *mutableGrants) set(grants []component.ProducerGrant, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.grants = grants
	m.err = err
}

func liveGrant() component.ProducerGrant {
	return component.ProducerGrant{
		ProducerID:     testProducerID,
		Version:        testProducerVersion,
		Kind:           replayCoreKind,
		SchemaVersions: []string{replayCoreSchema},
		Scope:          testCollectorKind,
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
	}
}

func replayClient(t *testing.T, baseURL string, live *mutableGrants, observer Observer) *Client {
	t.Helper()
	cfg := testConfig(baseURL)
	cfg.ReadGrants = live.read
	cfg.Observer = observer
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// recordingObserver captures grant decisions for deny-signal assertions.
type recordingObserver struct {
	mu        sync.Mutex
	decisions []Decision
}

func (r *recordingObserver) ObserveDecision(_ context.Context, d Decision) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.decisions = append(r.decisions, d)
}

func (r *recordingObserver) snapshot() []Decision {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Decision(nil), r.decisions...)
}

func TestReplay_RevokedGrantAtWiringFailsClosed(t *testing.T) {
	t.Parallel()

	revoked := liveGrant()
	revoked.Revoked = true
	live := &mutableGrants{grants: []component.ProducerGrant{revoked}}
	observer := &recordingObserver{}
	client := replayClient(t, "http://localhost:1", live, observer)

	err := client.VerifyWiring(context.Background(), []DeclaredEmission{{Kind: replayCoreKind, SchemaVersion: replayCoreSchema}})
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("VerifyWiring() = %v (%T), want *DenyError", err, err)
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

func TestReplay_RevokedGrantAtEmissionDenied(t *testing.T) {
	t.Parallel()

	revoked := liveGrant()
	revoked.Revoked = true
	live := &mutableGrants{grants: []component.ProducerGrant{revoked}}
	observer := &recordingObserver{}
	client := replayClient(t, "http://localhost:1", live, observer)

	err := client.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("AuthorizeEmission() = %v (%T), want *DenyError", err, err)
	}
	if deny.Reason != component.GrantReasonRevoked {
		t.Fatalf("reason = %q, want revoked", deny.Reason)
	}
	decisions := observer.snapshot()
	if len(decisions) != 1 || decisions[0].Allowed || decisions[0].Stage != StageEmission {
		t.Fatalf("decisions = %+v, want one emission deny", decisions)
	}
}

func TestReplay_ExpiredGrantAtEmissionDenied(t *testing.T) {
	t.Parallel()

	expired := liveGrant()
	expired.ExpiresAt = time.Now().UTC().Add(-time.Hour)
	live := &mutableGrants{grants: []component.ProducerGrant{expired}}
	client := replayClient(t, "http://localhost:1", live, nil)

	err := client.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("AuthorizeEmission() = %v (%T), want *DenyError", err, err)
	}
	if deny.Reason != component.GrantReasonExpired {
		t.Fatalf("reason = %q, want expired", deny.Reason)
	}
}

func TestReplay_RestoredGrantAllowsAgain(t *testing.T) {
	t.Parallel()

	revoked := liveGrant()
	revoked.Revoked = true
	live := &mutableGrants{grants: []component.ProducerGrant{revoked}}
	client := replayClient(t, "http://localhost:1", live, nil)

	if err := client.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema); err == nil {
		t.Fatal("AuthorizeEmission() with revoked grant = nil, want deny")
	}
	live.set([]component.ProducerGrant{liveGrant()}, nil)
	if err := client.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema); err != nil {
		t.Fatalf("AuthorizeEmission() after restore = %v, want allow without reconstruction", err)
	}
}

func TestReplay_OversizedResponseFailsClosed(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	now := time.Now().UTC().Truncate(time.Second)
	grant := mustGrantJSON(t, validGrant(now))
	fake.on(workflowv1.OpClaimNext, func(*http.Request) (int, string) {
		return http.StatusOK, grant + strings.Repeat(" ", 4096)
	})

	cfg := testConfig(fake.server.URL)
	cfg.MaxResponseBytes = 512
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.ClaimNext(context.Background(), validSelector(), now, time.Minute)
	var callErr *CallError
	if !errors.As(err, &callErr) {
		t.Fatalf("ClaimNext() = %v (%T), want *CallError for an over-bound body", err, err)
	}
	if callErr.Code != CodeResponseTooLarge {
		t.Fatalf("code = %q, want %q", callErr.Code, CodeResponseTooLarge)
	}
}

func TestReplay_AtBoundResponsePasses(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	now := time.Now().UTC().Truncate(time.Second)
	grant := mustGrantJSON(t, validGrant(now))

	cfg := testConfig(fake.server.URL)
	cfg.MaxResponseBytes = int64(len(grant))
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fake.okJSON(workflowv1.OpClaimNext, grant)
	if _, found, err := client.ClaimNext(context.Background(), validSelector(), now, time.Minute); err != nil || !found {
		t.Fatalf("ClaimNext() found=%v err=%v, want the grant at exactly the bound", found, err)
	}
}

func TestReplay_CanceledContextSendsNothing(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	fake.okJSON(workflowv1.OpClaimNext, `{}`)
	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := client.ClaimNext(ctx, validSelector(), time.Now().UTC(), time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("ClaimNext() = %v, want context.Canceled", err)
	}
	if got := fake.totalCalls(); got != 0 {
		t.Fatalf("wire calls = %d, want 0 for a canceled context", got)
	}
}

func TestReplay_CancelMidFlightAborts(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	fake.hang(workflowv1.OpHeartbeatClaim)
	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.Heartbeat(ctx, validMutation(time.Now().UTC()))
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Heartbeat() = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Heartbeat() did not return after cancel")
	}
}

func TestReplay_TrailingJSONRejected(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	now := time.Now().UTC().Truncate(time.Second)
	fake.okJSON(workflowv1.OpClaimNext, mustGrantJSON(t, validGrant(now))+`{"injected":true}`)
	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.ClaimNext(context.Background(), validSelector(), now, time.Minute); err == nil {
		t.Fatal("ClaimNext() with trailing JSON = nil, want strict rejection")
	}
}

func TestReplay_UnknownFieldRejected(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	now := time.Now().UTC().Truncate(time.Second)
	var decoded map[string]any
	raw := mustGrantJSON(t, validGrant(now))
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatal(err)
	}
	decoded["bogus_future_field"] = 1
	patched, _ := json.Marshal(decoded)
	fake.okJSON(workflowv1.OpClaimNext, string(patched))
	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.ClaimNext(context.Background(), validSelector(), now, time.Minute); err == nil {
		t.Fatal("ClaimNext() with an unknown field = nil, want strict rejection")
	}
}

func TestReplay_UnpinnedCommitIsStaleFence(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	fake.okJSON(workflowv1.OpValidateAndPinClaimForCommit, `{"pinned":false}`)
	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = client.PinForCommit(context.Background(), workflowv1.CommitPinRequest{
		ClaimID:        "claim-1",
		FencingToken:   9,
		ScopeID:        "scope-1",
		SourceRunID:    "sr-1",
		GenerationID:   "gen-1",
		CommitDeadline: now.Add(time.Minute),
	})
	var callErr *CallError
	if !errors.As(err, &callErr) {
		t.Fatalf("PinForCommit() = %v (%T), want *CallError", err, err)
	}
	if !callErr.Stale() {
		t.Fatalf("Stale() = false for code %q, want the stale-fence signal, never a commit", callErr.Code)
	}
}

func TestReplay_LifecycleBodyBytesRejected(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	fake.on(workflowv1.OpCompleteClaim, func(*http.Request) (int, string) {
		return http.StatusOK, `{"unexpected":"body"}`
	})
	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Complete(context.Background(), validMutation(time.Now().UTC())); err == nil {
		t.Fatal("Complete() with a body that is not the ok acknowledgement = nil, want rejection")
	}
}

func TestReplay_ClaimItemMismatchRejected(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	now := time.Now().UTC().Truncate(time.Second)
	grant := validGrant(now)
	grant.Claim.WorkItemID = "someone-elses-work"
	fake.okJSON(workflowv1.OpClaimNext, mustGrantJSON(t, grant))
	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, found, err := client.ClaimNext(context.Background(), validSelector(), now, time.Minute)
	if err == nil || found {
		t.Fatalf("ClaimNext() found=%v err=%v, want terminal identity rejection", found, err)
	}
	if got := fake.totalCalls(); got != 1 {
		t.Fatalf("wire calls = %d, want exactly the rejected claim (no heartbeat/commit follows)", got)
	}
}

func TestReplay_AuthTokenNeverSurfacesInErrors(t *testing.T) {
	t.Parallel()
	const canary = "test-only-canary-bearer-9f2k7q"
	fake := newFakeWorkflow(t)
	fake.fail(workflowv1.OpClaimNext, http.StatusInternalServerError, "internal", "boom")
	fake.fail(workflowv1.OpCompleteClaim, http.StatusBadGateway, "upstream", "bad gateway")
	fake.fail(workflowv1.OpValidateAndPinClaimForCommit, http.StatusServiceUnavailable, "unavailable", "down")

	cfg := testConfig(fake.server.URL)
	cfg.AuthToken = canary
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	errs := []error{}
	_, _, err = client.ClaimNext(context.Background(), validSelector(), now, time.Minute)
	errs = append(errs, err)
	errs = append(errs, client.Complete(context.Background(), validMutation(now)))
	_, err = client.PinForCommit(context.Background(), workflowv1.CommitPinRequest{
		ClaimID:        "claim-1",
		FencingToken:   9,
		ScopeID:        "scope-1",
		SourceRunID:    "sr-1",
		GenerationID:   "gen-1",
		CommitDeadline: now.Add(time.Minute),
	})
	errs = append(errs, err)
	for _, err := range errs {
		if err == nil {
			t.Fatal("expected an error from a failing op, got nil")
		}
		if strings.Contains(err.Error(), canary) {
			t.Fatalf("error %q carries the bearer token", err.Error())
		}
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, req := range fake.requests {
		if req.authHeader != "Bearer "+canary {
			t.Fatalf("auth header = %q, want the bearer on the wire only", req.authHeader)
		}
		if strings.Contains(string(req.body), canary) {
			t.Fatalf("request body carries the bearer token: %s", req.body)
		}
	}
}

func TestReplay_HostileServerEchoScrubbed(t *testing.T) {
	t.Parallel()
	const canary = "test-only-canary-bearer-9f2k7q"
	fake := newFakeWorkflow(t)
	fake.on(workflowv1.OpClaimNext, func(*http.Request) (int, string) {
		return http.StatusInternalServerError,
			`{"code":"internal","message":"backend failed for ` + canary + `"}`
	})

	cfg := testConfig(fake.server.URL)
	cfg.AuthToken = canary
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.ClaimNext(context.Background(), validSelector(), time.Now().UTC(), time.Minute)
	if err == nil {
		t.Fatal("ClaimNext() = nil, want the server error")
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("error %q echoes the bearer token back", err.Error())
	}
}

func TestReplay_UnreadableGrantsDenyCoreKinds(t *testing.T) {
	t.Parallel()

	live := &mutableGrants{}
	live.set(nil, errors.New("registry unreadable"))
	client := replayClient(t, "http://localhost:1", live, nil)

	err := client.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("AuthorizeEmission() = %v (%T), want *DenyError", err, err)
	}
	if deny.Reason != component.GrantReasonGrantsUnreadable {
		t.Fatalf("reason = %q, want grants_unreadable", deny.Reason)
	}
	if err := client.VerifyWiring(context.Background(), []DeclaredEmission{{Kind: replayCoreKind, SchemaVersion: replayCoreSchema}}); err == nil {
		t.Fatal("VerifyWiring() with unreadable grants = nil, want fail-closed")
	}
}

func TestReplay_NonCoreKindNeedsNoGrant(t *testing.T) {
	t.Parallel()

	live := &mutableGrants{err: errors.New("registry unreadable")}
	client := replayClient(t, "http://localhost:1", live, nil)

	if err := client.AuthorizeEmission(context.Background(), "custom_widget", "1.0.0"); err != nil {
		t.Fatalf("AuthorizeEmission(non-core) = %v, want allow without any grant", err)
	}
}
