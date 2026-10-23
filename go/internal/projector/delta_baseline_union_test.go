// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestDeltaBaselineRefusedBaseMissingIsRefusedAndCounted(t *testing.T) {
	if !DeltaBaselineRefusedBaseMissing.Refused() {
		t.Fatal("DeltaBaselineRefusedBaseMissing.Refused() = false, want true")
	}
	refusal := DeltaBaselineRefusal{Phase: DeltaBaselinePhasePreflight, Outcome: DeltaBaselineRefusedBaseMissing}
	if got, want := refusal.FailureClass(), DeltaBaselineBaseMissingClass; got != want {
		t.Fatalf("FailureClass() = %q, want %q", got, want)
	}
}

func TestDeltaBaseMissingErrorCarriesScopeAndBaseline(t *testing.T) {
	err := &DeltaBaseMissingError{
		ScopeID:           "scope-1",
		GenerationID:      "gen-delta-1",
		BaselineCommitSHA: "abc123",
	}
	if err.Error() == "" {
		t.Fatal("Error() is empty")
	}
	var target *DeltaBaseMissingError
	if !errors.As(err, &target) {
		t.Fatal("errors.As DeltaBaseMissingError = false, want true")
	}
	if target.ScopeID != "scope-1" || target.GenerationID != "gen-delta-1" || target.BaselineCommitSHA != "abc123" {
		t.Fatalf("error fields = %+v, want scope/generation/baseline", target)
	}
	// A base-missing refusal is not itself a superseded error: the service
	// converts it through the fence, which marks superseded.
	if errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatal("errors.Is ErrWorkSuperseded = true, want false")
	}
}

func TestIsDeltaBaselineRefusalIncludesBaseMissing(t *testing.T) {
	if !IsDeltaBaselineRefusal(classedRefusal{DeltaBaselineBaseMissingClass}) {
		t.Fatal("IsDeltaBaselineRefusal(base missing) = false, want true")
	}
}

// baseMissingFactStore fails every load with a union base-missing refusal.
type baseMissingFactStore struct{}

func (baseMissingFactStore) LoadProjectionFacts(context.Context, ScopeGenerationWork) (ProjectionFacts, error) {
	return ProjectionFacts{}, &DeltaBaseMissingError{
		ScopeID:           "scope-7797",
		GenerationID:      "gen-delta-1",
		BaselineCommitSHA: "baseline-commit",
	}
}

// A union base-missing load failure refuses through the fence with the new
// outcome instead of failing the work: the graph stays untouched, nothing
// is projected, and the generation is marked superseded for a full re-parse.
func TestProcessWorkRefusesDeltaBaseMissingThroughFence(t *testing.T) {
	t.Parallel()

	fence := &fakeDeltaBaselineFence{}
	runner := &stubProjectionRunner{}
	sink := &stubProjectorWorkSink{}
	service := Service{
		PollInterval:       10 * time.Millisecond,
		WorkSource:         &stubProjectorWorkSource{},
		FactStore:          baseMissingFactStore{},
		Runner:             runner,
		WorkSink:           sink,
		DeltaBaselineFence: fence,
		WriteMarker:        &fakeWriteMarker{},
	}
	work := ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-7797", ScopeKind: scope.KindRepository},
		Generation:   scope.ScopeGeneration{ScopeID: "scope-7797", GenerationID: "gen-delta-1"},
		AttemptCount: 1,
	}
	if err := service.processWork(context.Background(), work, 0); err != nil {
		t.Fatalf("processWork() = %v, want nil (refused deltas drop)", err)
	}
	fence.mu.Lock()
	defer fence.mu.Unlock()
	if got, want := len(fence.refusals), 1; got != want {
		t.Fatalf("fence refusals = %d, want %d", got, want)
	}
	refusal := fence.refusals[0]
	if refusal.Outcome != DeltaBaselineRefusedBaseMissing {
		t.Fatalf("refusal outcome = %q, want %q", refusal.Outcome, DeltaBaselineRefusedBaseMissing)
	}
	if refusal.Phase != DeltaBaselinePhasePreflight {
		t.Fatalf("refusal phase = %q, want preflight", refusal.Phase)
	}
	if refusal.State.BaselineCommitSHA != "baseline-commit" {
		t.Fatalf("refusal baseline = %q, want the union baseline", refusal.State.BaselineCommitSHA)
	}
	if runner.runCalls != 0 {
		t.Fatalf("project calls = %d, want 0 (nothing projected)", runner.runCalls)
	}
	if sink.failCalls != 0 || sink.ackCalls != 0 {
		t.Fatalf("fail calls = %d, ack calls = %d, want 0 and 0", sink.failCalls, sink.ackCalls)
	}
}
