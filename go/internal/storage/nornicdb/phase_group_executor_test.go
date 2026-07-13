// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestPhaseGroupExecutorDrainsExactChunkBeforeNextLabel(t *testing.T) {
	t.Parallel()

	inner := newExactBoundaryExecutor()
	executor := PhaseGroupExecutor{
		Inner:                  inner,
		EntityMaxStatements:    2,
		EntityPhaseConcurrency: 2,
	}
	statements := []sourcecypher.Statement{
		entityPhaseStatement("Class", "class-1"),
		entityPhaseStatement("Class", "class-2"),
		entityPhaseStatement("Function", "function-1"),
	}

	done := make(chan error, 1)
	go func() {
		done <- executor.ExecutePhaseGroup(context.Background(), statements)
	}()

	select {
	case <-inner.classStarted:
	case <-time.After(time.Second):
		t.Fatal("Class chunk did not start")
	}

	functionStartedBeforeClassRelease := false
	select {
	case <-inner.functionStarted:
		functionStartedBeforeClassRelease = true
	case <-time.After(100 * time.Millisecond):
	}
	close(inner.releaseClass)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ExecutePhaseGroup() did not finish")
	}
	if functionStartedBeforeClassRelease {
		t.Fatal("Function chunk started before the exact-size Class chunk drained")
	}
	select {
	case <-inner.functionStarted:
	default:
		t.Fatal("Function chunk did not run after the Class chunk drained")
	}
}

func entityPhaseStatement(label string, id string) sourcecypher.Statement {
	return sourcecypher.Statement{
		Cypher: "UNWIND $rows AS row MERGE (n:" + label + " {uid: row.uid})",
		Parameters: map[string]any{
			sourcecypher.StatementMetadataPhaseKey:       sourcecypher.CanonicalPhaseEntities,
			sourcecypher.StatementMetadataEntityLabelKey: label,
			"rows": []map[string]any{{"uid": id}},
		},
	}
}

type exactBoundaryExecutor struct {
	classStarted    chan struct{}
	functionStarted chan struct{}
	releaseClass    chan struct{}
	classOnce       sync.Once
	functionOnce    sync.Once
}

func newExactBoundaryExecutor() *exactBoundaryExecutor {
	return &exactBoundaryExecutor{
		classStarted:    make(chan struct{}),
		functionStarted: make(chan struct{}),
		releaseClass:    make(chan struct{}),
	}
}

func (e *exactBoundaryExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (e *exactBoundaryExecutor) ExecuteGroup(_ context.Context, statements []sourcecypher.Statement) error {
	switch {
	case strings.Contains(statements[0].Cypher, ":Class"):
		e.classOnce.Do(func() { close(e.classStarted) })
		<-e.releaseClass
	case strings.Contains(statements[0].Cypher, ":Function"):
		e.functionOnce.Do(func() { close(e.functionStarted) })
	}
	return nil
}

func TestPhaseGroupExecutorRejectsMissingInner(t *testing.T) {
	t.Parallel()

	executor := PhaseGroupExecutor{}
	if err := executor.Execute(context.Background(), sourcecypher.Statement{}); err == nil ||
		!strings.Contains(err.Error(), "inner executor is required") {
		t.Fatalf("Execute() error = %v, want missing-inner error", err)
	}
	if err := executor.ExecutePhaseGroup(context.Background(), []sourcecypher.Statement{{}}); err == nil ||
		!strings.Contains(err.Error(), "inner executor is required") {
		t.Fatalf("ExecutePhaseGroup() error = %v, want missing-inner error", err)
	}
}

func TestPhaseGroupExecutorEmptyGroupIsNoOp(t *testing.T) {
	t.Parallel()

	if err := (PhaseGroupExecutor{}).ExecutePhaseGroup(context.Background(), nil); err != nil {
		t.Fatalf("ExecutePhaseGroup(nil) error = %v, want nil", err)
	}
}
