// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/claim/fence"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// claimFencePinCall records one PinClaimForCommit call.
type claimFencePinCall struct {
	claimID      string
	token        int64
	scopeID      string
	sourceRunID  string
	generationID string
	deadline     time.Time
}

// claimFenceFakePinner answers pins from a per-call script (last entry
// repeats) and records every call. A nil script entry is a successful pin.
type claimFenceFakePinner struct {
	mu      sync.Mutex
	calls   []claimFencePinCall
	results []error
}

func (p *claimFenceFakePinner) PinClaimForCommit(
	_ context.Context, claimID string, token int64, scopeID, sourceRunID, generationID string, deadline time.Time,
) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, claimFencePinCall{claimID, token, scopeID, sourceRunID, generationID, deadline})
	if len(p.results) > 0 {
		index := min(len(p.calls)-1, len(p.results)-1)
		if err := p.results[index]; err != nil {
			return "", err
		}
	}
	return "commit-" + claimID, nil
}

func (p *claimFenceFakePinner) snapshot() []claimFencePinCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]claimFencePinCall(nil), p.calls...)
}

// claimFenceCodedError mimics a decoded claim-plane envelope error.
type claimFenceCodedError struct{ code string }

func (e claimFenceCodedError) Error() string { return "claim-plane pin: " + e.code }

func claimFenceTestCode(err error) string {
	var coded claimFenceCodedError
	if errors.As(err, &coded) {
		return coded.code
	}
	return ""
}

func claimFenceTestScope(id string) scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID: id, SourceSystem: "pagerduty", ScopeKind: scope.KindRepository,
		CollectorKind: scope.CollectorPagerDuty, PartitionKey: id,
	}
}

func claimFenceTestGeneration(scopeID, generationID string, at time.Time) scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: generationID, ScopeID: scopeID, ObservedAt: at, IngestedAt: at,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
	}
}

func claimFenceTestMutation(scopeID, generationID, claimID string, token int64) workflow.ClaimMutation {
	return workflow.ClaimMutation{
		WorkItemID: "pagerduty:i1:" + generationID, ClaimID: claimID, FencingToken: token,
		OwnerID: "owner", LeaseDuration: time.Minute, ObservedAt: time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC),
		ScopeID: scopeID, SourceRunID: generationID, GenerationID: generationID,
	}
}

// TestPlaneCommitPinFailureOpensNoTransaction is T7: a refused pin is terminal,
// an unanswered pin is retried with identical arguments and then retryable,
// and in every case no core transaction opens and the fact stream drains.
func TestPlaneCommitPinFailureOpensNoTransaction(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		err      error
		reason   fence.Reason
		terminal bool
		calls    int
	}{
		{"stale_fence", claimFenceCodedError{"stale_fence"}, fence.ReasonStaleFence, true, 1},
		{"not_found", claimFenceCodedError{"not_found"}, fence.ReasonNotFound, true, 1},
		{"transport", errors.New("dial tcp: i/o timeout"), fence.ReasonPinUnavailable, false, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			database := &fakeTransactionalDB{tx: &fakeTx{}}
			pinner := &claimFenceFakePinner{results: []error{tc.err}}
			store := NewIngestionStore(database)
			store.Now = func() time.Time { return now }
			store.ClaimPlane = &ClaimPlaneFence{Pinner: pinner, Code: claimFenceTestCode, PinBackoff: time.Microsecond}
			stream := make(chan facts.Envelope, 2)
			stream <- facts.Envelope{FactID: "f1"}
			stream <- facts.Envelope{FactID: "f2"}
			close(stream)

			err := store.CommitClaimedScopeGeneration(context.Background(),
				claimFenceTestMutation("scope-a", "gen-a", "claim-a", 3),
				claimFenceTestScope("scope-a"), claimFenceTestGeneration("scope-a", "gen-a", now), stream)

			var rejection *fence.Rejection
			if !errors.As(err, &rejection) || rejection.Reason != tc.reason || rejection.TerminalFailure() != tc.terminal {
				t.Fatalf("commit error = %v, want %s (terminal=%v)", err, tc.reason, tc.terminal)
			}
			if database.beginCalls != 0 {
				t.Fatalf("begin calls = %d, want 0 (no core transaction without a pin)", database.beginCalls)
			}
			if len(stream) != 0 {
				t.Fatalf("fact stream left %d undrained facts", len(stream))
			}
			calls := pinner.snapshot()
			if len(calls) != tc.calls {
				t.Fatalf("pin calls = %d, want %d", len(calls), tc.calls)
			}
			for _, call := range calls {
				if call != calls[0] {
					t.Fatalf("retried pin %+v differs from first %+v", call, calls[0])
				}
			}
			want := claimFencePinCall{"claim-a", 3, "scope-a", "gen-a", "gen-a", now.Add(time.Minute)}
			if calls[0] != want {
				t.Fatalf("pin call = %+v, want %+v", calls[0], want)
			}
		})
	}
}

