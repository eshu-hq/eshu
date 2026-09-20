// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package refresh

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/value"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
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
