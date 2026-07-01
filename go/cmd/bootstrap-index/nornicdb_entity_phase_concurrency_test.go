// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestBootstrapNornicDBDefaultEntityPhaseConcurrencyTracksNumCPU(t *testing.T) {
	t.Parallel()

	got := nornicDBDefaultEntityPhaseConcurrency()
	want := runtime.NumCPU()
	if want > nornicDBEntityPhaseConcurrencyCap {
		want = nornicDBEntityPhaseConcurrencyCap
	}
	if want < 1 {
		want = 1
	}
	if got != want {
		t.Fatalf("default entity phase concurrency = %d, want %d", got, want)
	}
}

func TestBootstrapCanonicalExecutorUsesConfiguredEntityPhaseConcurrency(t *testing.T) {
	t.Parallel()

	executor, err := bootstrapCanonicalExecutorForGraphBackend(
		&recordingBootstrapGroupExecutor{},
		runtimecfg.GraphBackendNornicDB,
		func(key string) string {
			if key == nornicDBEntityPhaseConcurrencyEnv {
				return "6"
			}
			return ""
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("bootstrapCanonicalExecutorForGraphBackend() error = %v, want nil", err)
	}
	phaseExecutor, ok := executor.(bootstrapNornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want bootstrapNornicDBPhaseGroupExecutor", executor)
	}
	if got, want := phaseExecutor.entityPhaseConcurrency, 6; got != want {
		t.Fatalf("entity phase concurrency = %d, want %d", got, want)
	}
}

func TestBootstrapNornicDBEntityPhaseConcurrencyRejectsInvalidEnv(t *testing.T) {
	t.Parallel()

	_, err := bootstrapCanonicalExecutorForGraphBackend(
		&recordingBootstrapGroupExecutor{},
		runtimecfg.GraphBackendNornicDB,
		func(key string) string {
			if key == nornicDBEntityPhaseConcurrencyEnv {
				return "zero"
			}
			return ""
		},
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("bootstrapCanonicalExecutorForGraphBackend() error = nil, want invalid env error")
	}
}

func TestBootstrapOperatorStatementSummaryDoesNotExposePathOrEntityID(t *testing.T) {
	t.Parallel()

	stmt := sourcecypher.Statement{
		Parameters: map[string]any{
			sourcecypher.StatementMetadataPhaseKey:       sourcecypher.CanonicalPhaseEntityContainment,
			sourcecypher.StatementMetadataEntityLabelKey: "Function",
			sourcecypher.StatementMetadataSummaryKey: "label=Function containment file=internal/private/service.go " +
				"rows=3 first_id=repo-secret:fn:1 last_id=repo-secret:fn:3",
		},
	}

	got := bootstrapOperatorStatementSummary(stmt)
	for _, forbidden := range []string{"internal/private/service.go", "repo-secret", "first_id", "last_id", "file="} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("bootstrapOperatorStatementSummary() = %q, contains %q", got, forbidden)
		}
	}
	for _, want := range []string{"phase=entity_containment", "label=Function"} {
		if !strings.Contains(got, want) {
			t.Fatalf("bootstrapOperatorStatementSummary() = %q, want substring %q", got, want)
		}
	}
}

func TestBootstrapNornicDBPhaseGroupExecutorDispatchesEntityChunksConcurrently(t *testing.T) {
	t.Parallel()

	const workers = 4
	inner := &blockingBootstrapGroupExecutor{release: make(chan struct{})}
	executor := bootstrapNornicDBPhaseGroupExecutor{
		inner:                  inner,
		maxStatements:          5,
		entityMaxStatements:    1,
		entityPhaseConcurrency: workers,
	}

	stmts := make([]sourcecypher.Statement, 8)
	for i := range stmts {
		stmts[i] = bootstrapEntityPhaseStatement()
	}

	done := make(chan error, 1)
	go func() {
		done <- executor.ExecutePhaseGroup(context.Background(), stmts)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt64(&inner.maxInFlight) < int64(workers) {
		if time.Now().After(deadline) {
			close(inner.release)
			<-done
			t.Fatalf("max in-flight ExecuteGroup = %d, want >= %d",
				atomic.LoadInt64(&inner.maxInFlight), workers)
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(inner.release)
	if err := <-done; err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := atomic.LoadInt64(&inner.callCount), int64(len(stmts)); got != want {
		t.Fatalf("ExecuteGroup calls = %d, want %d", got, want)
	}
}

func bootstrapEntityPhaseStatement() sourcecypher.Statement {
	return sourcecypher.Statement{
		Cypher: "RETURN $value",
		Parameters: map[string]any{
			"value":                                1,
			sourcecypher.StatementMetadataPhaseKey: sourcecypher.CanonicalPhaseEntities,
			sourcecypher.StatementMetadataEntityLabelKey: "Function",
			sourcecypher.StatementMetadataSummaryKey:     "phase=entities label=Function rows=1",
		},
	}
}

type blockingBootstrapGroupExecutor struct {
	release     chan struct{}
	inFlight    int64
	maxInFlight int64
	callCount   int64
}

func (b *blockingBootstrapGroupExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (b *blockingBootstrapGroupExecutor) ExecuteGroup(ctx context.Context, _ []sourcecypher.Statement) error {
	atomic.AddInt64(&b.callCount, 1)
	cur := atomic.AddInt64(&b.inFlight, 1)
	defer atomic.AddInt64(&b.inFlight, -1)
	for {
		maxSeen := atomic.LoadInt64(&b.maxInFlight)
		if cur <= maxSeen {
			break
		}
		if atomic.CompareAndSwapInt64(&b.maxInFlight, maxSeen, cur) {
			break
		}
	}
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
