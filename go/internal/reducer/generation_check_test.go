package reducer

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRuntimeExecuteSkipsStaleGeneration(t *testing.T) {
	t.Parallel()

	handler := &stubHandler{
		result: Result{
			IntentID:        "intent-1",
			Domain:          DomainWorkloadIdentity,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "wrote 3 canonical nodes",
			CanonicalWrites: 3,
		},
	}

	registry := NewRegistry()
	if err := registry.Register(DomainDefinition{
		Domain:  DomainWorkloadIdentity,
		Summary: "test domain",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: testTruthContract("workload_identity"),
		Handler:       handler,
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	rt, err := NewRuntime(registry)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	// Generation checker says this generation is stale.
	rt.GenerationCheck = func(_ context.Context, scopeID, generationID string) (bool, error) {
		if scopeID == "scope-123" && generationID == "old-gen" {
			return false, nil
		}
		return true, nil
	}

	intent := Intent{
		IntentID:        "intent-1",
		ScopeID:         "scope-123",
		GenerationID:    "old-gen",
		SourceSystem:    "git",
		Domain:          DomainWorkloadIdentity,
		Cause:           "test",
		EntityKeys:      []string{"workload:eshu"},
		RelatedScopeIDs: []string{"scope-999"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
		Status:          IntentStatusPending,
	}

	result, err := rt.execute(context.Background(), intent)
	if err != nil {
		t.Fatalf("execute() error = %v, want nil", err)
	}

	if result.Status != ResultStatusSuperseded {
		t.Fatalf("execute() result.Status = %q, want %q", result.Status, ResultStatusSuperseded)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("execute() result.CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
	if handler.handleCalls != 0 {
		t.Fatalf("handler was called %d times, want 0 (stale intent should be skipped)", handler.handleCalls)
	}
}

func TestRuntimeExecuteProceedsForCurrentGeneration(t *testing.T) {
	t.Parallel()

	handler := &stubHandler{
		result: Result{
			IntentID:        "intent-1",
			Domain:          DomainWorkloadIdentity,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "wrote 3 canonical nodes",
			CanonicalWrites: 3,
		},
	}

	registry := NewRegistry()
	if err := registry.Register(DomainDefinition{
		Domain:  DomainWorkloadIdentity,
		Summary: "test domain",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: testTruthContract("workload_identity"),
		Handler:       handler,
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	rt, err := NewRuntime(registry)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	// Generation checker says this generation is current.
	rt.GenerationCheck = func(_ context.Context, _, _ string) (bool, error) {
		return true, nil
	}

	intent := Intent{
		IntentID:        "intent-1",
		ScopeID:         "scope-123",
		GenerationID:    "current-gen",
		SourceSystem:    "git",
		Domain:          DomainWorkloadIdentity,
		Cause:           "test",
		EntityKeys:      []string{"workload:eshu"},
		RelatedScopeIDs: []string{"scope-999"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
		Status:          IntentStatusPending,
	}

	result, err := rt.execute(context.Background(), intent)
	if err != nil {
		t.Fatalf("execute() error = %v, want nil", err)
	}

	if result.Status != ResultStatusSucceeded {
		t.Fatalf("execute() result.Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if result.CanonicalWrites != 3 {
		t.Fatalf("execute() result.CanonicalWrites = %d, want 3", result.CanonicalWrites)
	}
	if handler.handleCalls != 1 {
		t.Fatalf("handler was called %d times, want 1", handler.handleCalls)
	}
}

func TestRuntimeExecuteProceedsWhenGenerationCheckNil(t *testing.T) {
	t.Parallel()

	handler := &stubHandler{
		result: Result{
			IntentID:        "intent-1",
			Domain:          DomainWorkloadIdentity,
			Status:          ResultStatusSucceeded,
			CanonicalWrites: 5,
		},
	}

	registry := NewRegistry()
	if err := registry.Register(DomainDefinition{
		Domain:  DomainWorkloadIdentity,
		Summary: "test domain",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: testTruthContract("workload_identity"),
		Handler:       handler,
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	rt, err := NewRuntime(registry)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	// GenerationCheck is nil (default) — guard disabled.

	intent := Intent{
		IntentID:        "intent-1",
		ScopeID:         "scope-123",
		GenerationID:    "any-gen",
		SourceSystem:    "git",
		Domain:          DomainWorkloadIdentity,
		Cause:           "test",
		EntityKeys:      []string{"workload:eshu"},
		RelatedScopeIDs: []string{"scope-999"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
		Status:          IntentStatusPending,
	}

	result, err := rt.execute(context.Background(), intent)
	if err != nil {
		t.Fatalf("execute() error = %v, want nil", err)
	}

	if handler.handleCalls != 1 {
		t.Fatalf("handler was called %d times, want 1 (nil checker should not block)", handler.handleCalls)
	}
	if result.CanonicalWrites != 5 {
		t.Fatalf("execute() result.CanonicalWrites = %d, want 5", result.CanonicalWrites)
	}
}

func TestRuntimeExecutePropagatesGenerationCheckError(t *testing.T) {
	t.Parallel()

	handler := &stubHandler{
		result: Result{Status: ResultStatusSucceeded},
	}

	registry := NewRegistry()
	if err := registry.Register(DomainDefinition{
		Domain:  DomainWorkloadIdentity,
		Summary: "test domain",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: testTruthContract("workload_identity"),
		Handler:       handler,
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	rt, err := NewRuntime(registry)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	checkErr := errors.New("postgres connection refused")
	rt.GenerationCheck = func(_ context.Context, _, _ string) (bool, error) {
		return false, checkErr
	}

	intent := Intent{
		IntentID:        "intent-1",
		ScopeID:         "scope-123",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadIdentity,
		Cause:           "test",
		EntityKeys:      []string{"workload:eshu"},
		RelatedScopeIDs: []string{"scope-999"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
		Status:          IntentStatusPending,
	}

	_, err = rt.execute(context.Background(), intent)
	if err == nil {
		t.Fatal("execute() error = nil, want non-nil")
	}
	if !errors.Is(err, checkErr) {
		t.Fatalf("execute() error = %v, want wrapped %v", err, checkErr)
	}
	if handler.handleCalls != 0 {
		t.Fatalf("handler was called %d times, want 0 (error should prevent execution)", handler.handleCalls)
	}
}

// stubHandler is a minimal Handler for generation guard tests.
type stubHandler struct {
	handleCalls int
	result      Result
	err         error
}

func (h *stubHandler) Handle(_ context.Context, _ Intent) (Result, error) {
	h.handleCalls++
	return h.result, h.err
}
