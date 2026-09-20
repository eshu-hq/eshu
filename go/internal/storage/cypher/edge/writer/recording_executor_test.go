// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"sync"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// recordingExecutor duplicates the recording fake from package cypher
// (writer_test.go). Test fakes cannot be imported across the package split,
// so each side carries its own copy.

type recordingExecutor struct {
	calls     []sourcecypher.Statement
	errAtCall error

	// readCalls records every Reader.Run invocation (used by shell-exec
	// orphan-cleanup retract tests, the only production path that reads
	// through EdgeWriter.Reader). readCandidates/readConnected script the S1
	// candidate keys and which of those currently have a relationship (S2);
	// both are nil by default, so a test that never exercises shell-exec
	// cleanup reads sees zero candidates and the production code performs no
	// further read or write.
	readCalls      []sourcecypher.Statement
	readCandidates []string
	readConnected  map[string]bool
	readErr        error
}

func (r *recordingExecutor) Execute(_ context.Context, statement sourcecypher.Statement) error {
	r.calls = append(r.calls, statement)
	if r.errAtCall != nil {
		return r.errAtCall
	}

	return nil
}

// Run implements OrphanSweepReader so recordingExecutor can double as the
// EdgeWriter.Reader in shell-exec orphan-cleanup retract tests. It
// distinguishes the S1 candidate-keys read (no "keys" parameter) from the S2
// connected-keys read (a "keys" parameter) the same way the production
// statements do.
func (r *recordingExecutor) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.readCalls = append(r.readCalls, sourcecypher.Statement{Cypher: cypher, Parameters: params})
	if r.readErr != nil {
		return nil, r.readErr
	}
	if keys, ok := params["keys"].([]string); ok {
		rows := make([]map[string]any, 0, len(keys))
		for _, k := range keys {
			if r.readConnected[k] {
				rows = append(rows, map[string]any{"key": k})
			}
		}
		return rows, nil
	}
	rows := make([]map[string]any, 0, len(r.readCandidates))
	for _, k := range r.readCandidates {
		rows = append(rows, map[string]any{"key": k})
	}
	return rows, nil
}

// noopGroupExecutor duplicates the no-op fake from package cypher
// (cloud_resource_node_writer_bench_test.go) for the same reason.
type noopGroupExecutor struct{}

func (noopGroupExecutor) Execute(_ context.Context, _ sourcecypher.Statement) error { return nil }

func (noopGroupExecutor) ExecuteGroup(_ context.Context, _ []sourcecypher.Statement) error {
	return nil
}

// concurrencyProbeExecutor duplicates the concurrency probe fake from package
// cypher (backpressure_executor_test.go) for the same reason.

type concurrencyProbeExecutor struct {
	mu      sync.Mutex
	current int
	peak    int
	release chan struct{}
	err     error
}

func (e *concurrencyProbeExecutor) enter() {
	e.mu.Lock()
	e.current++
	if e.current > e.peak {
		e.peak = e.current
	}
	e.mu.Unlock()
}

func (e *concurrencyProbeExecutor) leave() {
	e.mu.Lock()
	e.current--
	e.mu.Unlock()
}

func (e *concurrencyProbeExecutor) peakConcurrency() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.peak
}

// currentConcurrency reports how many callers are inside the inner Execute
// right now (having incremented the probe, not merely holding an executor
// permit). Tests wait on this rather than the executor's InFlight() permit
// count so the peak is measured only once the goroutines have actually entered
// the probe -- a permit can be acquired a scheduling instant before the
// goroutine reaches enter(), which otherwise races the release and leaves the
// observed peak below the ceiling.
func (e *concurrencyProbeExecutor) currentConcurrency() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.current
}

func (e *concurrencyProbeExecutor) Execute(ctx context.Context, _ sourcecypher.Statement) error {
	e.enter()
	defer e.leave()
	if e.release != nil {
		select {
		case <-e.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return e.err
}

func (e *concurrencyProbeExecutor) ExecuteGroup(ctx context.Context, _ []sourcecypher.Statement) error {
	e.enter()
	defer e.leave()
	if e.release != nil {
		select {
		case <-e.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return e.err
}

// ExecuteProbe mirrors Execute/ExecuteGroup's concurrency-tracking shape so
// TestBackpressureExecutorProbeRespectsBound can prove ExecuteProbe shares the
// same permit pool. found is always true; only the error and the concurrency
// bound are under test here.
func (e *concurrencyProbeExecutor) ExecuteProbe(ctx context.Context, _ sourcecypher.Statement) (bool, error) {
	e.enter()
	defer e.leave()
	if e.release != nil {
		select {
		case <-e.release:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return true, e.err
}
