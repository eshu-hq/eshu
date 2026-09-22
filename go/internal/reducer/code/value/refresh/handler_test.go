// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package refresh

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/value"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// fakeFixpointProjector records the scope/generation each fixpoint run sees.
type fakeFixpointProjector struct {
	result value.FixpointProjectionResult
	err    error
	calls  [][2]string
}

func (f *fakeFixpointProjector) ProjectValueFlowFixpointEvidence(
	_ context.Context,
	scopeID, generationID string,
) (value.FixpointProjectionResult, error) {
	f.calls = append(f.calls, [2]string{scopeID, generationID})
	return f.result, f.err
}

func refreshIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-refresh-1",
		ScopeID:      "eshu:global",
		GenerationID: "eshu:global:genesis",
		Domain:       reducercontract.DomainCodeValueFlowRefresh,
	}
}

// TestHandlerRunsFixpointOnly is the RED test for #6785 PR B: the refresh
// handler re-runs the global value-flow fixpoint without re-persisting
// summaries, sources, or graph ids (the producers change only graph edges).
func TestHandlerRunsFixpointOnly(t *testing.T) {
	t.Parallel()

	fixpoint := &fakeFixpointProjector{
		result: value.FixpointProjectionResult{FindingCount: 1, GraphRows: 1},
	}
	h := Handler{Fixpoint: fixpoint}
	result, err := h.Handle(context.Background(), refreshIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(fixpoint.calls) != 1 {
		t.Fatalf("fixpoint runs = %d, want 1", len(fixpoint.calls))
	}
	if got := fixpoint.calls[0]; got != [2]string{"eshu:global", "eshu:global:genesis"} {
		t.Errorf("fixpoint ran on %q, want the intent scope/generation", got)
	}
	if result.Domain != reducercontract.DomainCodeValueFlowRefresh {
		t.Errorf("result domain = %q, want %q", result.Domain, reducercontract.DomainCodeValueFlowRefresh)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Errorf("result status = %q, want succeeded", result.Status)
	}
	if result.CanonicalWrites != 1 {
		t.Errorf("canonical writes = %d, want the fixpoint graph rows", result.CanonicalWrites)
	}
}

func TestHandlerRejectsOtherDomains(t *testing.T) {
	t.Parallel()

	h := Handler{Fixpoint: &fakeFixpointProjector{}}
	intent := refreshIntent()
	intent.Domain = reducercontract.DomainCodeFunctionSummary
	if _, err := h.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for a non-refresh domain")
	}
}

func TestHandlerRequiresProjector(t *testing.T) {
	t.Parallel()

	if _, err := (Handler{}).Handle(context.Background(), refreshIntent()); err == nil {
		t.Fatal("expected error without a fixpoint projector")
	}
}

func TestHandlerPropagatesFixpointError(t *testing.T) {
	t.Parallel()

	boom := errors.New("fixpoint exploded")
	h := Handler{Fixpoint: &fakeFixpointProjector{err: boom}}
	if _, err := h.Handle(context.Background(), refreshIntent()); !errors.Is(err, boom) {
		t.Fatalf("Handle() error = %v, want %v", err, boom)
	}
}

// fakeInputsLiveness returns a fixed pending set for the #6923 fence read.
type fakeInputsLiveness struct {
	pending []string
	err     error
	calls   int
}

func (f *fakeInputsLiveness) PendingValueFlowInputs(context.Context) ([]string, error) {
	f.calls++
	return f.pending, f.err
}

// TestHandlerRefusesWhileInputsUndrained is the RED test for #6923: the
// refresh singleton must refuse — Retryable, non-counting
// value_flow_inputs_not_ready — before touching the fixpoint projector at
// all while its input fence reports a pending writer of the cloud-sink
// chain.
func TestHandlerRefusesWhileInputsUndrained(t *testing.T) {
	t.Parallel()

	fixpoint := &fakeFixpointProjector{}
	liveness := &fakeInputsLiveness{pending: []string{"scope-a/gen-1=code_function_summary:running"}}
	now := time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC)
	h := Handler{
		Fixpoint:       fixpoint,
		InputsLiveness: liveness,
		Now:            func() time.Time { return now },
	}

	_, err := h.Handle(context.Background(), refreshIntent())
	if err == nil {
		t.Fatal("Handle() error = nil, want a fence refusal")
	}
	var retryable interface{ Retryable() bool }
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("Handle() error = %v, want Retryable", err)
	}
	var classed interface{ FailureClass() string }
	if !errors.As(err, &classed) || classed.FailureClass() != crossscope.ValueFlowInputsNotReadyFailureClass {
		t.Fatalf("Handle() error = %v, want failure class %q", err, crossscope.ValueFlowInputsNotReadyFailureClass)
	}
	if len(fixpoint.calls) != 0 {
		t.Fatalf("fixpoint runs = %d, want 0 (no load before the fence clears)", len(fixpoint.calls))
	}
	if liveness.calls != 1 {
		t.Fatalf("fence reads = %d, want 1", liveness.calls)
	}
}

// TestHandlerSolvesWhenBoundExpired is the RED test for #6923's starvation
// bound: once elapsed time since the singleton's own cycle anchor reaches
// crossscope.ProducerReadinessMaxWait, the handler solves anyway (outcome
// abandoned) instead of deferring forever behind a writer that never drains.
func TestHandlerSolvesWhenBoundExpired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC)
	fixpoint := &fakeFixpointProjector{result: value.FixpointProjectionResult{GraphRows: 2}}
	liveness := &fakeInputsLiveness{pending: []string{"scope-a/gen-1=code_function_summary:running"}}
	h := Handler{
		Fixpoint:       fixpoint,
		InputsLiveness: liveness,
		Now:            func() time.Time { return now },
	}
	intent := refreshIntent()
	intent.CycleStartedAt = now.Add(-31 * time.Minute)

	result, err := h.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil (bound expired, solve anyway)", err)
	}
	if len(fixpoint.calls) != 1 {
		t.Fatalf("fixpoint runs = %d, want 1", len(fixpoint.calls))
	}
	if liveness.calls != 1 {
		t.Fatalf("fence reads = %d, want 1", liveness.calls)
	}
	if result.CanonicalWrites != 2 {
		t.Fatalf("canonical writes = %d, want 2", result.CanonicalWrites)
	}
}
