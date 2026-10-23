// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func unionLoadWork() ScopeGenerationWork {
	observed := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	return ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID: "scope-8779", SourceSystem: "git", ScopeKind: scope.KindRepository,
			CollectorKind: scope.CollectorGit, PartitionKey: "repo-8779",
		},
		Generation: scope.ScopeGeneration{
			GenerationID: "gen-delta", ScopeID: "scope-8779", ObservedAt: observed, IngestedAt: observed,
			Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		},
	}
}

// #8779: the union base id travels out of the load that built the overlay
// onto the generation Project receives; nothing re-resolves it.
func TestProjectionFactsProjectionGeneration(t *testing.T) {
	t.Parallel()
	claimed := unionLoadWork().Generation

	single, err := ProjectionFacts{}.ProjectionGeneration(claimed)
	if err != nil || single.DeltaUnionBaseGenerationID != "" || single.IsDelta {
		t.Fatalf("single-generation load = %+v, %v; want the claimed generation unchanged", single, err)
	}

	union, err := ProjectionFacts{UnionBaseGenerationID: "gen-base", UnionOutcome: DeltaUnionBaseFound}.ProjectionGeneration(claimed)
	if err != nil {
		t.Fatalf("union load error = %v, want nil", err)
	}
	if union.DeltaUnionBaseGenerationID != "gen-base" || !union.IsDelta || union.GenerationID != claimed.GenerationID {
		t.Fatalf("union load generation = %+v, want IsDelta and union base gen-base on %s", union, claimed.GenerationID)
	}

	if _, err := (ProjectionFacts{UnionBaseGenerationID: "gen-base", UnionOutcome: DeltaUnionScopeMismatch}).
		ProjectionGeneration(claimed); err == nil || !strings.Contains(err.Error(), string(DeltaUnionScopeMismatch)) {
		t.Fatalf("scope-mismatch union load error = %v, want a refusal naming %s", err, DeltaUnionScopeMismatch)
	}
}

func TestServiceProjectsDeltaWithUnionBaseFromLoad(t *testing.T) {
	t.Parallel()
	runner := &stubProjectionRunner{}
	sink := &stubProjectorWorkSink{}
	service := Service{
		PollInterval:       time.Millisecond,
		WorkSource:         &stubProjectorWorkSource{workItems: []ScopeGenerationWork{unionLoadWork()}},
		FactStore:          &stubFactStore{unionBase: "gen-base", unionOutcome: DeltaUnionBaseFound},
		Runner:             runner,
		WorkSink:           sink,
		DeltaBaselineFence: &fakeDeltaBaselineFence{},
		WriteMarker:        &fakeWriteMarker{},
		Wait:               func(context.Context, time.Duration) error { return context.Canceled },
	}
	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if runner.runCalls != 1 || sink.ackCalls != 1 {
		t.Fatalf("project calls = %d, ack calls = %d; want 1 and 1", runner.runCalls, sink.ackCalls)
	}
	if got := runner.lastGeneration; got.DeltaUnionBaseGenerationID != "gen-base" || !got.IsDelta {
		t.Fatalf("Project generation = %+v, want IsDelta with union base gen-base", got)
	}
}

func TestServiceFailsUnprojectableUnionLoadBeforeProject(t *testing.T) {
	t.Parallel()
	runner := &stubProjectionRunner{}
	sink := &stubProjectorWorkSink{}
	marker := &fakeWriteMarker{}
	service := Service{
		PollInterval:       time.Millisecond,
		WorkSource:         &stubProjectorWorkSource{workItems: []ScopeGenerationWork{unionLoadWork()}},
		FactStore:          &stubFactStore{unionBase: "gen-base", unionOutcome: DeltaUnionScopeMismatch},
		Runner:             runner,
		WorkSink:           sink,
		DeltaBaselineFence: &fakeDeltaBaselineFence{},
		WriteMarker:        marker,
		Wait:               func(context.Context, time.Duration) error { return context.Canceled },
	}
	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if runner.runCalls != 0 || sink.failCalls != 1 || sink.ackCalls != 0 || marker.calls != 0 {
		t.Fatalf("project=%d fail=%d ack=%d write-marks=%d, want 0, 1, 0, 0 (refused before any write)",
			runner.runCalls, sink.failCalls, sink.ackCalls, marker.calls)
	}
	if sink.failedWith == nil || !strings.Contains(sink.failedWith.Error(), string(DeltaUnionScopeMismatch)) {
		t.Fatalf("failed with %v, want the scope-mismatch refusal", sink.failedWith)
	}
}
