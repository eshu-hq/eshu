// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package claim

// Contract proof against the pinned eshu-workflow v0.1.0 server package: the
// real HTTP handler serves the Client, so response shapes (the `{"ok":true}`
// mutation acknowledgement, the 404 empty-queue envelope, the 409 stale-fence
// envelope) come from the satellite and never from a hand-written fake. Only
// the storage interface is stubbed; the handler, routes, and encoding are the
// shipped ones.

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-workflow/server"
	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"
)

// stubStore implements only the claim-plane methods the Client uses. The
// embedded nil interface makes any other (control-plane) call panic, which
// would flag an unexpected route use.
type stubStore struct {
	server.ClaimStore

	mu        sync.Mutex
	mutations []string
	claimNext func(workflowv1.ClaimSelector, time.Time) (workflowv1.ClaimNextResponse, error)
	pin       func(workflowv1.CommitPinRequest) (workflowv1.CommitPinResponse, error)
	mutateErr error
}

func (s *stubStore) record(op string, m workflowv1.ClaimMutation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mutations = append(s.mutations, op+":"+m.ClaimID)
	return s.mutateErr
}

func (s *stubStore) ClaimNext(
	_ context.Context, sel workflowv1.ClaimSelector, now time.Time, _ time.Duration,
) (workflowv1.ClaimNextResponse, error) {
	return s.claimNext(sel, now)
}

func (s *stubStore) Heartbeat(_ context.Context, m workflowv1.ClaimMutation) error {
	return s.record("heartbeat", m)
}

func (s *stubStore) Complete(_ context.Context, m workflowv1.ClaimMutation) error {
	return s.record("complete", m)
}

func (s *stubStore) Release(_ context.Context, m workflowv1.ClaimMutation) error {
	return s.record("release", m)
}

func (s *stubStore) FailRetryable(_ context.Context, m workflowv1.ClaimMutation) error {
	return s.record("fail_retryable", m)
}

func (s *stubStore) FailTerminal(_ context.Context, m workflowv1.ClaimMutation) error {
	return s.record("fail_terminal", m)
}

func (s *stubStore) PinForCommit(
	_ context.Context, req workflowv1.CommitPinRequest,
) (workflowv1.CommitPinResponse, error) {
	return s.pin(req)
}

func realServerClient(t *testing.T, store *stubStore) *Client {
	t.Helper()
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)
	client, err := NewClient(testConfig(httpServer.URL))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestContract_RealServerAcksEveryLifecycleOperation(t *testing.T) {
	t.Parallel()
	store := &stubStore{}
	client := realServerClient(t, store)
	now := time.Now().UTC()
	ctx := context.Background()
	for name, call := range map[string]func(context.Context, workflowv1.ClaimMutation) error{
		"heartbeat":      client.Heartbeat,
		"complete":       client.Complete,
		"release":        client.Release,
		"fail_retryable": client.FailRetryable,
		"fail_terminal":  client.FailTerminal,
	} {
		if err := call(ctx, validMutation(now)); err != nil {
			t.Errorf("%s against the real v0.1.0 server = %v, want success on the ok acknowledgement", name, err)
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.mutations) != 5 {
		t.Fatalf("server applied %d mutations (%v), want 5", len(store.mutations), store.mutations)
	}
}

func TestContract_RealServerStaleFenceIsTyped(t *testing.T) {
	t.Parallel()
	store := &stubStore{mutateErr: &workflowv1.ProtocolError{Code: workflowv1.CodeStaleFence, Message: "epoch superseded"}}
	client := realServerClient(t, store)
	err := client.Complete(context.Background(), validMutation(time.Now().UTC()))
	var callErr *CallError
	if !errors.As(err, &callErr) || !callErr.Stale() {
		t.Fatalf("Complete() = %v, want a stale *CallError from the real server envelope", err)
	}
}

func TestContract_RealServerClaimNextIssuedAndIdle(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	store := &stubStore{}
	store.claimNext = func(workflowv1.ClaimSelector, time.Time) (workflowv1.ClaimNextResponse, error) {
		return workflowv1.ClaimNextResponse{Issued: true, Grant: validGrant(now)}, nil
	}
	client := realServerClient(t, store)
	item, found, err := client.ClaimNext(context.Background(), validSelector(), now, time.Minute)
	if err != nil || !found || item.Claim.ClaimID != "claim-1" {
		t.Fatalf("ClaimNext() = (%+v, %v, %v), want the issued grant", item, found, err)
	}

	store.claimNext = func(workflowv1.ClaimSelector, time.Time) (workflowv1.ClaimNextResponse, error) {
		return workflowv1.ClaimNextResponse{}, nil
	}
	_, found, err = client.ClaimNext(context.Background(), validSelector(), now, time.Minute)
	if err != nil || found {
		t.Fatalf("idle ClaimNext() = (found=%v, %v), want the empty-queue signal", found, err)
	}
}

func TestContract_RealServerPinForCommit(t *testing.T) {
	t.Parallel()
	store := &stubStore{}
	store.pin = func(workflowv1.CommitPinRequest) (workflowv1.CommitPinResponse, error) {
		return workflowv1.CommitPinResponse{Pinned: true, CommitID: "commit-1"}, nil
	}
	client := realServerClient(t, store)
	pin, err := client.PinForCommit(context.Background(), workflowv1.CommitPinRequest{
		ClaimID:        "claim-1",
		FencingToken:   9,
		ScopeID:        "scope-1",
		SourceRunID:    "sr-1",
		GenerationID:   "gen-1",
		CommitDeadline: time.Now().UTC().Add(time.Minute),
	})
	if err != nil || pin.CommitID != "commit-1" {
		t.Fatalf("PinForCommit() = (%+v, %v), want commit-1", pin, err)
	}
}
