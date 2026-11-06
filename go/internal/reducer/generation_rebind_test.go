// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// rebindRecordingHandler records the generation each Handle call carried.
type rebindRecordingHandler struct {
	generations []string
	result      reducercontract.Result
}

func (h *rebindRecordingHandler) Handle(_ context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	h.generations = append(h.generations, intent.GenerationID)
	return h.result, nil
}

func rebindTestRuntime(t *testing.T, domain reducercontract.Domain, handler *rebindRecordingHandler) *Runtime {
	t.Helper()
	registry := NewRegistry()
	if err := registry.Register(DomainDefinition{
		Domain:        domain,
		Summary:       "test domain",
		Ownership:     OwnershipShape{CrossSource: true, CrossScope: true, CanonicalWrite: true},
		TruthContract: testTruthContract(string(domain)),
		Handler:       handler,
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	rt, err := NewRuntime(registry)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	return rt
}

func rebindTestIntent(domain reducercontract.Domain, generationID string) reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-10106",
		ScopeID:      "scope-10106",
		GenerationID: generationID,
		SourceSystem: "git",
		Domain:       domain,
		Cause:        "cross-scope crossplane xrd redrive",
		EntityKeys:   []string{"crossplane_satisfied_by_materialization:scope-10106"},
		EnqueuedAt:   time.Date(2026, time.November, 6, 11, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.November, 6, 11, 0, 0, 0, time.UTC),
		Status:       IntentStatusClaimed,
	}
}

// Only gen-active is current; every older generation is superseded.
func rebindFreshness() GenerationFreshnessCheck {
	return func(_ context.Context, _, generationID string) (bool, error) {
		return generationID == "gen-active", nil
	}
}

func TestRuntimeExecuteRebindsSupersededCrossplaneIntent(t *testing.T) {
	t.Parallel()
	handler := &rebindRecordingHandler{result: reducercontract.Result{
		IntentID: "intent-10106", Domain: reducercontract.DomainCrossplaneSatisfiedByMaterialization,
		Status: ResultStatusSucceeded, EvidenceSummary: "materialized 1 SATISFIED_BY edge(s)", CanonicalWrites: 1,
	}}
	rt := rebindTestRuntime(t, reducercontract.DomainCrossplaneSatisfiedByMaterialization, handler)
	rt.GenerationCheck = rebindFreshness()
	rt.ActiveGeneration = func(_ context.Context, _ string) (string, bool, error) {
		return "gen-active", true, nil
	}

	result, err := rt.execute(context.Background(), rebindTestIntent(
		reducercontract.DomainCrossplaneSatisfiedByMaterialization, "gen-full"))
	if err != nil {
		t.Fatalf("execute() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want succeeded", result.Status)
	}
	if len(handler.generations) != 1 || handler.generations[0] != "gen-active" {
		t.Fatalf("handler generations = %v, want [gen-active]", handler.generations)
	}
	if !strings.Contains(result.EvidenceSummary, "rebound from superseded generation gen-full to active generation gen-active") {
		t.Fatalf("EvidenceSummary = %q, want the rebind note", result.EvidenceSummary)
	}
}

