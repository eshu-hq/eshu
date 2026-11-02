// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fence

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// pinCall records one PinClaimForCommit invocation.
type pinCall struct {
	claimID      string
	token        int64
	scopeID      string
	sourceRunID  string
	generationID string
	deadline     time.Time
}

// fakePinner returns scripted results in call order; once the script is used
// up it repeats the last entry.
type fakePinner struct {
	mu      sync.Mutex
	calls   []pinCall
	results []error
}

func (f *fakePinner) PinClaimForCommit(
	_ context.Context,
	claimID string,
	fencingToken int64,
	scopeID, sourceRunID, generationID string,
	commitDeadline time.Time,
) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, pinCall{claimID, fencingToken, scopeID, sourceRunID, generationID, commitDeadline})
	var err error
	if len(f.results) > 0 {
		index := len(f.calls) - 1
		if index >= len(f.results) {
			index = len(f.results) - 1
		}
		err = f.results[index]
	}
	if err != nil {
		return "", err
	}
	return "commit-" + claimID, nil
}

func (f *fakePinner) snapshot() []pinCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]pinCall(nil), f.calls...)
}

// codedError carries a protocol code the way the claim-plane client does.
type codedError struct{ code string }

func (e codedError) Error() string { return "claim-plane pin: " + e.code }

func testCode(err error) string {
	var coded codedError
	if errors.As(err, &coded) {
		return coded.code
	}
	return ""
}

func testRequest() Request {
	return Request{
		ClaimID: "claim-1", FencingToken: 4, WorkItemID: "item-1",
		ScopeID: "scope-1", SourceRunID: "run-1", GenerationID: "gen-1",
	}
}

func TestRequestForRequiresPlannedIdentity(t *testing.T) {
	mutation := workflow.ClaimMutation{
		WorkItemID: "item-1", ClaimID: "claim-1", FencingToken: 2,
		ScopeID: "scope-1", SourceRunID: "run-1", GenerationID: "gen-1",
		LeaseDuration: time.Minute,
	}
	request, err := RequestFor(mutation)
	if err != nil {
		t.Fatalf("RequestFor() error = %v", err)
	}
	if request.ScopeID != "scope-1" || request.SourceRunID != "run-1" || request.GenerationID != "gen-1" ||
		request.WorkItemID != "item-1" || request.FencingToken != 2 || request.Lease != time.Minute {
		t.Fatalf("RequestFor() = %+v", request)
	}
	for name, edit := range map[string]func(*workflow.ClaimMutation){
		"scope":      func(m *workflow.ClaimMutation) { m.ScopeID = " " },
		"source_run": func(m *workflow.ClaimMutation) { m.SourceRunID = "" },
		"generation": func(m *workflow.ClaimMutation) { m.GenerationID = "" },
		"lease":      func(m *workflow.ClaimMutation) { m.LeaseDuration = 0 },
		"token":      func(m *workflow.ClaimMutation) { m.FencingToken = 0 },
	} {
		broken := mutation
		edit(&broken)
		if _, err := RequestFor(broken); err == nil {
			t.Fatalf("RequestFor(missing %s) error = nil, want refusal", name)
		}
	}
}

func TestPinStaleAndNotFoundAreTerminalWithoutRetry(t *testing.T) {
	for _, code := range []string{"stale_fence", "lease_expired", "claim_conflict", "not_found"} {
		t.Run(code, func(t *testing.T) {
			pinner := &fakePinner{results: []error{codedError{code}}}
			_, err := Pin(context.Background(), pinner, testCode, testRequest(), time.Unix(100, 0), 3, 0)
			var rejection *Rejection
			if !errors.As(err, &rejection) {
				t.Fatalf("Pin() error = %v, want *Rejection", err)
			}
			want := ReasonStaleFence
			if code == "not_found" {
				want = ReasonNotFound
			}
			if rejection.Reason != want || !rejection.TerminalFailure() {
				t.Fatalf("rejection = %+v terminal=%v, want reason %s terminal", rejection, rejection.TerminalFailure(), want)
			}
			if got := len(pinner.snapshot()); got != 1 {
				t.Fatalf("pin calls = %d, want 1 (no retry on a refusal)", got)
			}
		})
	}
}

func TestPinRetriesTransportWithIdenticalArgumentsThenUnavailable(t *testing.T) {
	pinner := &fakePinner{results: []error{errors.New("dial tcp: i/o timeout")}}
	deadline := time.Unix(200, 0)
	_, err := Pin(context.Background(), pinner, testCode, testRequest(), deadline, 3, 0)
	var rejection *Rejection
	if !errors.As(err, &rejection) || rejection.Reason != ReasonPinUnavailable {
		t.Fatalf("Pin() error = %v, want pin_unavailable rejection", err)
	}
	if rejection.TerminalFailure() {
		t.Fatal("pin_unavailable must be retryable")
	}
	calls := pinner.snapshot()
	if len(calls) != 3 {
		t.Fatalf("pin calls = %d, want 3", len(calls))
	}
	for i, call := range calls {
		if call != calls[0] {
			t.Fatalf("call %d = %+v, want identical to %+v", i, call, calls[0])
		}
	}
	if calls[0].deadline != deadline || calls[0].generationID != "gen-1" || calls[0].token != 4 {
		t.Fatalf("pin arguments = %+v", calls[0])
	}
}

func TestPinSucceedsAfterTransientFailure(t *testing.T) {
	pinner := &fakePinner{results: []error{errors.New("reset"), nil}}
	commitID, err := Pin(context.Background(), pinner, testCode, testRequest(), time.Unix(300, 0), 3, 0)
	if err != nil || commitID != "commit-claim-1" {
		t.Fatalf("Pin() = (%q, %v), want commit-claim-1", commitID, err)
	}
}

func TestCheckDeadlineRefusesInsideMargin(t *testing.T) {
	latest := time.Unix(1000, 0)
	margin := 5 * time.Second
	if slack, err := CheckDeadline(latest, latest.Add(-margin), margin); err != nil || slack != margin {
		t.Fatalf("CheckDeadline(at margin) = (%v, %v), want (%v, nil)", slack, err, margin)
	}
	slack, err := CheckDeadline(latest, latest.Add(-margin+time.Nanosecond), margin)
	var rejection *Rejection
	if !errors.As(err, &rejection) || rejection.Reason != ReasonDeadline || rejection.TerminalFailure() {
		t.Fatalf("CheckDeadline(inside margin) error = %v, want retryable deadline rejection", err)
	}
	if slack != margin-time.Nanosecond {
		t.Fatalf("slack = %v, want %v", slack, margin-time.Nanosecond)
	}
}

func TestReasonsIsTheClosedSet(t *testing.T) {
	want := []Reason{ReasonStaleFence, ReasonNotFound, ReasonPinUnavailable, ReasonDeadline, ReasonFenceToken, ReasonFenceClaimMismatch}
	got := Reasons()
	if len(got) != len(want) {
		t.Fatalf("Reasons() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Reasons() = %v, want %v", got, want)
		}
	}
}
