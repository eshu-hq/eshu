// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// fakeSourcePacer reports every source paced or clear and records the
// consults it served.
type fakeSourcePacer struct {
	mu         sync.Mutex
	paced      bool
	retryAfter time.Duration
	// resumeBound is what ResumeBound reports.
	resumeBound time.Duration
	sources     []string
}

// ResumeBound lets the fake carry a configured resume bound, like the real
// pacer's release age.
func (f *fakeSourcePacer) ResumeBound() time.Duration { return f.resumeBound }

func (f *fakeSourcePacer) SourcePaced(_ context.Context, source string) (bool, time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sources = append(f.sources, source)
	return f.paced, f.retryAfter
}

func pacedTestWork() ScopeGenerationWork {
	return ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-123",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-123",
		},
		Generation: scope.ScopeGeneration{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
		AttemptCount: 1,
	}
}

// TestProcessWorkSourcePacedReleasesBeforeAnyWrite is #10882 F8: a claimed
// item whose source is slowed is released on the source-paced path after one
// consult, before the fact load, the write marker, and the canonical and
// content writes. N paced cycles issue zero writer calls and hold no worker
// in a wait; the release carries the grant delay as its retry hint.
func TestProcessWorkSourcePacedReleasesBeforeAnyWrite(t *testing.T) {
	t.Parallel()

	const cycles = 5
	pacer := &fakeSourcePacer{paced: true, retryAfter: 45 * time.Second, resumeBound: 15 * time.Minute}
	factStore := &stubFactStore{}
	runner := &stubProjectionRunner{}
	sink := &stubProjectorWorkSink{}
	marker := &fakeWriteMarker{}
	service := Service{
		FactStore:          factStore,
		Runner:             runner,
		WorkSink:           sink,
		DeltaBaselineFence: &fakeDeltaBaselineFence{},
		WriteMarker:        marker,
		SourcePacer:        pacer,
	}

	for i := 0; i < cycles; i++ {
		started := time.Now()
		if err := service.processWork(context.Background(), pacedTestWork(), 0); err != nil {
			t.Fatalf("cycle %d: processWork() error = %v, want nil", i, err)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("cycle %d: worker held for %s, want to return within one consult", i, elapsed)
		}
	}

	if len(pacer.sources) != cycles || pacer.sources[0] != "git" {
		t.Fatalf("consults = %v, want %d consults of the scope's source git", pacer.sources, cycles)
	}
	if factStore.loadCalls != 0 || runner.runCalls != 0 || marker.calls != 0 {
		t.Fatalf("paced work touched writers: load=%d project=%d marker=%d, want 0/0/0",
			factStore.loadCalls, runner.runCalls, marker.calls)
	}
	if sink.ackCalls != 0 || sink.failCalls != cycles {
		t.Fatalf("sink ack=%d fail=%d, want 0/%d", sink.ackCalls, sink.failCalls, cycles)
	}
	var paced failure.SourcePacedError
	if !errors.As(sink.failedWith, &paced) || !paced.IsSourcePaced() {
		t.Fatalf("Fail cause = %v, want failure.SourcePacedError", sink.failedWith)
	}
	if !failure.IsRetryable(sink.failedWith) {
		t.Fatal("source-paced cause is not retryable: Fail would dead-letter it")
	}
	if got := failure.RetryAfterHint(sink.failedWith); got != 45*time.Second {
		t.Fatalf("RetryAfterHint = %s, want the grant delay 45s", got)
	}
	if got := failure.ResumeBoundHint(sink.failedWith); got != 15*time.Minute {
		t.Fatalf("ResumeBoundHint = %s, want the pacer's resume bound 15m", got)
	}
}

// TestProcessWorkProjectsWhenSourceIsClear proves the gate is transparent: a
// clear verdict, and no pacer at all, both run the normal projection.
func TestProcessWorkProjectsWhenSourceIsClear(t *testing.T) {
	t.Parallel()

	for name, pacer := range map[string]SourcePacer{
		"clear verdict": &fakeSourcePacer{},
		"no pacer":      nil,
	} {
		runner := &stubProjectionRunner{}
		sink := &stubProjectorWorkSink{}
		service := Service{
			FactStore:          &stubFactStore{},
			Runner:             runner,
			WorkSink:           sink,
			Heartbeater:        &stubProjectorWorkHeartbeater{},
			DeltaBaselineFence: &fakeDeltaBaselineFence{},
			WriteMarker:        &fakeWriteMarker{},
			SourcePacer:        pacer,
		}
		if err := service.processWork(context.Background(), pacedTestWork(), 0); err != nil {
			t.Fatalf("%s: processWork() error = %v, want nil", name, err)
		}
		if runner.runCalls != 1 || sink.ackCalls != 1 || sink.failCalls != 0 {
			t.Fatalf("%s: project=%d ack=%d fail=%d, want 1/1/0", name, runner.runCalls, sink.ackCalls, sink.failCalls)
		}
	}
}