// TestPlaneCommitRefusesDerivedScannerClaims is T11: the derived scanner
// heartbeat is not ported to the plane path, so a derived scanner claim is
// refused before any pin or transaction.
func TestPlaneCommitRefusesDerivedScannerClaims(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	database := &fakeTransactionalDB{tx: &fakeTx{}}
	pinner := &claimFenceFakePinner{}
	store := NewIngestionStore(database)
	store.Now = func() time.Time { return now }
	store.ClaimPlane = &ClaimPlaneFence{Pinner: pinner, Code: claimFenceTestCode}
	mutation := claimFenceTestMutation("scope-s", "gen-s", "claim-s", 1)
	mutation.DerivedScannerImage = true

	err := store.CommitClaimedScopeGeneration(context.Background(), mutation,
		claimFenceTestScope("scope-s"), claimFenceTestGeneration("scope-s", "gen-s", now), closedFactStream())
	if err == nil {
		t.Fatal("commit error = nil, want derived scanner refusal")
	}
	if database.beginCalls != 0 || len(pinner.snapshot()) != 0 {
		t.Fatalf("begin calls = %d, pin calls = %d, want 0 and 0", database.beginCalls, len(pinner.snapshot()))
	}
}

// TestPlaneCommitFailsClosedWithoutPinnerOrIdentity refuses a plane store
// with no pinner and a mutation missing the planned identity.
func TestPlaneCommitFailsClosedWithoutPinnerOrIdentity(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	for name, setup := range map[string]func(*IngestionStore, *workflow.ClaimMutation){
		"no pinner":           func(s *IngestionStore, _ *workflow.ClaimMutation) { s.ClaimPlane = &ClaimPlaneFence{} },
		"no planned identity": func(_ *IngestionStore, m *workflow.ClaimMutation) { m.SourceRunID = "" },
	} {
		database := &fakeTransactionalDB{tx: &fakeTx{}}
		store := NewIngestionStore(database)
		store.Now = func() time.Time { return now }
		store.ClaimPlane = &ClaimPlaneFence{Pinner: &claimFenceFakePinner{}, Code: claimFenceTestCode}
		mutation := claimFenceTestMutation("scope-c", "gen-c", "claim-c", 1)
		setup(&store, &mutation)
		err := store.CommitClaimedScopeGeneration(context.Background(), mutation,
			claimFenceTestScope("scope-c"), claimFenceTestGeneration("scope-c", "gen-c", now), closedFactStream())
		if err == nil || database.beginCalls != 0 {
			t.Fatalf("%s: commit error = %v, begin calls = %d, want refusal and no transaction", name, err, database.beginCalls)
		}
	}
}

func closedFactStream() <-chan facts.Envelope {
	stream := make(chan facts.Envelope)
	close(stream)
	return stream
}
