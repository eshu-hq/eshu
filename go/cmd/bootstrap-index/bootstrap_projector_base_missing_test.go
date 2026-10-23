// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// #8779 review F3: a delta whose #7797 union base is missing must retire the
// same way in the bootstrap-index loop as in the projector service, through
// the delta-baseline fence as refused_base_missing (supersede plus the
// reindex watermark the Postgres fence records), never through the generic
// Fail path, which would retry a projection that cannot succeed.

// baseMissingBootstrapFactStore fails every load with the union refusal.
type baseMissingBootstrapFactStore struct{}

func (baseMissingBootstrapFactStore) LoadProjectionFacts(_ context.Context, work projector.ScopeGenerationWork) (projector.ProjectionFacts, error) {
	return projector.ProjectionFacts{}, &projector.DeltaBaseMissingError{
		ScopeID: work.Scope.ScopeID, GenerationID: work.Generation.GenerationID, BaselineCommitSHA: "A",
	}
}

// baseMissingBootstrapFence passes the preflight (the delta's baseline is
// the active commit) and records every refusal, answering as the Postgres
// fence does for a refusal it marked.
type baseMissingBootstrapFence struct {
	mu       sync.Mutex
	refusals []projector.DeltaBaselineRefusal
}

func (f *baseMissingBootstrapFence) ReadDeltaBaseline(context.Context, projector.ScopeGenerationWork) (projector.DeltaBaselineState, error) {
	return projector.DeltaBaselineState{
		TargetFound: true, TargetStatus: scope.GenerationStatusPending, IsDelta: true,
		BaselineCommitSHA: "A", ActiveGenerationID: "gen-full-a", ActiveCommitSHA: "A",
	}, nil
}

func (f *baseMissingBootstrapFence) RefuseDeltaBaseline(_ context.Context, work projector.ScopeGenerationWork, r projector.DeltaBaselineRefusal) error {
	f.mu.Lock()
	f.refusals = append(f.refusals, r)
	f.mu.Unlock()
	return fmt.Errorf("delta generation %s refused: %w", work.Generation.GenerationID, failure.ErrWorkSuperseded)
}

func TestDrainProjectorRefusesDeltaBaseMissingThroughFence(t *testing.T) {
	t.Parallel()
	for _, workers := range []int{1, 4} {
		fence := &baseMissingBootstrapFence{}
		runner := &countingBootstrapRunner{}
		sink := &countingBootstrapSink{}
		err := drainProjector(context.Background(), &concurrentWorkSource{items: bootstrapDeltaItems(3)},
			baseMissingBootstrapFactStore{}, runner, sink, fence, passBootstrapBaselineFence{}, nil, 0, workers, nil, nil, nil)
		if err != nil {
			t.Fatalf("workers=%d drainProjector() = %v, want nil (a refused delta is retired, not failed)", workers, err)
		}
		if runner.calls.Load() != 0 || sink.acked.Load() != 0 || sink.failed.Load() != 0 {
			t.Fatalf("workers=%d project=%d ack=%d fail=%d, want 0 0 0 (refused through the fence, not Fail)",
				workers, runner.calls.Load(), sink.acked.Load(), sink.failed.Load())
		}
		if len(fence.refusals) != 3 {
			t.Fatalf("workers=%d fence refusals = %d, want 3", workers, len(fence.refusals))
		}
		for _, refusal := range fence.refusals {
			if refusal.Outcome != projector.DeltaBaselineRefusedBaseMissing ||
				refusal.Phase != projector.DeltaBaselinePhasePreflight ||
				refusal.FailureClass() != projector.DeltaBaselineBaseMissingClass ||
				refusal.State.BaselineCommitSHA != "A" || refusal.State.ActiveGenerationID != "gen-full-a" {
				t.Fatalf("workers=%d refusal = %+v, want a preflight %s refusal at baseline A naming the active generation",
					workers, refusal, projector.DeltaBaselineRefusedBaseMissing)
			}
		}
	}
}
