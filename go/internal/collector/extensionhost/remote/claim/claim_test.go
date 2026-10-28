// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package claim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"

	"github.com/eshu-hq/eshu/go/internal/component"
)

const (
	testProducerID      = "test-extension-producer"
	testProducerVersion = "0.1.0"
	testCollectorKind   = "test-source"
	testInstanceID      = "test-instance"
	testOwnerID         = "test-owner"
)

func testConfig(baseURL string) Config {
	return Config{
		BaseURL:         baseURL,
		ProducerID:      testProducerID,
		ProducerVersion: testProducerVersion,
		Scopes:          []string{testCollectorKind},
		ReadGrants:      func(context.Context) ([]component.ProducerGrant, error) { return nil, nil },
		Clock:           func() time.Time { return time.Now().UTC() },
	}
}

func validSelector() workflowv1.ClaimSelector {
	return workflowv1.ClaimSelector{
		CollectorKind:       testCollectorKind,
		CollectorInstanceID: testInstanceID,
		OwnerID:             testOwnerID,
		ClaimID:             "claim-1",
	}
}

func validGrant(now time.Time) workflowv1.ClaimedWorkItem {
	return workflowv1.ClaimedWorkItem{
		WorkItem: workflowv1.WorkItem{
			WorkItemID:          "wi-1",
			RunID:               "run-1",
			CollectorKind:       testCollectorKind,
			CollectorInstanceID: testInstanceID,
			SourceSystem:        "test-system",
			ScopeID:             "scope-1",
			AcceptanceUnitID:    "au-1",
			SourceRunID:         "sr-1",
			GenerationID:        "gen-1",
			Status:              workflowv1.WorkItemStatusClaimed,
			AttemptCount:        1,
			CurrentClaimID:      "claim-1",
			CurrentFencingToken: 3,
			CurrentOwnerID:      testOwnerID,
			LeaseExpiresAt:      now.Add(5 * time.Minute),
			CreatedAt:           now,
			UpdatedAt:           now,
		},
		Claim: workflowv1.Claim{
			ClaimID:        "claim-1",
			WorkItemID:     "wi-1",
			FencingToken:   3,
			OwnerID:        testOwnerID,
			Status:         workflowv1.ClaimStatusActive,
			ClaimedAt:      now,
			HeartbeatAt:    now,
			LeaseExpiresAt: now.Add(5 * time.Minute),
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
}

func validMutation(now time.Time) workflowv1.ClaimMutation {
	return workflowv1.ClaimMutation{
		WorkItemID:    "wi-1",
		ClaimID:       "claim-1",
		FencingToken:  3,
		OwnerID:       testOwnerID,
		ObservedAt:    now,
		LeaseDuration: time.Minute,
	}
}

func mustGrantJSON(t *testing.T, grant workflowv1.ClaimedWorkItem) string {
	t.Helper()
	raw, err := json.Marshal(workflowv1.ClaimNextResponse{Issued: true, Grant: grant})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestNewClientRejectsMissingProducerIdentity(t *testing.T) {
	t.Parallel()
	for _, mutate := range []func(*Config){
		func(c *Config) { c.BaseURL = "" },
		func(c *Config) { c.ProducerID = "  " },
		func(c *Config) { c.ProducerVersion = "" },
		func(c *Config) { c.ReadGrants = nil },
		func(c *Config) { c.Scopes = nil },
	} {
		cfg := testConfig("http://localhost:1")
		mutate(&cfg)
		if _, err := NewClient(cfg); err == nil {
			t.Fatalf("NewClient(%+v) = nil, want fail-closed config error", cfg)
		}
	}
}

func TestClaimNextAcceptsAValidGrant(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	now := time.Now().UTC().Truncate(time.Second)
	fake.okJSON(workflowv1.OpClaimNext, mustGrantJSON(t, validGrant(now)))

	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	item, found, err := client.ClaimNext(context.Background(), validSelector(), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("found = false, want the issued grant")
	}
	if item.WorkItem.WorkItemID != "wi-1" || item.Claim.ClaimID != "claim-1" {
		t.Fatalf("grant = %+v, want wi-1/claim-1", item)
	}
	if got := fake.calls(workflowv1.OpClaimNext); got != 1 {
		t.Fatalf("ClaimNext calls = %d, want 1", got)
	}
}

func TestClaimNextValidatesLocallyWithoutTouchingTheWire(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	selector := validSelector()
	selector.ClaimID = ""
	if _, _, err := client.ClaimNext(context.Background(), selector, now, time.Minute); err == nil {
		t.Fatal("ClaimNext with blank claim id = nil, want local validation error")
	}
	if _, _, err := client.ClaimNext(context.Background(), validSelector(), now, 0); err == nil {
		t.Fatal("ClaimNext with zero lease TTL = nil, want local validation error")
	}
	if got := fake.totalCalls(); got != 0 {
		t.Fatalf("wire calls = %d, want 0 for locally-rejected input", got)
	}
}

func TestClaimNextHonorsAPacedDeferral(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	raw, _ := json.Marshal(workflowv1.ClaimNextResponse{
		Issued: false,
		Deferral: workflowv1.ClaimPacingDeferral{
			Signal:     workflowv1.ClaimPacingSignalDomain,
			Domain:     "test-source/test-instance",
			RetryAfter: 2 * time.Second,
		},
	})
	fake.okJSON(workflowv1.OpClaimNext, string(raw))

	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, found, err := client.ClaimNext(context.Background(), validSelector(), time.Now().UTC(), time.Minute)
	if found {
		t.Fatal("found = true on a paced grant, want no claim issued")
	}
	var paced *PacedError
	if !errors.As(err, &paced) {
		t.Fatalf("err = %v (%T), want *PacedError", err, err)
	}
	if paced.RetryAfterDelay() != 2*time.Second {
		t.Fatalf("retry-after = %s, want 2s", paced.RetryAfterDelay())
	}
}

func TestClaimNextMapsNotFoundToNoWork(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	fake.fail(workflowv1.OpClaimNext, http.StatusNotFound, workflowv1.CodeNotFound, "no eligible work")

	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, found, err := client.ClaimNext(context.Background(), validSelector(), time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("found = true on not_found, want the empty-queue signal")
	}
}

func TestLifecycleOpsSendMutationsAndAcceptEmptySuccess(t *testing.T) {
	t.Parallel()
	ops := []workflowv1.Operation{
		workflowv1.OpHeartbeatClaim,
		workflowv1.OpCompleteClaim,
		workflowv1.OpReleaseClaim,
		workflowv1.OpFailClaimRetryable,
		workflowv1.OpFailClaimTerminal,
	}
	for _, op := range ops {
		op := op
		t.Run(string(op), func(t *testing.T) {
			t.Parallel()
			fake := newFakeWorkflow(t)
			fake.on(op, func(*http.Request) (int, string) { return http.StatusOK, `` })
			client, err := NewClient(testConfig(fake.server.URL))
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			var callErr error
			switch op {
			case workflowv1.OpHeartbeatClaim:
				callErr = client.Heartbeat(context.Background(), validMutation(now))
			case workflowv1.OpCompleteClaim:
				callErr = client.Complete(context.Background(), validMutation(now))
			case workflowv1.OpReleaseClaim:
				callErr = client.Release(context.Background(), validMutation(now))
			case workflowv1.OpFailClaimRetryable:
				callErr = client.FailRetryable(context.Background(), validMutation(now))
			case workflowv1.OpFailClaimTerminal:
				callErr = client.FailTerminal(context.Background(), validMutation(now))
			}
			if callErr != nil {
				t.Fatal(callErr)
			}
			if got := fake.calls(op); got != 1 {
				t.Fatalf("calls = %d, want 1", got)
			}
		})
	}
}

func TestCompleteMapsStaleFenceToATypedError(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	fake.fail(workflowv1.OpCompleteClaim, http.StatusConflict, workflowv1.CodeStaleFence, "epoch superseded")

	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	err = client.Complete(context.Background(), validMutation(time.Now().UTC()))
	var callErr *CallError
	if !errors.As(err, &callErr) {
		t.Fatalf("err = %v (%T), want *CallError", err, err)
	}
	if !callErr.Stale() {
		t.Fatalf("Stale() = false for code %q, want true", callErr.Code)
	}
}

func TestPinForCommitAcceptsAPinnedResponse(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	fake.okJSON(workflowv1.OpValidateAndPinClaimForCommit, `{"pinned":true,"commit_id":"commit-1"}`)

	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	pin, err := client.PinForCommit(context.Background(), workflowv1.CommitPinRequest{
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
		t.Fatalf("commit id = %q, want commit-1", pin.CommitID)
	}
}

func TestClientSendsTheProtocolVersionHeader(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	now := time.Now().UTC().Truncate(time.Second)
	fake.okJSON(workflowv1.OpClaimNext, mustGrantJSON(t, validGrant(now)))

	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.ClaimNext(context.Background(), validSelector(), now, time.Minute); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(fake.requests))
	}
	if got := fake.requests[0].version; got != "1" {
		t.Fatalf("version header = %q, want %q", got, "1")
	}
}
