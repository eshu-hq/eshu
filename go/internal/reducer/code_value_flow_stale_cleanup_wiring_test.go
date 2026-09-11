// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestServiceStartsCodeValueFlowStaleCleanupRunner is the
// Service.startSideRunners wiring case for the value-flow stale cleanup
// runner (moved to [cleanup], go/internal/reducer/code/value/cleanup, under
// issue #6061). It stays in root because it drives Service.startSideRunners,
// which only the reducer root can construct. Its fakes are local, minimal
// copies of the [cleanup] package's own test fakes: Go test files cannot
// share unexported symbols across a package boundary (issue #6061).
func TestServiceStartsCodeValueFlowStaleCleanupRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &wiringFakeCodeValueFlowCurrentGenerationReader{
		rows: []CodeValueFlowCurrentGeneration{{ScopeID: "scope-a", GenerationID: "gen-a"}},
	}
	taintSweeper := &wiringRecordingCodeValueFlowTaintSweeper{}
	interproc := &wiringRecordingCodeValueFlowInterprocSweeper{}
	started := make(chan struct{}, 1)
	runner := &CodeValueFlowStaleCleanupRunner{
		CurrentGenerations: reader,
		TaintEvidence:      taintSweeper,
		InterprocEvidence:  interproc,
		Config:             CodeValueFlowStaleCleanupRunnerConfig{PollInterval: time.Hour},
		Wait: func(ctx context.Context, _ time.Duration) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		},
	}
	service := Service{CodeValueFlowStaleCleanupRunner: runner}
	var wg sync.WaitGroup
	var gotErr error
	service.startSideRunners(ctx, &wg, func(err error) {
		if !errors.Is(err, context.Canceled) {
			gotErr = err
		}
	})

	deadline := time.After(time.Second)
	for taintSweeper.callCount() != 1 {
		select {
		case <-deadline:
			t.Fatal("taintSweeper stale cleanup was not called")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	<-started
	cancel()
	wg.Wait()

	if gotErr != nil {
		t.Fatalf("side runner error = %v, want nil", gotErr)
	}
}

type wiringFakeCodeValueFlowCurrentGenerationReader struct {
	rows []CodeValueFlowCurrentGeneration
}

func (r *wiringFakeCodeValueFlowCurrentGenerationReader) ListCurrentCodeValueFlowGenerations(
	_ context.Context,
	_ string,
	_ int,
) ([]CodeValueFlowCurrentGeneration, error) {
	return r.rows, nil
}

type wiringRecordingCodeValueFlowTaintSweeper struct {
	mu    sync.Mutex
	calls int
}

func (w *wiringRecordingCodeValueFlowTaintSweeper) RetractStaleCodeTaintEvidence(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ int,
) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	return nil
}

func (w *wiringRecordingCodeValueFlowTaintSweeper) callCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

type wiringRecordingCodeValueFlowInterprocSweeper struct{}

func (w *wiringRecordingCodeValueFlowInterprocSweeper) RetractStaleCodeInterprocEvidence(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ int,
) error {
	return nil
}