func TestRuntimeExecuteDoesNotRebindOtherDomains(t *testing.T) {
	t.Parallel()
	handler := &rebindRecordingHandler{result: reducercontract.Result{Status: ResultStatusSucceeded}}
	rt := rebindTestRuntime(t, DomainWorkloadIdentity, handler)
	rt.GenerationCheck = rebindFreshness()
	rt.ActiveGeneration = func(_ context.Context, _ string) (string, bool, error) {
		return "gen-active", true, nil
	}

	result, err := rt.execute(context.Background(), rebindTestIntent(DomainWorkloadIdentity, "gen-full"))
	if err != nil {
		t.Fatalf("execute() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSuperseded {
		t.Fatalf("result.Status = %q, want superseded for a generation-scoped domain", result.Status)
	}
	if len(handler.generations) != 0 {
		t.Fatalf("handler generations = %v, want none", handler.generations)
	}
}

func TestRuntimeExecuteSupersededWithoutActiveGenerationLookup(t *testing.T) {
	t.Parallel()
	handler := &rebindRecordingHandler{result: reducercontract.Result{Status: ResultStatusSucceeded}}
	rt := rebindTestRuntime(t, reducercontract.DomainCrossplaneSatisfiedByMaterialization, handler)
	rt.GenerationCheck = rebindFreshness()
	// ActiveGeneration nil: the rebind is disabled and the pre-#10106
	// terminal superseded result stands.

	result, err := rt.execute(context.Background(), rebindTestIntent(
		reducercontract.DomainCrossplaneSatisfiedByMaterialization, "gen-full"))
	if err != nil {
		t.Fatalf("execute() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSuperseded || len(handler.generations) != 0 {
		t.Fatalf("result = %v handler %v, want superseded without a handler call", result.Status, handler.generations)
	}
}

func TestRuntimeExecuteRebindNoSuccessor(t *testing.T) {
	t.Parallel()
	for name, lookup := range map[string]ActiveGenerationID{
		"no active generation": func(context.Context, string) (string, bool, error) { return "", false, nil },
		"lookup agrees with intent": func(context.Context, string) (string, bool, error) {
			return "gen-full", true, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler := &rebindRecordingHandler{result: reducercontract.Result{Status: ResultStatusSucceeded}}
			rt := rebindTestRuntime(t, reducercontract.DomainCrossplaneSatisfiedByMaterialization, handler)
			rt.GenerationCheck = rebindFreshness()
			rt.ActiveGeneration = lookup

			result, err := rt.execute(context.Background(), rebindTestIntent(
				reducercontract.DomainCrossplaneSatisfiedByMaterialization, "gen-full"))
			if err != nil {
				t.Fatalf("execute() error = %v, want nil", err)
			}
			if result.Status != ResultStatusSuperseded || len(handler.generations) != 0 {
				t.Fatalf("result = %v handler %v, want superseded without a handler call", result.Status, handler.generations)
			}
		})
	}
}

func TestRuntimeExecuteRebindLookupErrorPropagates(t *testing.T) {
	t.Parallel()
	handler := &rebindRecordingHandler{result: reducercontract.Result{Status: ResultStatusSucceeded}}
	rt := rebindTestRuntime(t, reducercontract.DomainCrossplaneSatisfiedByMaterialization, handler)
	rt.GenerationCheck = rebindFreshness()
	lookupErr := errors.New("postgres connection refused")
	rt.ActiveGeneration = func(context.Context, string) (string, bool, error) {
		return "", false, lookupErr
	}

	_, err := rt.execute(context.Background(), rebindTestIntent(
		reducercontract.DomainCrossplaneSatisfiedByMaterialization, "gen-full"))
	if !errors.Is(err, lookupErr) {
		t.Fatalf("execute() error = %v, want wrapped %v", err, lookupErr)
	}
	if len(handler.generations) != 0 {
		t.Fatalf("handler generations = %v, want none", handler.generations)
	}
}

// TestRuntimeExecuteRebindChaseExhaustsToRetryable pins the churn bound:
// every rebound generation is itself superseded by the next check, so
// execute must return the retryable rebind-exhausted error (the queue
// re-runs the intent non-counting) rather than dropping it as superseded.
func TestRuntimeExecuteRebindChaseExhaustsToRetryable(t *testing.T) {
	t.Parallel()
	handler := &rebindRecordingHandler{result: reducercontract.Result{Status: ResultStatusSucceeded}}
	rt := rebindTestRuntime(t, reducercontract.DomainCrossplaneSatisfiedByMaterialization, handler)
	rt.GenerationCheck = func(context.Context, string, string) (bool, error) { return false, nil }
	next := 0
	rt.ActiveGeneration = func(context.Context, string) (string, bool, error) {
		next++
		return "gen-chase-" + string(rune('0'+next)), true, nil
	}

	result, err := rt.execute(context.Background(), rebindTestIntent(
		reducercontract.DomainCrossplaneSatisfiedByMaterialization, "gen-full"))
	if err == nil {
		t.Fatalf("execute() = %#v, nil error; want the rebind-exhausted deferral", result)
	}
	if result.Status == ResultStatusSuperseded {
		t.Fatal("execute() returned a superseded result under activation churn")
	}
	var exhausted reducercontract.GenerationRebindExhaustedError
	if !errors.As(err, &exhausted) {
		t.Fatalf("execute() error = %v, want GenerationRebindExhaustedError", err)
	}
	if !IsRetryable(err) {
		t.Fatalf("execute() error = %v, want retryable", err)
	}
	var classed interface{ FailureClass() string }
	if !errors.As(err, &classed) || classed.FailureClass() != reducercontract.GenerationActivationNotReadyFailureClass {
		t.Fatalf("execute() error = %v, want failure class %q", err, reducercontract.GenerationActivationNotReadyFailureClass)
	}
	if len(handler.generations) != 0 {
		t.Fatalf("handler generations = %v, want none while generations churn", handler.generations)
	}
}
