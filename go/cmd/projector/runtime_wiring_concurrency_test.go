// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"
)

func TestProjectorCanonicalExecutorBoundsRealNornicDBFanout(t *testing.T) {
	t.Parallel()

	getenv := projectorConcurrencyTestEnv
	raw := newProjectorBlockingConcurrencyExecutor(3)
	defer raw.releaseAll()
	executor := projectorCanonicalExecutorForGraphBackend(
		raw,
		runtimecfg.GraphBackendNornicDB,
		projectorNornicDBConfigForTest(t, getenv),
		getenv,
		nil,
		nil,
	)
	phase, ok := executor.(sourcecypher.PhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want PhaseGroupExecutor", executor)
	}

	done := make(chan error, 1)
	go func() {
		done <- phase.ExecutePhaseGroup(context.Background(), projectorFunctionStatements(7))
	}()
	select {
	case <-raw.reached:
	case <-time.After(2 * time.Second):
		t.Fatal("three raw grouped writes were not admitted concurrently")
	}
	time.Sleep(50 * time.Millisecond)
	if got, want := raw.maxInFlight(), 3; got != want {
		t.Fatalf("raw grouped-write max in flight before release = %d, want %d", got, want)
	}
	raw.releaseAll()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExecutePhaseGroup() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("phase group did not finish after releasing blocked writes")
	}
	if got := raw.maxInFlight(); got <= 1 {
		t.Fatalf("raw grouped-write max in flight = %d, want preserved concurrency", got)
	}
}

func TestProjectorCanonicalExecutorSharesNornicDBGateWithDrain(t *testing.T) {
	t.Parallel()

	getenv := projectorConcurrencyTestEnv
	raw := newProjectorBlockingConcurrencyExecutor(3)
	defer raw.releaseAll()
	executor := projectorCanonicalExecutorForGraphBackend(
		raw,
		runtimecfg.GraphBackendNornicDB,
		projectorNornicDBConfigForTest(t, getenv),
		getenv,
		nil,
		nil,
	)
	phase, ok := executor.(storagenornicdb.PhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicdb.PhaseGroupExecutor", executor)
	}
	if phase.DrainReader == nil {
		t.Fatal("NornicDB phase executor has no drain reader")
	}

	groupDone := make(chan error, 1)
	go func() {
		groupDone <- phase.ExecutePhaseGroup(context.Background(), projectorFunctionStatements(3))
	}()
	select {
	case <-raw.reached:
	case <-time.After(2 * time.Second):
		t.Fatal("three raw grouped writes were not admitted concurrently")
	}
	drainDone := make(chan error, 1)
	go func() {
		_, err := phase.DrainReader.RunWrite(context.Background(), "RETURN 0 AS __drained", nil)
		drainDone <- err
	}()
	select {
	case <-raw.drainStarted:
		t.Fatal("drain entered the raw executor while all canonical permits were occupied")
	case <-time.After(100 * time.Millisecond):
	}
	if got, want := raw.maxInFlight(), 3; got != want {
		t.Fatalf("combined grouped-write and drain max in flight before release = %d, want %d", got, want)
	}

	raw.releaseAll()
	for name, done := range map[string]<-chan error{
		"phase group": groupDone,
		"drain":       drainDone,
	} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s error = %v", name, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s did not finish after releasing blocked writes", name)
		}
	}
	if got, want := raw.maxInFlight(), 3; got != want {
		t.Fatalf("combined grouped-write and drain max in flight = %d, want %d", got, want)
	}
}

func projectorConcurrencyTestEnv(name string) string {
	switch name {
	case projectorNornicDBEntityPhaseConcurrencyEnv:
		return "7"
	case projectorNornicDBEntityPhaseGroupStatementsEnv:
		return "1"
	case graphbackpressure.CanonicalMaxInFlightEnv:
		return "3"
	default:
		return ""
	}
}

func projectorFunctionStatements(count int) []sourcecypher.Statement {
	statements := make([]sourcecypher.Statement, count)
	for i := range statements {
		statements[i] = sourcecypher.Statement{
			Operation: sourcecypher.OperationCanonicalUpsert,
			Cypher:    "UNWIND $rows AS row MERGE (f:Function {uid: row.uid})",
			Parameters: map[string]any{
				"rows":                                 []map[string]any{{"uid": i}},
				sourcecypher.StatementMetadataPhaseKey: sourcecypher.CanonicalPhaseEntities,
				sourcecypher.StatementMetadataEntityLabelKey: "Function",
			},
		}
	}
	return statements
}

type projectorBlockingConcurrencyExecutor struct {
	mu            sync.Mutex
	current       int
	max           int
	reachedTarget int
	reached       chan struct{}
	reachedOnce   sync.Once
	drainStarted  chan struct{}
	drainOnce     sync.Once
	release       chan struct{}
	releaseOnce   sync.Once
}

func newProjectorBlockingConcurrencyExecutor(target int) *projectorBlockingConcurrencyExecutor {
	return &projectorBlockingConcurrencyExecutor{
		reachedTarget: target,
		reached:       make(chan struct{}),
		drainStarted:  make(chan struct{}),
		release:       make(chan struct{}),
	}
}

func (e *projectorBlockingConcurrencyExecutor) Execute(
	context.Context,
	sourcecypher.Statement,
) error {
	return nil
}

func (e *projectorBlockingConcurrencyExecutor) ExecuteGroup(
	ctx context.Context,
	_ []sourcecypher.Statement,
) error {
	e.enter(false)
	return e.wait(ctx)
}

func (e *projectorBlockingConcurrencyExecutor) RunWrite(
	ctx context.Context,
	_ string,
	_ map[string]any,
) (storagenornicdb.DrainWriteResult, error) {
	e.enter(true)
	return storagenornicdb.DrainWriteResult{}, e.wait(ctx)
}

func (e *projectorBlockingConcurrencyExecutor) enter(drain bool) {
	e.mu.Lock()
	e.current++
	if e.current > e.max {
		e.max = e.current
	}
	if e.current == e.reachedTarget {
		e.reachedOnce.Do(func() { close(e.reached) })
	}
	if drain {
		e.drainOnce.Do(func() { close(e.drainStarted) })
	}
	e.mu.Unlock()
}

func (e *projectorBlockingConcurrencyExecutor) wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		e.decrementCurrent()
		return ctx.Err()
	case <-e.release:
		e.decrementCurrent()
		return nil
	}
}

func (e *projectorBlockingConcurrencyExecutor) decrementCurrent() {
	e.mu.Lock()
	e.current--
	e.mu.Unlock()
}

func (e *projectorBlockingConcurrencyExecutor) maxInFlight() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.max
}

func (e *projectorBlockingConcurrencyExecutor) releaseAll() {
	e.releaseOnce.Do(func() { close(e.release) })
}
